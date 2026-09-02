package main

import (
	"bufio"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gtools/pkg/b64drop"
)

type dropOptions struct {
	name, chunkSizeText   string
	noChunk, verifySource bool
	chunk                 int
	chunkSizeChanged      bool
}

type preparedTransfer struct {
	manifest    b64drop.TransferManifest
	payloadPath string
	temporary   bool
}

func runDrop(args []string) error {
	flags, help := flagsFor("drop", dropHelp)
	options := dropOptions{}
	flags.StringVarP(&options.name, "name", "n", "", "logical filename")
	flags.StringVar(&options.chunkSizeText, "chunk-size", strconv.FormatInt(b64drop.DefaultChunkPayloadSize, 10), "maximum Base64 payload per chunk")
	flags.BoolVar(&options.noChunk, "no-chunk", false, "force a single B64DROP v1 envelope")
	flags.IntVar(&options.chunk, "chunk", 0, "print only this cached chunk")
	flags.BoolVar(&options.verifySource, "verify-source", false, "recalculate source SHA-256 before using cache")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *help {
		flags.Usage()
		return nil
	}
	options.chunkSizeChanged = flags.Changed("chunk-size")
	if options.chunk < 0 {
		return fmt.Errorf("--chunk must be greater than zero")
	}
	if options.noChunk && options.chunk != 0 {
		return fmt.Errorf("--no-chunk and --chunk cannot be used together")
	}
	if options.chunk != 0 && options.chunkSizeChanged {
		return fmt.Errorf("--chunk-size cannot be changed while reprinting --chunk")
	}
	payloadLimit, err := parseSize(options.chunkSizeText)
	if err != nil {
		return fmt.Errorf("invalid --chunk-size: %v", err)
	}
	chunkBinarySize, err := b64drop.ChunkBinarySize(payloadLimit)
	if err != nil {
		return err
	}
	path, err := oneInput(flags)
	if err != nil {
		return err
	}
	stdin := path == "" || path == "-"
	if stdin && options.name == "" {
		return fmt.Errorf("--name is required for stdin")
	}
	if stdin && options.chunk != 0 {
		return fmt.Errorf("--chunk requires file input")
	}
	var prepared preparedTransfer
	if stdin {
		prepared, err = prepareStdin(options.name, chunkBinarySize)
	} else {
		prepared, err = prepareFile(path, options, chunkBinarySize)
	}
	if err != nil {
		return err
	}
	if prepared.temporary {
		defer os.RemoveAll(filepath.Dir(prepared.payloadPath))
	}
	base64Payload := prepared.manifest.TransferSize
	if options.chunk != 0 {
		return outputChunk(prepared, options.chunk)
	}
	if options.noChunk || base64Payload <= payloadLimit {
		if options.noChunk && base64Payload > payloadLimit {
			fmt.Fprintf(os.Stderr, "warning: single payload is %s; PuTTY may not copy it completely\n", humanBytes(base64Payload))
		}
		return outputSingle(prepared)
	}
	return outputInteractive(prepared, payloadLimit, stdin)
}

func prepareStdin(name string, chunkBinarySize int64) (preparedTransfer, error) {
	dir, err := ioutil.TempDir("", "gb64-transfer-")
	if err != nil {
		return preparedTransfer{}, err
	}
	payload := filepath.Join(dir, "payload.gz")
	manifest, err := createSpool(os.Stdin, payload, name, "", 0, 0, chunkBinarySize)
	if err != nil {
		os.RemoveAll(dir)
		return preparedTransfer{}, err
	}
	return preparedTransfer{manifest: manifest, payloadPath: payload, temporary: true}, nil
}

