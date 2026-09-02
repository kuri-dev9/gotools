package b64drop

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	BeginMarker = "-----BEGIN B64DROP-----"
	EndMarker   = "-----END B64DROP-----"
)

type Metadata struct {
	Filename     string
	OriginalSize int64
	SHA256       string
}

func SafeBasename(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	parts := strings.Split(name, "/")
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("filename is empty")
	}
	name = parts[len(parts)-1]
	if name == "" || name == "." || name == ".." || !utf8.ValidString(name) {
		return "", fmt.Errorf("invalid filename")
	}
	for _, r := range name {
		if r == 0 || r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("filename contains a control character")
		}
	}
	return name, nil
}

func EscapeFilename(name string) (string, error) {
	name, err := SafeBasename(name)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	const hexUpper = "0123456789ABCDEF"
	for _, c := range []byte(name) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", rune(c)) {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hexUpper[c>>4])
			b.WriteByte(hexUpper[c&15])
		}
	}
	return b.String(), nil
}

func UnescapeFilename(value string) (string, error) {
	b := make([]byte, 0, len(value))
	for i := 0; i < len(value); i++ {
		if value[i] != '%' {
			b = append(b, value[i])
			continue
		}
		if i+2 >= len(value) {
			return "", fmt.Errorf("malformed filename escape")
		}
		v, err := strconv.ParseUint(value[i+1:i+3], 16, 8)
		if err != nil {
			return "", fmt.Errorf("malformed filename escape")
		}
		b = append(b, byte(v))
		i += 2
	}
	return SafeBasename(string(b))
}

