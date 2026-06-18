package filewatch

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	ConfPath string
	Count    int
	Warn     int
	All      bool
	Ignore   []string
	Now      time.Time
}

type Report struct {
	ConfDir string
	Configs []ConfigReport
}

type ConfigReport struct {
	Name    string
	Entries []WatchEntry
	Error   string
}

type WatchEntry struct {
	Label   string
	Dir     string
	Pattern string
	Files   []FileEntry
	Error   string
}

type FileEntry struct {
	Name string
	Time time.Time
	Size int64
	Warn bool
}

type patternSpec struct {
	Label   string
	Pattern string
}

type compiledPattern struct {
	dir      string
	fileExpr *regexp.Regexp
	timeExpr *regexp.Regexp
}

func Run(opts Options) (*Report, error) {
	if opts.Count <= 0 {
		opts.Count = 2
	}
	if opts.Warn <= 0 {
		opts.Warn = 1
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}

	confDir, err := ResolveConfDir(opts.ConfPath)
	if err != nil {
		return nil, err
	}

	files, err := listConfigFiles(confDir, opts.Ignore)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .ini or .json files in %s", confDir)
	}

	report := &Report{ConfDir: confDir}
	for _, file := range files {
		cfg := ConfigReport{Name: filepath.Base(file)}
		specs, err := ParseConfig(file)
		if err != nil {
			cfg.Error = err.Error()
			report.Configs = append(report.Configs, cfg)
			continue
		}

		for _, spec := range specs {
			for _, expanded := range expandAlternates(spec.Pattern) {
				entry := WatchEntry{Label: spec.Label, Pattern: expanded}
				cp, err := compilePattern(expanded)
				if err != nil {
					entry.Dir = patternDir(expanded)
					entry.Error = err.Error()
					cfg.Entries = append(cfg.Entries, entry)
					continue
				}

				entry.Dir = cp.dir
				entry.Files, entry.Error = scanFiles(cp, opts)
				cfg.Entries = append(cfg.Entries, entry)
			}
		}
		report.Configs = append(report.Configs, cfg)
	}

	return report, nil
}

func ResolveConfDir(input string) (string, error) {
	if input == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		return filepath.Clean(filepath.Join(filepath.Dir(exe), "..", "conf")), nil
	}

	info, err := os.Stat(input)
	if err == nil && !info.IsDir() && isConfigName(input) {
		return filepath.Dir(input), nil
	}

	if err == nil && info.IsDir() {
		if hasConfigFile(input) {
			return input, nil
		}
		child := filepath.Join(input, "conf")
		if hasConfigFile(child) {
			return child, nil
		}
		return "", fmt.Errorf("no .ini or .json files in %s or %s", input, child)
	}

	if hasConfigFile(input) {
		return input, nil
	}
	child := filepath.Join(input, "conf")
	if hasConfigFile(child) {
		return child, nil
	}
	return "", fmt.Errorf("conf path not found: %s", input)
}

func ParseConfig(path string) ([]patternSpec, error) {
	name := strings.ToLower(path)
	switch {
	case strings.HasSuffix(name, ".ini"):
		return parseINIFile(path)
	case strings.HasSuffix(name, ".json"):
		return parseJSONFile(path)
	default:
		return nil, nil
	}
}

func parseINIFile(path string) ([]patternSpec, error) {
	ini, err := readINI(path)
	if err != nil {
		return nil, err
	}

	var specs []patternSpec
	for section, values := range ini {
		lowerSection := strings.ToLower(section)
		for key, value := range values {
			lowerKey := strings.ToLower(key)
			if value == "" {
				continue
			}
			switch {
			case lowerKey == "src_filepath":
				specs = appendSplit(specs, "IN", value)
			case lowerKey == "dst_filepath":
				specs = appendSplit(specs, "OUT", value)
			case strings.HasSuffix(lowerKey, "filepath_pattern"):
				label := strings.ToUpper(strings.TrimSuffix(lowerKey, "_filepath_pattern"))
				label = strings.TrimSuffix(label, "FILEPATH_PATTERN")
				if label == "" {
					label = strings.ToUpper(section)
				}
				specs = appendSplit(specs, label, value)
			case lowerSection == "action" && lowerKey == "write":
				for _, target := range splitList(value) {
					specs = appendActionSection(specs, getSection(ini, target))
				}
			}
		}
	}
	return specs, nil
}