func prepareFile(path string, options dropOptions, chunkBinarySize int64) (preparedTransfer, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return preparedTransfer{}, err
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		return preparedTransfer{}, err
	}
	if !info.Mode().IsRegular() {
		return preparedTransfer{}, fmt.Errorf("input is not a regular file")
	}
	name := options.name
	if name == "" {
		name = filepath.Base(abs)
	}
	if name, err = b64drop.SafeBasename(name); err != nil {
		return preparedTransfer{}, err
	}
	cacheRoot, err := cacheRoot()
	if err != nil {
		return preparedTransfer{}, err
	}
	_ = cleanupCache(cacheRoot, b64drop.IncompleteTransferMaxAge)
	key := sha256.Sum256([]byte(abs))
	dir := filepath.Join(cacheRoot, hex.EncodeToString(key[:]))
	manifestPath, payload := filepath.Join(dir, "manifest.json"), filepath.Join(dir, "payload.gz")
	manifest, loadErr := b64drop.LoadManifest(manifestPath)
	valid := loadErr == nil && manifest.SourcePath == abs && manifest.SourceSize == info.Size() && manifest.SourceModTime == info.ModTime().UnixNano() && manifest.Filename == name
	if valid {
		if payloadInfo, statErr := os.Stat(payload); statErr != nil || payloadInfo.Size() != manifest.CompressedSize {
			valid = false
		}
	}
	if valid && options.verifySource {
		file, openErr := os.Open(abs)
		if openErr != nil {
			return preparedTransfer{}, openErr
		}
		_, digest, inspectErr := b64drop.Inspect(file)
		file.Close()
		if inspectErr != nil {
			return preparedTransfer{}, inspectErr
		}
		if digest != manifest.OriginalSHA256 {
			valid = false
		}
	}
	if !valid && options.chunk != 0 {
		return preparedTransfer{}, fmt.Errorf("cached transfer is unavailable or stale; run a full gb64 transfer first")
	}
	if valid {
		if options.chunkSizeChanged && chunkBinarySize != manifest.ChunkBinarySize {
			manifest.ChunkBinarySize = chunkBinarySize
			manifest.ChunkTotal, manifest.TransferSize = chunkGeometry(manifest.CompressedSize, chunkBinarySize)
			manifest.TransferID, err = newTransferID()
			if err != nil {
				return preparedTransfer{}, err
			}
			manifest.CreatedAt = time.Now().UTC()
			if err = b64drop.SaveManifest(manifestPath, manifest); err != nil {
				return preparedTransfer{}, err
			}
		}
		return preparedTransfer{manifest: manifest, payloadPath: payload}, nil
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return preparedTransfer{}, err
	}
	temp, err := ioutil.TempFile(dir, ".payload-")
	if err != nil {
		return preparedTransfer{}, err
	}
	tempPath := temp.Name()
	temp.Close()
	defer os.Remove(tempPath)
	file, err := os.Open(abs)
	if err != nil {
		return preparedTransfer{}, err
	}
	manifest, err = createSpool(file, tempPath, name, abs, info.Size(), info.ModTime().UnixNano(), chunkBinarySize)
	file.Close()
	if err != nil {
		return preparedTransfer{}, err
	}
	os.Remove(payload)
	if err = os.Rename(tempPath, payload); err != nil {
		return preparedTransfer{}, err
	}
	if err = b64drop.SaveManifest(manifestPath, manifest); err != nil {
		return preparedTransfer{}, err
	}
	return preparedTransfer{manifest: manifest, payloadPath: payload}, nil
}

func createSpool(source io.Reader, payloadPath, name, sourcePath string, sourceSize, sourceMtime, chunkBinarySize int64) (b64drop.TransferManifest, error) {
	file, err := os.OpenFile(payloadPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return b64drop.TransferManifest{}, err
	}
	h := sha256.New()
	zipper := gzip.NewWriter(file)
	zipper.Header.ModTime = time.Time{}
	zipper.Header.OS = 255
	originalSize, copyErr := io.Copy(zipper, io.TeeReader(source, h))
	closeZipErr := zipper.Close()
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		return b64drop.TransferManifest{}, copyErr
	}
	if closeZipErr != nil {
		return b64drop.TransferManifest{}, closeZipErr
	}
	if syncErr != nil {
		return b64drop.TransferManifest{}, syncErr
	}
	if closeErr != nil {
		return b64drop.TransferManifest{}, closeErr
	}
	info, err := os.Stat(payloadPath)
	if err != nil {
		return b64drop.TransferManifest{}, err
	}
	id, err := newTransferID()
	if err != nil {
		return b64drop.TransferManifest{}, err
	}
	total, transferSize := chunkGeometry(info.Size(), chunkBinarySize)
	return b64drop.TransferManifest{Version: 2, TransferID: id, Filename: name, OriginalSize: originalSize, OriginalSHA256: hex.EncodeToString(h.Sum(nil)), CompressedSize: info.Size(), TransferSize: transferSize, ChunkBinarySize: chunkBinarySize, ChunkTotal: total, CreatedAt: time.Now().UTC(), SourcePath: sourcePath, SourceSize: sourceSize, SourceModTime: sourceMtime}, nil
}

func chunkGeometry(compressedSize, chunkBinarySize int64) (int, int64) {
	total := int((compressedSize + chunkBinarySize - 1) / chunkBinarySize)
	if total == 0 {
		total = 1
	}
	var transfer int64
	for index := 0; index < total; index++ {
		size := chunkBinarySize
		remaining := compressedSize - int64(index)*chunkBinarySize
		if remaining < size {
			size = remaining
		}
		transfer += b64drop.Base64Size(size)
	}
	return total, transfer
}

func outputSingle(prepared preparedTransfer) error {
	file, err := os.Open(prepared.payloadPath)
	if err != nil {
		return err
	}
	defer file.Close()
	m := prepared.manifest
	return b64drop.EncodeCompressed(os.Stdout, file, b64drop.Metadata{Filename: m.Filename, OriginalSize: m.OriginalSize, SHA256: m.OriginalSHA256})
}

