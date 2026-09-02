package b64drop

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func ReceiveChunk(reader io.Reader, transferRoot, outputDir string) (ChunkProgress, error) {
	metadata, data, err := ParseChunk(reader)
	progress := ChunkProgress{LastIndex: metadata.ChunkIndex}
	if metadata.TransferID != "" {
		progress.Manifest = ManifestFromChunk(metadata)
	}
	if err != nil {
		if metadata.ChunkIndex > 0 {
			progress.Failed = []int{metadata.ChunkIndex}
		}
		return progress, err
	}
	if err = os.MkdirAll(transferRoot, 0700); err != nil {
		return progress, err
	}
	dir := filepath.Join(transferRoot, metadata.TransferID)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return progress, err
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	manifest, loadErr := LoadManifest(manifestPath)
	if os.IsNotExist(loadErr) {
		manifest = ManifestFromChunk(metadata)
		manifest.CreatedAt = time.Now().UTC()
		if err = SaveManifest(manifestPath, manifest); err != nil {
			return progress, err
		}
	} else if loadErr != nil {
		return progress, fmt.Errorf("load transfer manifest: %v", loadErr)
	} else if !manifestMatchesChunk(manifest, metadata) {
		return progress, fmt.Errorf("chunk metadata conflicts with transfer %s", metadata.TransferID)
	}
	progress.Manifest = manifest
	chunkPath := chunkFilename(dir, metadata.ChunkIndex)
	if existing, readErr := ioutil.ReadFile(chunkPath); readErr == nil {
		digest := sha256.Sum256(existing)
		if int64(len(existing)) == metadata.ChunkSize && hex.EncodeToString(digest[:]) == metadata.ChunkSHA256 {
			progress.ReReceived = true
		} else {
			return progress, fmt.Errorf("stored chunk %d is corrupt", metadata.ChunkIndex)
		}
	} else if !os.IsNotExist(readErr) {
		return progress, readErr
	} else if err = publishChunk(chunkPath, data); err != nil {
		return progress, err
	}
	progress.Received, progress.Missing = scanChunks(dir, manifest.ChunkTotal)
	if len(progress.Missing) != 0 {
		return progress, nil
	}
	var storedSize int64
	for index := 1; index <= manifest.ChunkTotal; index++ {
		info, statErr := os.Stat(chunkFilename(dir, index))
		if statErr != nil {
			return progress, statErr
		}
		storedSize += info.Size()
	}
	if storedSize != manifest.CompressedSize {
		return progress, fmt.Errorf("compressed_size mismatch")
	}
	sequence := &chunkSequence{dir: dir, total: manifest.ChunkTotal}
	finalMetadata := Metadata{Filename: manifest.Filename, OriginalSize: manifest.OriginalSize, SHA256: manifest.OriginalSHA256}
	path, _, err := RestoreCompressed(sequence, outputDir, finalMetadata)
	sequence.Close()
	if err != nil {
		return progress, fmt.Errorf("final transfer validation: %v", err)
	}
	progress.Completed = true
	progress.PublishedPath = path
	if removeErr := os.RemoveAll(dir); removeErr != nil {
		return progress, fmt.Errorf("file restored but transfer cleanup failed: %v", removeErr)
	}
	return progress, nil
}

func manifestMatchesChunk(m TransferManifest, c ChunkMetadata) bool {
	return m.Version == 2 && m.TransferID == c.TransferID && m.Filename == c.Filename && m.OriginalSize == c.OriginalSize && m.OriginalSHA256 == c.OriginalSHA256 && m.CompressedSize == c.CompressedSize && m.TransferSize == c.TransferSize && m.ChunkTotal == c.ChunkTotal
}

func publishChunk(path string, data []byte) error {
	temp, err := ioutil.TempFile(filepath.Dir(path), ".chunk-")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	_ = temp.Chmod(0600)
	_, err = io.Copy(temp, bytes.NewReader(data))
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Link(tempName, path); os.IsExist(err) {
		return nil
	}
	return err
}

func chunkFilename(dir string, index int) string {
	return filepath.Join(dir, fmt.Sprintf("chunk_%06d.bin", index))
}

func scanChunks(dir string, total int) ([]int, []int) {
	received := make([]int, 0, total)
	missing := make([]int, 0)
	for index := 1; index <= total; index++ {
		if info, err := os.Stat(chunkFilename(dir, index)); err == nil && info.Mode().IsRegular() {
			received = append(received, index)
		} else {
			missing = append(missing, index)
		}
	}
	return received, missing
}

func CleanupTransfers(root string, maxAge time.Duration) error {
	entries, err := ioutil.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if !entry.IsDir() || entry.ModTime().After(cutoff) {
			continue
		}
		if err = os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

type chunkSequence struct {
	dir         string
	total, next int
	current     *os.File
}

func (r *chunkSequence) Read(p []byte) (int, error) {
	for {
		if r.current == nil {
			r.next++
			if r.next > r.total {
				return 0, io.EOF
			}
			file, err := os.Open(chunkFilename(r.dir, r.next))
			if err != nil {
				return 0, err
			}
			r.current = file
		}
		n, err := r.current.Read(p)
		if err == io.EOF {
			r.current.Close()
			r.current = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (r *chunkSequence) Close() error {
	if r.current != nil {
		return r.current.Close()
	}
	return nil
}

func SortUnique(values []int) []int {
	sort.Ints(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