func appendActionSection(specs []patternSpec, values map[string]string) []patternSpec {
	if len(values) == 0 {
		return specs
	}
	srcDir, srcFile := getValue(values, "SRC_DIR"), getValue(values, "SRC_FILE_PATTERN")
	dstDir, dstFile := getValue(values, "DST_DIR"), getValue(values, "DST_FILE_PATTERN")
	if srcDir != "" && srcFile != "" {
		specs = append(specs, patternSpec{Label: "IN", Pattern: joinPattern(srcDir, srcFile)})
	}
	if dstDir != "" && dstFile != "" {
		specs = append(specs, patternSpec{Label: "OUT", Pattern: joinPattern(dstDir, dstFile)})
	}
	return specs
}

func parseJSONFile(path string) ([]patternSpec, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root map[string]interface{}
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, err
	}

	var specs []patternSpec
	for jobName, raw := range root {
		if strings.HasPrefix(jobName, "--") {
			continue
		}
		job, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		output, ok := job["OUTPUT"].(map[string]interface{})
		if !ok {
			continue
		}
		if !equalsString(output["type"], "FILE") {
			continue
		}
		if paths, ok := output["paths"].(string); ok {
			specs = appendSplit(specs, "OUT", paths)
		}
	}
	return specs, nil
}

func readINI(path string) (map[string]map[string]string, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	result := make(map[string]map[string]string)
	section := ""
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.Contains(line, "]") {
			section = strings.TrimSpace(line[1:strings.Index(line, "]")])
			if _, ok := result[section]; !ok {
				result[section] = make(map[string]string)
			}
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := stripInlineComment(strings.TrimSpace(parts[1]))
		if _, ok := result[section]; !ok {
			result[section] = make(map[string]string)
		}
		result[section][key] = value
	}
	return result, nil
}

func getSection(ini map[string]map[string]string, name string) map[string]string {
	for section, values := range ini {
		if strings.EqualFold(section, name) {
			return values
		}
	}
	return nil
}

