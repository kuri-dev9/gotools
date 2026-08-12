package xfer

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type matcher struct {
	pattern string
	regex   *regexp.Regexp
}

func newMatcher(pattern string, useRegex bool) (*matcher, error) {
	if pattern == "" {
		pattern = "*"
	}
	m := &matcher{pattern: pattern}
	if useRegex {
		expression, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regular expression: %w", err)
		}
		m.regex = expression
		return m, nil
	}
	if _, err := path.Match(pattern, ""); err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}
	return m, nil
}

func (m *matcher) match(name string) bool {
	if m.regex != nil {
		return m.regex.MatchString(name)
	}
	ok, _ := path.Match(m.pattern, name)
	return ok
}

func renamed(name, rename string, count int) (string, error) {
	if rename == "" {
		return name, nil
	}
	if strings.Contains(rename, "{name}") {
		return strings.ReplaceAll(rename, "{name}", name), nil
	}
	if count > 1 {
		return "", fmt.Errorf("--rename requires one matched file or a {name} placeholder")
	}
	return rename, nil
}

func destinationName(name, rename string, renameFunc func(string) (string, error), count int) (string, error) {
	if rename != "" {
		return renamed(name, rename, count)
	}
	if renameFunc != nil {
		return renameFunc(name)
	}
	return name, nil
}

func prepareDownloadFiles(files []string, options DownloadOptions) ([]string, bool, bool, error) {
	mode, err := normalizeDownloadMode(options.Mode)
	if err != nil {
		return nil, false, false, err
	}
	selected := append([]string(nil), files...)
	sort.Strings(selected)

	if mode == DownloadModeOnce {
		if options.OnceFile == "" {
			return nil, false, false, fmt.Errorf("once mode requires a time-based source filename")
		}
		for _, file := range selected {
			if path.Base(file) == path.Base(options.OnceFile) {
				return []string{file}, false, true, nil
			}
		}
		return nil, false, false, fmt.Errorf("remote file %q was not found for once mode", options.OnceFile)
	}

	if mode == DownloadModeSkipWrite {
		lastExisting := -1
		for index, file := range selected {
			name, err := destinationName(path.Base(file), options.Rename, options.RenameFunc, len(selected))
			if err != nil {
				return nil, false, false, err
			}
			destination := filepath.Join(options.LocalDir, name)
			if _, err := os.Stat(destination); err == nil {
				lastExisting = index
			} else if !os.IsNotExist(err) {
				return nil, false, false, fmt.Errorf("check local destination %q: %w", destination, err)
			}
		}
		return selected[lastExisting+1:], false, true, nil
	}

	if mode == DownloadModeOverwrite {
		return selected, true, false, nil
	}
	return selected, false, false, nil
}
