package b64drop

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	ChunkBeginMarker               = "-----BEGIN B64DROP CHUNK-----"
	ChunkEndMarker                 = "-----END B64DROP CHUNK-----"
	DefaultChunkPayloadSize  int64 = 5 * 1024 * 1024
	IncompleteTransferMaxAge       = 7 * 24 * time.Hour
)

type ChunkMetadata struct {
	TransferID     string `json:"transfer_id"`
	Filename       string `json:"filename"`
	OriginalSize   int64  `json:"original_size"`
	OriginalSHA256 string `json:"original_sha256"`
	Compression    string `json:"compression"`
	Encoding       string `json:"encoding"`
	CompressedSize int64  `json:"compressed_size"`
	TransferSize   int64  `json:"transfer_size"`
	ChunkIndex     int    `json:"chunk_index"`
	ChunkTotal     int    `json:"chunk_total"`
	ChunkSize      int64  `json:"chunk_size"`
	ChunkSHA256    string `json:"chunk_sha256"`
}

type TransferManifest struct {
	Version         int       `json:"version"`
	TransferID      string    `json:"transfer_id"`
	Filename        string    `json:"filename"`
	OriginalSize    int64     `json:"original_size"`
	OriginalSHA256  string    `json:"original_sha256"`
	CompressedSize  int64     `json:"compressed_size"`
	TransferSize    int64     `json:"transfer_size"`
	ChunkBinarySize int64     `json:"chunk_binary_size"`
	ChunkTotal      int       `json:"chunk_total"`
	CreatedAt       time.Time `json:"created_at"`
	SourcePath      string    `json:"source_path,omitempty"`
	SourceSize      int64     `json:"source_size,omitempty"`
	SourceModTime   int64     `json:"source_mtime_unix_nano,omitempty"`
}

type ChunkProgress struct {
	Manifest      TransferManifest
	Received      []int
	Missing       []int
	Failed        []int
	LastIndex     int
	ReReceived    bool
	Completed     bool
	PublishedPath string
}

func ChunkBinarySize(payloadLimit int64) (int64, error) {
	if payloadLimit < 4 {
		return 0, fmt.Errorf("chunk size must be at least 4 bytes")
	}
	return (payloadLimit / 4) * 3, nil
}

func Base64Size(binarySize int64) int64 {
	if binarySize == 0 {
		return 0
	}
	return ((binarySize + 2) / 3) * 4
}

func EncodeChunk(writer io.Writer, source io.Reader, metadata ChunkMetadata) error {
	if err := validateChunkMetadata(metadata); err != nil {
		return err
	}
	name, err := EscapeFilename(metadata.Filename)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "%s\nversion=2\ntransfer_id=%s\nfilename=%s\noriginal_size=%d\noriginal_sha256=%s\ncompression=gzip\nencoding=base64\ncompressed_size=%d\ntransfer_size=%d\nchunk_index=%d\nchunk_total=%d\nchunk_size=%d\nchunk_sha256=%s\n\n", ChunkBeginMarker, metadata.TransferID, name, metadata.OriginalSize, metadata.OriginalSHA256, metadata.CompressedSize, metadata.TransferSize, metadata.ChunkIndex, metadata.ChunkTotal, metadata.ChunkSize, metadata.ChunkSHA256)
	if err != nil {
		return err
	}
	lw := &lineWriter{w: writer}
	encoder := base64.NewEncoder(base64.StdEncoding, lw)
	n, err := io.Copy(encoder, source)
	if err != nil {
		return err
	}
	if err = encoder.Close(); err != nil {
		return err
	}
	if n != metadata.ChunkSize {
		return fmt.Errorf("chunk source size mismatch")
	}
	if lw.n > 0 {
		if _, err = io.WriteString(writer, "\n"); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(writer, "%s\n", ChunkEndMarker)
	return err
}