func getValue(values map[string]string, key string) string {
	for k, v := range values {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

func stripInlineComment(s string) string {
	inQuote := false
	for i, r := range s {
		switch r {
		case '"', '\'':
			inQuote = !inQuote
		case '#', ';':
			if !inQuote {
				return strings.TrimSpace(s[:i])
			}
		}
	}
	return strings.Trim(strings.TrimSpace(s), `"'`)
}

func appendSplit(specs []patternSpec, label, value string) []patternSpec {
	for _, p := range splitList(value) {
		specs = append(specs, patternSpec{Label: label, Pattern: p})
	}
	return specs
}

func splitList(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func scanFiles(cp compiledPattern, opts Options) ([]FileEntry, string) {
	entries, err := ioutil.ReadDir(cp.dir)
	if err != nil {
		return nil, err.Error()
	}

	var files []FileEntry
	for _, entry := range entries {
		if entry.IsDir() || ignored(entry.Name(), opts.Ignore) {
			continue
		}
		if !cp.fileExpr.MatchString(entry.Name()) {
			continue
		}
		tm, ok := extractTime(cp, entry.Name())
		if !ok {
			continue
		}
		files = append(files, FileEntry{
			Name: entry.Name(),
			Time: tm,
			Size: entry.Size(),
			Warn: opts.Now.Sub(tm) >= time.Duration(opts.Warn)*time.Minute,
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Time.After(files[j].Time)
	})
	if len(files) == 0 {
		return nil, "no matching files"
	}
	if len(files) > opts.Count {
		files = files[:opts.Count]
	}
	return files, ""
}

func compilePattern(full string) (compiledPattern, error) {
	dir, file := splitPathPattern(full)
	if dir == "" || file == "" {
		return compiledPattern{}, fmt.Errorf("invalid pattern: %s", full)
	}

	fileExpr, timeExpr, err := makeRegex(file)
	if err != nil {
		return compiledPattern{}, err
	}
	return compiledPattern{dir: dir, fileExpr: fileExpr, timeExpr: timeExpr}, nil
}

func makeRegex(pattern string) (*regexp.Regexp, *regexp.Regexp, error) {
	tokens := []string{"%Y", "%m", "%d", "%H", "%M"}
	expr := regexp.QuoteMeta(pattern)
	for _, token := range tokens {
		expr = strings.ReplaceAll(expr, regexp.QuoteMeta(token), tokenRegex(token))
	}

	if !strings.Contains(pattern, "%Y") || !strings.Contains(pattern, "%m") || !strings.Contains(pattern, "%d") {
		return nil, nil, fmt.Errorf("pattern has no supported date token")
	}

	baseExpr := expr
	if ext := filepath.Ext(pattern); ext != "" {
		extExpr := regexp.QuoteMeta(ext)
		baseExpr = strings.TrimSuffix(expr, extExpr)
	}
	fullExpr := "^" + baseExpr
	if ext := filepath.Ext(pattern); ext != "" {
		fullExpr += "(?:" + regexp.QuoteMeta(ext) + ")?"
	}
	fullExpr += "$"

	timeExpr, err := regexp.Compile(`\d{8}_\d{4}|\d{12}|\d{8}_\d{2}|\d{10}`)
	if err != nil {
		return nil, nil, err
	}
	fileExpr, err := regexp.Compile(fullExpr)
	if err != nil {
		return nil, nil, err
	}
	return fileExpr, timeExpr, nil
}

func tokenRegex(token string) string {
	switch token {
	case "%Y":
		return `(\d{4})`
	default:
		return `(\d{2})`
	}
}

func extractTime(cp compiledPattern, name string) (time.Time, bool) {
	m := cp.timeExpr.FindString(name)
	if m == "" {
		return time.Time{}, false
	}
	digits := regexp.MustCompile(`\d+`).FindAllString(m, -1)
	joined := strings.Join(digits, "")
	var tm time.Time
	var err error
	switch len(joined) {
	case 10:
		tm, err = time.ParseInLocation("2006010215", joined, time.Local)
	case 12:
		tm, err = time.ParseInLocation("200601021504", joined, time.Local)
	default:
		err = fmt.Errorf("unsupported time token")
	}
	return tm, err == nil
}

func expandAlternates(pattern string) []string {
	re := regexp.MustCompile(`\[([^\]]+)\]`)
	loc := re.FindStringSubmatchIndex(pattern)
	if loc == nil {
		return []string{pattern}
	}

	body := pattern[loc[2]:loc[3]]
	parts := strings.Split(body, "|")
	var out []string
	for _, part := range parts {
		next := pattern[:loc[0]] + part + pattern[loc[1]:]
		out = append(out, expandAlternates(next)...)
	}
	return out
}

func splitPathPattern(full string) (string, string) {
	idx := strings.LastIndex(full, "/")
	if b := strings.LastIndex(full, `\`); b > idx {
		idx = b
	}
	if idx < 0 {
		return ".", full
	}
	return full[:idx], full[idx+1:]
}

func patternDir(full string) string {
	dir, _ := splitPathPattern(full)
	return dir
}

func joinPattern(dir, file string) string {
	return strings.TrimRight(dir, `/\`) + "/" + strings.TrimLeft(file, `/\`)
}

func FormatSize(size int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	value := float64(size)
	i := 0
	for value >= 1024 && i < len(units)-1 {
		value /= 1024
		i++
	}
	if i == 0 {
		return strconv.FormatInt(size, 10) + " " + units[i]
	}
	return fmt.Sprintf("%.1f %s", value, units[i])
}

func listConfigFiles(dir string, ignore []string) ([]string, error) {
	entries, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !isConfigName(entry.Name()) || ignored(entry.Name(), ignore) {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	return files, nil
}

func hasConfigFile(dir string) bool {
	files, err := listConfigFiles(dir, nil)
	return err == nil && len(files) > 0
}

func isConfigName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".ini") || strings.HasSuffix(lower, ".json")
}

func ignored(name string, patterns []string) bool {
	for _, pattern := range patterns {
		ok, err := filepath.Match(pattern, name)
		if err == nil && ok {
			return true
		}
	}
	return false
}

func equalsString(v interface{}, want string) bool {
	s, ok := v.(string)
	return ok && strings.EqualFold(s, want)
}
