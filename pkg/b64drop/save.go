package b64drop

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

func RestoreEnvelope(reader io.Reader, outputDir string) (string, int64, error) {
	metadata, compressed, err := Parse(reader)
	if err != nil {
		return "", 0, err
	}
	return RestoreCompressed(compressed, outputDir, metadata)
}

func RestoreCompressed(compressed io.Reader, outputDir string, metadata Metadata) (string, int64, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", 0, fmt.Errorf("create output directory: %v", err)
	}
	temp, err := ioutil.TempFile(outputDir, ".b64drop_tmp_")
	if err != nil {
		return "", 0, fmt.Errorf("create temporary file: %v", err)
	}
	tempName := temp.Name()
	keep := false
	defer func() {
		temp.Close()
		if !keep {
			os.Remove(tempName)
		}
	}()
	if err = Restore(compressed, temp, metadata); err != nil {
		return "", 0, err
	}
	if err = temp.Sync(); err != nil {
		return "", 0, fmt.Errorf("sync temporary file: %v", err)
	}
	if err = temp.Close(); err != nil {
		return "", 0, fmt.Errorf("close temporary file: %v", err)
	}
	for index := 0; ; index++ {
		candidate := collisionName(outputDir, metadata.Filename, index)
		if linkErr := os.Link(tempName, candidate); os.IsExist(linkErr) {
			continue
		} else if linkErr != nil {
			return "", 0, fmt.Errorf("publish output file: %v", linkErr)
		}
		if removeErr := os.Remove(tempName); removeErr != nil {
			os.Remove(candidate)
			return "", 0, fmt.Errorf("remove temporary file: %v", removeErr)
		}
		keep = true
		return candidate, metadata.OriginalSize, nil
	}
}

func collisionName(dir, filename string, index int) string {
	if index == 0 {
		return filepath.Join(dir, filename)
	}
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	if ext == ".gz" || ext == ".bz2" || ext == ".xz" {
		if inner := filepath.Ext(base); inner != "" {
			ext = inner + ext
			base = strings.TrimSuffix(base, inner)
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, index, ext))
}