func outputChunk(prepared preparedTransfer, index int) error {
	m := prepared.manifest
	if index < 1 || index > m.ChunkTotal {
		return fmt.Errorf("chunk must be between 1 and %d", m.ChunkTotal)
	}
	offset := int64(index-1) * m.ChunkBinarySize
	size := m.ChunkBinarySize
	if remaining := m.CompressedSize - offset; remaining < size {
		size = remaining
	}
	file, err := os.Open(prepared.payloadPath)
	if err != nil {
		return err
	}
	defer file.Close()
	h := sha256.New()
	if _, err = io.Copy(h, io.NewSectionReader(file, offset, size)); err != nil {
		return err
	}
	metadata := b64drop.ChunkMetadata{TransferID: m.TransferID, Filename: m.Filename, OriginalSize: m.OriginalSize, OriginalSHA256: m.OriginalSHA256, Compression: "gzip", Encoding: "base64", CompressedSize: m.CompressedSize, TransferSize: m.TransferSize, ChunkIndex: index, ChunkTotal: m.ChunkTotal, ChunkSize: size, ChunkSHA256: hex.EncodeToString(h.Sum(nil))}
	return b64drop.EncodeChunk(os.Stdout, io.NewSectionReader(file, offset, size), metadata)
}

func outputInteractive(prepared preparedTransfer, payloadLimit int64, stdinSource bool) error {
	m := prepared.manifest
	fmt.Fprintf(os.Stderr, "B64Drop Transfer\n\nFile        : %s\nOriginal    : %s\nCompressed  : %s\nTransfer    : %s\nChunks      : %d\nChunk Size  : %s Base64 payload (configurable)\n\n", m.Filename, humanBytes(m.OriginalSize), humanBytes(m.CompressedSize), humanBytes(m.TransferSize), m.ChunkTotal, humanBytes(payloadLimit))
	console, closeConsole, err := promptInput(stdinSource)
	if err != nil {
		return err
	}
	defer closeConsole()
	reader := bufio.NewReader(console)
	for index := 1; index <= m.ChunkTotal; index++ {
		fmt.Fprintf(os.Stderr, "Press ENTER to output chunk %d/%d...", index, m.ChunkTotal)
		if _, err = reader.ReadString('\n'); err != nil {
			return err
		}
		if err = outputChunk(prepared, index); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Chunk %d/%d printed. Copy this block to Windows.\n\n", index, m.ChunkTotal)
	}
	for {
		fmt.Fprint(os.Stderr, "Enter a chunk number to reprint, or q to quit: ")
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			if readErr == io.EOF {
				return nil
			}
			return readErr
		}
		line = strings.TrimSpace(line)
		if line == "q" || line == "quit" || line == "" {
			return nil
		}
		index, parseErr := strconv.Atoi(line)
		if parseErr != nil || index < 1 || index > m.ChunkTotal {
			fmt.Fprintf(os.Stderr, "Enter a number from 1 to %d.\n", m.ChunkTotal)
			continue
		}
		if err = outputChunk(prepared, index); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Chunk %d/%d reprinted.\n", index, m.ChunkTotal)
	}
}

func promptInput(stdinSource bool) (*os.File, func() error, error) {
	if !stdinSource {
		return os.Stdin, func() error { return nil }, nil
	}
	name := "/dev/tty"
	if runtime.GOOS == "windows" {
		name = "CONIN$"
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, nil, fmt.Errorf("chunk mode requires an interactive console: %v", err)
	}
	return file, file.Close, nil
}

func cacheRoot() (string, error) {
	root, err := os.UserCacheDir()
	if err == nil {
		candidate := filepath.Join(root, "gb64", "transfers")
		if mkdirErr := os.MkdirAll(candidate, 0700); mkdirErr == nil {
			return candidate, nil
		}
	}
	fallback := filepath.Join(os.TempDir(), "gb64-cache", "transfers")
	if err = os.MkdirAll(fallback, 0700); err != nil {
		return "", err
	}
	return fallback, nil
}
func cleanupCache(root string, maxAge time.Duration) error {
	entries, err := ioutil.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() && entry.ModTime().Before(cutoff) {
			if err = os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
func newTransferID() (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func parseSize(value string) (int64, error) {
	text := strings.TrimSpace(strings.ToUpper(value))
	multiplier := int64(1)
	for _, suffix := range []struct {
		name  string
		value int64
	}{{"MIB", 1024 * 1024}, {"MB", 1024 * 1024}, {"M", 1024 * 1024}, {"KIB", 1024}, {"KB", 1024}, {"K", 1024}} {
		if strings.HasSuffix(text, suffix.name) {
			multiplier = suffix.value
			text = strings.TrimSpace(strings.TrimSuffix(text, suffix.name))
			break
		}
	}
	number, err := strconv.ParseInt(text, 10, 64)
	if err != nil || number <= 0 {
		return 0, fmt.Errorf("expected a positive size such as 5M")
	}
	if number > (1<<63-1)/multiplier {
		return 0, fmt.Errorf("size is too large")
	}
	return number * multiplier, nil
}

func humanBytes(size int64) string {
	const kb = 1024
	const mb = kb * 1024
	const gb = mb * 1024
	if size >= gb {
		return fmt.Sprintf("%.1f GB", float64(size)/gb)
	}
	if size >= mb {
		return fmt.Sprintf("%.1f MB", float64(size)/mb)
	}
	if size >= kb {
		return fmt.Sprintf("%.1f KB", float64(size)/kb)
	}
	return fmt.Sprintf("%d bytes", size)
}