func Inspect(reader io.Reader) (int64, string, error) {
	h := sha256.New()
	n, err := io.Copy(h, reader)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

type lineWriter struct {
	w   io.Writer
	n   int
	err error
}

func (w *lineWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	written := 0
	for _, c := range p {
		if w.n == 76 {
			_, w.err = io.WriteString(w.w, "\n")
			w.n = 0
			if w.err != nil {
				return written, w.err
			}
		}
		_, w.err = w.w.Write([]byte{c})
		if w.err != nil {
			return written, w.err
		}
		w.n++
		written++
	}
	return written, nil
}

func Encode(writer io.Writer, source io.Reader, metadata Metadata) error {
	name, err := EscapeFilename(metadata.Filename)
	if err != nil {
		return err
	}
	if metadata.OriginalSize < 0 || len(metadata.SHA256) != 64 {
		return fmt.Errorf("invalid metadata")
	}
	if _, err = hex.DecodeString(metadata.SHA256); err != nil || metadata.SHA256 != strings.ToLower(metadata.SHA256) {
		return fmt.Errorf("invalid SHA-256")
	}
	if _, err = fmt.Fprintf(writer, "%s\nversion=1\nfilename=%s\noriginal_size=%d\ncompression=gzip\nencoding=base64\nsha256=%s\n\n", BeginMarker, name, metadata.OriginalSize, metadata.SHA256); err != nil {
		return err
	}
	lw := &lineWriter{w: writer}
	b64 := base64.NewEncoder(base64.StdEncoding, lw)
	gzipWriter, err := gzip.NewWriterLevel(b64, gzip.DefaultCompression)
	if err != nil {
		return err
	}
	gzipWriter.Header.ModTime = gzipWriter.Header.ModTime.UTC()
	gzipWriter.Header.OS = 255
	if _, err = io.Copy(gzipWriter, source); err != nil {
		return err
	}
	if err = gzipWriter.Close(); err != nil {
		return err
	}
	if err = b64.Close(); err != nil {
		return err
	}
	if lw.n > 0 {
		if _, err = io.WriteString(writer, "\n"); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(writer, "%s\n", EndMarker)
	return err
}

func EncodeCompressed(writer io.Writer, compressed io.Reader, metadata Metadata) error {
	name, err := EscapeFilename(metadata.Filename)
	if err != nil {
		return err
	}
	if metadata.OriginalSize < 0 || len(metadata.SHA256) != 64 || !validLowerHex(metadata.SHA256) {
		return fmt.Errorf("invalid metadata")
	}
	if _, err = fmt.Fprintf(writer, "%s\nversion=1\nfilename=%s\noriginal_size=%d\ncompression=gzip\nencoding=base64\nsha256=%s\n\n", BeginMarker, name, metadata.OriginalSize, metadata.SHA256); err != nil {
		return err
	}
	lw := &lineWriter{w: writer}
	encoder := base64.NewEncoder(base64.StdEncoding, lw)
	if _, err = io.Copy(encoder, compressed); err != nil {
		return err
	}
	if err = encoder.Close(); err != nil {
		return err
	}
	if lw.n > 0 {
		if _, err = io.WriteString(writer, "\n"); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(writer, "%s\n", EndMarker)
	return err
}

func Parse(reader io.Reader) (Metadata, io.Reader, error) {
	var metadata Metadata
	raw, err := ioutil.ReadAll(reader)
	if err != nil {
		return metadata, nil, err
	}
	block, err := ExtractBlock(string(raw), BeginMarker, EndMarker)
	if err != nil {
		return metadata, nil, err
	}
	scanner := bufio.NewScanner(strings.NewReader(block))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	if !scanner.Scan() || strings.TrimSuffix(scanner.Text(), "\r") != BeginMarker {
		return metadata, nil, fmt.Errorf("missing BEGIN marker")
	}
	values := make(map[string]string)
	order := []string{"version", "filename", "original_size", "compression", "encoding", "sha256"}
	for i := 0; ; i++ {
		if !scanner.Scan() {
			return metadata, nil, fmt.Errorf("incomplete metadata")
		}
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if i != len(order) {
				return metadata, nil, fmt.Errorf("missing metadata")
			}
			break
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || i >= len(order) || parts[0] != order[i] {
			return metadata, nil, fmt.Errorf("invalid metadata")
		}
		values[parts[0]] = parts[1]
	}
	if values["version"] != "1" {
		return metadata, nil, fmt.Errorf("unsupported protocol version")
	}
	if values["compression"] != "gzip" || values["encoding"] != "base64" {
		return metadata, nil, fmt.Errorf("unsupported encoding")
	}
	name, err := UnescapeFilename(values["filename"])
	if err != nil {
		return metadata, nil, err
	}
	size, err := strconv.ParseInt(values["original_size"], 10, 64)
	if err != nil || size < 0 || strconv.FormatInt(size, 10) != values["original_size"] {
		return metadata, nil, fmt.Errorf("invalid original_size")
	}
	digest := values["sha256"]
	if len(digest) != 64 || digest != strings.ToLower(digest) {
		return metadata, nil, fmt.Errorf("invalid SHA-256")
	}
	if _, err = hex.DecodeString(digest); err != nil {
		return metadata, nil, fmt.Errorf("invalid SHA-256")
	}
	var payload strings.Builder
	foundEnd := false
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == EndMarker {
			foundEnd = true
			break
		}
		payload.WriteString(line)
		payload.WriteByte('\n')
	}
	if err = scanner.Err(); err != nil {
		return metadata, nil, err
	}
	if !foundEnd {
		return metadata, nil, fmt.Errorf("missing END marker")
	}
	metadata = Metadata{Filename: name, OriginalSize: size, SHA256: digest}
	return metadata, base64.NewDecoder(base64.StdEncoding, &payloadReader{reader: strings.NewReader(payload.String())}), nil
}

func ExtractBlock(text, begin, end string) (string, error) {
	start := strings.Index(text, begin)
	if start < 0 {
		return "", fmt.Errorf("missing BEGIN marker")
	}
	rest := text[start+len(begin):]
	finish := strings.Index(rest, end)
	if finish < 0 {
		return "", fmt.Errorf("missing END marker")
	}
	if strings.Contains(rest[:finish], begin) {
		return "", fmt.Errorf("nested BEGIN marker")
	}
	return text[start : start+len(begin)+finish+len(end)], nil
}

type payloadReader struct{ reader io.Reader }

func (r *payloadReader) Read(p []byte) (int, error) {
	buffer := make([]byte, len(p))
	for {
		n, err := r.reader.Read(buffer)
		written := 0
		for _, c := range buffer[:n] {
			if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
				p[written] = c
				written++
			}
		}
		if written > 0 || err != nil {
			return written, err
		}
	}
}

func Restore(reader io.Reader, destination io.Writer, metadata Metadata) error {
	buffered := bufio.NewReader(reader)
	gzipReader, err := gzip.NewReader(buffered)
	if err != nil {
		return fmt.Errorf("gzip: %v", err)
	}
	gzipReader.Multistream(false)
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(destination, h), gzipReader)
	closeErr := gzipReader.Close()
	if copyErr != nil {
		return fmt.Errorf("gzip: %v", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("gzip: %v", closeErr)
	}
	if _, trailingErr := buffered.Peek(1); trailingErr != io.EOF {
		if trailingErr == nil {
			return fmt.Errorf("trailing compressed data")
		}
		return fmt.Errorf("read compressed data: %v", trailingErr)
	}
	if n != metadata.OriginalSize {
		return fmt.Errorf("original_size mismatch")
	}
	if hex.EncodeToString(h.Sum(nil)) != metadata.SHA256 {
		return fmt.Errorf("SHA-256 mismatch")
	}
	return nil
}

func OpenInput(path string) (*os.File, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	name, err := SafeBasename(filepath.Base(path))
	if err != nil {
		f.Close()
		return nil, "", err
	}
	return f, name, nil
}