func ParseChunk(reader io.Reader) (ChunkMetadata, []byte, error) {
	var metadata ChunkMetadata
	raw, err := ioutil.ReadAll(reader)
	if err != nil {
		return metadata, nil, err
	}
	block, err := ExtractBlock(string(raw), ChunkBeginMarker, ChunkEndMarker)
	if err != nil {
		return metadata, nil, err
	}
	scanner := bufio.NewScanner(strings.NewReader(block))
	scanner.Buffer(make([]byte, 4096), int(DefaultChunkPayloadSize*2))
	if !scanner.Scan() || strings.TrimSuffix(scanner.Text(), "\r") != ChunkBeginMarker {
		return metadata, nil, fmt.Errorf("missing BEGIN marker")
	}
	order := []string{"version", "transfer_id", "filename", "original_size", "original_sha256", "compression", "encoding", "compressed_size", "transfer_size", "chunk_index", "chunk_total", "chunk_size", "chunk_sha256"}
	values := make(map[string]string)
	for i := 0; ; i++ {
		if !scanner.Scan() {
			return metadata, nil, fmt.Errorf("incomplete chunk metadata")
		}
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if i != len(order) {
				return metadata, nil, fmt.Errorf("missing chunk metadata")
			}
			break
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || i >= len(order) || parts[0] != order[i] {
			return metadata, nil, fmt.Errorf("invalid chunk metadata")
		}
		values[parts[0]] = parts[1]
	}
	metadata, err = parseChunkValues(values)
	if err != nil {
		return metadata, nil, err
	}
	var payload strings.Builder
	found := false
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == ChunkEndMarker {
			found = true
			break
		}
		payload.WriteString(line)
		payload.WriteByte('\n')
	}
	if err = scanner.Err(); err != nil {
		return metadata, nil, err
	}
	if !found {
		return metadata, nil, fmt.Errorf("missing END marker")
	}
	decoded, err := ioutil.ReadAll(base64.NewDecoder(base64.StdEncoding, &payloadReader{reader: strings.NewReader(payload.String())}))
	if err != nil {
		return metadata, nil, fmt.Errorf("invalid Base64: %v", err)
	}
	if int64(len(decoded)) != metadata.ChunkSize {
		return metadata, nil, fmt.Errorf("chunk_size mismatch")
	}
	digest := sha256.Sum256(decoded)
	if hex.EncodeToString(digest[:]) != metadata.ChunkSHA256 {
		return metadata, nil, fmt.Errorf("chunk SHA-256 mismatch")
	}
	return metadata, decoded, nil
}

func parseChunkValues(v map[string]string) (ChunkMetadata, error) {
	var m ChunkMetadata
	if v["version"] != "2" {
		return m, fmt.Errorf("unsupported chunk version")
	}
	m.TransferID = v["transfer_id"]
	name, err := UnescapeFilename(v["filename"])
	if err != nil {
		return m, err
	}
	m.Filename = name
	parse64 := func(key string) (int64, error) {
		n, e := strconv.ParseInt(v[key], 10, 64)
		if e != nil || n < 0 || strconv.FormatInt(n, 10) != v[key] {
			return 0, fmt.Errorf("invalid %s", key)
		}
		return n, nil
	}
	if m.OriginalSize, err = parse64("original_size"); err != nil {
		return m, err
	}
	if m.CompressedSize, err = parse64("compressed_size"); err != nil {
		return m, err
	}
	if m.TransferSize, err = parse64("transfer_size"); err != nil {
		return m, err
	}
	if m.ChunkSize, err = parse64("chunk_size"); err != nil {
		return m, err
	}
	index, err := parse64("chunk_index")
	if err != nil {
		return m, err
	}
	m.ChunkIndex = int(index)
	total, err := parse64("chunk_total")
	if err != nil {
		return m, err
	}
	m.ChunkTotal = int(total)
	m.OriginalSHA256, m.ChunkSHA256 = v["original_sha256"], v["chunk_sha256"]
	m.Compression, m.Encoding = v["compression"], v["encoding"]
	if err = validateChunkMetadata(m); err != nil {
		return m, err
	}
	return m, nil
}

func validateChunkMetadata(m ChunkMetadata) error {
	if len(m.TransferID) != 32 || !validLowerHex(m.TransferID) {
		return fmt.Errorf("invalid transfer_id")
	}
	if m.Compression != "gzip" || m.Encoding != "base64" {
		return fmt.Errorf("unsupported chunk encoding")
	}
	if m.OriginalSize < 0 || m.CompressedSize < 0 || m.TransferSize < 0 || m.ChunkSize < 0 {
		return fmt.Errorf("invalid chunk sizes")
	}
	if m.ChunkIndex < 1 || m.ChunkTotal < 1 || m.ChunkIndex > m.ChunkTotal {
		return fmt.Errorf("invalid chunk index")
	}
	if m.ChunkTotal > 1000000 {
		return fmt.Errorf("chunk_total is too large")
	}
	if len(m.OriginalSHA256) != 64 || !validLowerHex(m.OriginalSHA256) || len(m.ChunkSHA256) != 64 || !validLowerHex(m.ChunkSHA256) {
		return fmt.Errorf("invalid chunk SHA-256")
	}
	_, err := SafeBasename(m.Filename)
	return err
}

func validLowerHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}

func ManifestFromChunk(m ChunkMetadata) TransferManifest {
	return TransferManifest{Version: 2, TransferID: m.TransferID, Filename: m.Filename, OriginalSize: m.OriginalSize, OriginalSHA256: m.OriginalSHA256, CompressedSize: m.CompressedSize, TransferSize: m.TransferSize, ChunkTotal: m.ChunkTotal}
}

func SaveManifest(path string, manifest TransferManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	temp, err := ioutil.TempFile(filepath.Dir(path), ".manifest-")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	_ = temp.Chmod(0600)
	_, err = temp.Write(data)
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	os.Remove(path)
	return os.Rename(tempName, path)
}

func LoadManifest(path string) (TransferManifest, error) {
	var manifest TransferManifest
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return manifest, err
	}
	err = json.Unmarshal(data, &manifest)
	return manifest, err
}
