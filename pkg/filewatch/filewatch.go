package filewatch

import (
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
	ConfDir     string
	ProjectHome string
	Configs     []ConfigReport
}

type ConfigReport struct {
	Path    string
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
	baseDir  string
	dirExpr  *regexp.Regexp
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

	report := &Report{ConfDir: confDir, ProjectHome: projectHomeFromConfDir(confDir)}
	for _, file := range files {
		absFile, err := filepath.Abs(file)
		if err != nil {
			absFile = file
		}
		cfg := ConfigReport{Path: absFile}
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

				entry.Dir = cp.baseDir
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

	actionTargets := actionWriteTargets(ini)
	if len(actionTargets) > 0 {
		var specs []patternSpec
		for _, target := range actionTargets {
			specs = appendActionSection(specs, getSection(ini, target))
		}
		return specs, nil
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

func actionWriteTargets(ini map[string]map[string]string) []string {
	action := getSection(ini, "ACTION")
	if len(action) == 0 {
		return nil
	}
	return splitList(getValue(action, "write"))
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
	projectHome := projectHomeFromConfigPath(path)
	return parseLooseJSONText(stripQueryBlocks(string(b)), projectHome), nil
}

func stripQueryBlocks(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	skipQuery := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if skipQuery {
			if trimmed == `"` || trimmed == `",` || strings.HasSuffix(trimmed, `",`) {
				skipQuery = false
			}
			continue
		}
		if isJSONKeyLine(trimmed, "query") {
			if strings.Count(trimmed, `"`) < 4 {
				skipQuery = true
			}
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func parseLooseJSONText(s, projectHome string) []patternSpec {
	var specs []patternSpec
	for _, job := range findNamedObjectBlocks(s, "") {
		name := strings.TrimSpace(job.name)
		if strings.HasPrefix(name, "--") {
			continue
		}
		inputBlock := findFirstNamedBlock(job.body, "INPUT")
		outputBlock := findFirstNamedBlock(job.body, "OUTPUT")
		if outputBlock == "" || !blockStringEquals(outputBlock, "type", "FILE") {
			continue
		}
		paths := extractStringValue(outputBlock, "paths")
		if paths == "" {
			continue
		}
		vars := defaultVariables(projectHome)
		mergeVariables(vars, extractVariables(inputBlock))
		mergeVariables(vars, extractVariables(outputBlock))
		specs = appendSplit(specs, "OUT", substituteVariables(paths, vars))
	}
	if len(specs) == 0 {
		outputBlock := findFirstNamedBlock(s, "OUTPUT")
		if outputBlock != "" && blockStringEquals(outputBlock, "type", "FILE") {
			paths := extractStringValue(outputBlock, "paths")
			if paths != "" {
				vars := defaultVariables(projectHome)
				mergeVariables(vars, extractVariables(findFirstNamedBlock(s, "INPUT")))
				mergeVariables(vars, extractVariables(outputBlock))
				specs = appendSplit(specs, "OUT", substituteVariables(paths, vars))
			}
		}
	}
	return specs
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

func getJSONMap(parent map[string]interface{}, key string) map[string]interface{} {
	for k, v := range parent {
		if strings.HasPrefix(k, "--") {
			continue
		}
		if strings.EqualFold(k, key) {
			m, _ := v.(map[string]interface{})
			return m
		}
	}
	return nil
}

func getJSONString(parent map[string]interface{}, key string) string {
	for k, v := range parent {
		if strings.HasPrefix(k, "--") {
			continue
		}
		if strings.EqualFold(k, key) {
			s, _ := v.(string)
			return s
		}
	}
	return ""
}

func getJSONVariables(parent map[string]interface{}) map[string]string {
	vars := make(map[string]string)
	varBlock := getJSONMap(parent, "variable")
	for k, v := range varBlock {
		if strings.HasPrefix(k, "--") {
			continue
		}
		if s, ok := v.(string); ok {
			vars[k] = s
		}
	}
	return vars
}

func defaultVariables(projectHome string) map[string]string {
	return map[string]string{
		"LOADER_DIR": projectHome,
		"SCRIPT_DIR": projectHome,
	}
}

func mergeVariables(dst, src map[string]string) {
	for k, v := range src {
		if strings.HasPrefix(k, "--") {
			continue
		}
		dst[k] = v
	}
}

func substituteVariables(s string, vars map[string]string) string {
	re := regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	return re.ReplaceAllStringFunc(s, func(match string) string {
		key := match[2 : len(match)-1]
		if value, ok := vars[key]; ok {
			if isDateVariable(key) && isRuntimeArgValue(value) {
				return match
			}
			return value
		}
		return match
	})
}

func isDateVariable(key string) bool {
	switch strings.ToUpper(key) {
	case "DT", "HM", "YMD", "YMDH", "YMDHM":
		return true
	default:
		return false
	}
}

func isRuntimeArgValue(value string) bool {
	return regexp.MustCompile(`^\$\{ARGV\[[0-9]+\]\}$`).MatchString(strings.TrimSpace(value))
}

func projectHomeFromConfigPath(path string) string {
	return projectHomeFromConfDir(filepath.Dir(path))
}

func projectHomeFromConfDir(confDir string) string {
	if strings.EqualFold(filepath.Base(confDir), "conf") {
		return filepath.Dir(confDir)
	}
	return confDir
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

func leadingSpace(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

func isJSONKeyLine(line, key string) bool {
	re := regexp.MustCompile(`^"` + regexp.QuoteMeta(key) + `"\s*:`)
	return re.MatchString(line)
}

type namedBlock struct {
	name string
	body string
}

func findNamedObjectBlocks(s, parentKey string) []namedBlock {
	var blocks []namedBlock
	re := regexp.MustCompile(`"([^"]+)"\s*:\s*\{`)
	matches := re.FindAllStringSubmatchIndex(s, -1)
	for _, match := range matches {
		name := s[match[2]:match[3]]
		if parentKey != "" && !strings.EqualFold(name, parentKey) {
			continue
		}
		bodyStart := match[1] - 1
		bodyEnd := findMatchingBrace(s, bodyStart)
		if bodyEnd < 0 {
			continue
		}
		blocks = append(blocks, namedBlock{name: name, body: s[bodyStart : bodyEnd+1]})
	}
	return blocks
}

func findFirstNamedBlock(s, key string) string {
	blocks := findNamedObjectBlocks(s, key)
	if len(blocks) == 0 {
		return ""
	}
	return blocks[0].body
}

func findMatchingBrace(s string, start int) int {
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func blockStringEquals(block, key, want string) bool {
	return strings.EqualFold(extractStringValue(block, key), want)
}

func extractStringValue(block, key string) string {
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `"\s*:\s*"([^"]*)"`)
	matches := re.FindAllStringSubmatch(block, -1)
	for _, match := range matches {
		if len(match) == 2 {
			return match[1]
		}
	}
	return ""
}

func extractVariables(block string) map[string]string {
	vars := make(map[string]string)
	varBlock := findFirstNamedBlock(block, "variable")
	if varBlock == "" {
		return vars
	}
	re := regexp.MustCompile(`"([^"]+)"\s*:\s*"([^"]*)"`)
	for _, match := range re.FindAllStringSubmatch(varBlock, -1) {
		if len(match) != 3 || strings.HasPrefix(match[1], "--") {
			continue
		}
		vars[match[1]] = match[2]
	}
	return vars
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
	var files []FileEntry
	if cp.dirExpr == nil {
		if err := scanFilesInDir(cp.baseDir, "", cp, opts, &files); err != nil {
			return nil, err.Error()
		}
	} else {
		dirs, err := ioutil.ReadDir(cp.baseDir)
		if err != nil {
			return nil, err.Error()
		}
		for _, dir := range dirs {
			if !dir.IsDir() {
				continue
			}
			relDir := dir.Name()
			if !cp.dirExpr.MatchString(filepath.ToSlash(relDir)) {
				continue
			}
			_ = scanFilesInDir(filepath.Join(cp.baseDir, relDir), relDir, cp, opts, &files)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Time.After(files[j].Time) })
	if len(files) == 0 {
		return nil, "no matching files"
	}
	if len(files) > opts.Count {
		files = files[:opts.Count]
	}
	return files, ""
}

func scanFilesInDir(absDir, relDir string, cp compiledPattern, opts Options, files *[]FileEntry) error {
	entries, err := ioutil.ReadDir(absDir)
	if err != nil {
		return err
	}
	for _, f := range entries {
		if f.IsDir() || ignored(f.Name(), opts.Ignore) || !cp.fileExpr.MatchString(f.Name()) {
			continue
		}
		tm, ok := extractTime(cp, f.Name())
		if !ok {
			continue
		}
		name := f.Name()
		if relDir != "" {
			name = filepath.Join(relDir, f.Name())
		}
		*files = append(*files, FileEntry{Name: name, Time: tm, Size: f.Size(), Warn: opts.Now.Sub(tm) >= time.Duration(opts.Warn)*time.Minute})
	}
	return nil
}

func compilePattern(full string) (compiledPattern, error) {
	baseDir, dirPattern, filePattern := splitPathPattern(full)
	if baseDir == "" || filePattern == "" {
		return compiledPattern{}, fmt.Errorf("invalid pattern: %s", full)
	}
	var dirExpr *regexp.Regexp
	var err error
	if dirPattern != "" {
		dirExpr, _, err = makeRegex(filepath.ToSlash(dirPattern))
		if err != nil {
			return compiledPattern{}, err
		}
	}
	fileExpr, timeExpr, err := makeRegex(filePattern)
	if err != nil {
		return compiledPattern{}, err
	}
	return compiledPattern{baseDir: baseDir, dirExpr: dirExpr, fileExpr: fileExpr, timeExpr: timeExpr}, nil
}

func makeRegex(pattern string) (*regexp.Regexp, *regexp.Regexp, error) {
	tokens := []string{"%Y", "%m", "%d", "%H", "%M"}
	expr := regexp.QuoteMeta(pattern)
	for _, token := range tokens {
		expr = strings.ReplaceAll(expr, regexp.QuoteMeta(token), tokenRegex(token))
	}
	expr = replaceDateVariableTokens(pattern, expr)

	if !hasDateToken(pattern) {
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

	timeExpr, err := regexp.Compile(`\d{8}_\d{4}|\d{8}_\d{2}|\d{12}|\d{10}|\d{8}`)
	if err != nil {
		return nil, nil, err
	}
	fileExpr, err := regexp.Compile(fullExpr)
	if err != nil {
		return nil, nil, err
	}
	return fileExpr, timeExpr, nil
}

func hasDateToken(pattern string) bool {
	if strings.Contains(pattern, "%Y") && strings.Contains(pattern, "%m") && strings.Contains(pattern, "%d") {
		return true
	}
	return dateVariablePattern().MatchString(pattern)
}

func replaceDateVariableTokens(pattern, expr string) string {
	for _, token := range dateVariablePattern().FindAllString(pattern, -1) {
		expr = strings.ReplaceAll(expr, regexp.QuoteMeta(token), dateVariableRegex(token))
	}
	return expr
}

func dateVariablePattern() *regexp.Regexp {
	return regexp.MustCompile(`\$\{(?:DT|HM|YMD|YMDH|YMDHM|ARGV\[[0-9]+\])\}`)
}

func dateVariableRegex(token string) string {
	name := strings.TrimSuffix(strings.TrimPrefix(token, `${`), `}`)
	switch strings.ToUpper(name) {
	case "HM":
		return `(\d{2,4})`
	default:
		return `(\d{8,12})`
	}
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
	if cp.timeExpr == nil {
		return time.Time{}, false
	}
	m := cp.timeExpr.FindString(name)
	if m == "" {
		return time.Time{}, false
	}
	digits := regexp.MustCompile(`\d+`).FindAllString(m, -1)
	joined := strings.Join(digits, "")
	var tm time.Time
	var err error
	switch len(joined) {
	case 8:
		tm, err = time.ParseInLocation("20060102", joined, time.Local)
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

func splitPathPattern(pattern string) (baseDir, dirPattern, filePattern string) {
	pattern = filepath.Clean(pattern)
	filePattern = filepath.Base(pattern)
	dir := filepath.Dir(pattern)
	parts := strings.Split(filepath.ToSlash(dir), "/")
	idx := len(parts)

	for i, p := range parts {
		if strings.Contains(p, "%") ||
			strings.Contains(p, "${") ||
			strings.Contains(p, "*") ||
			strings.Contains(p, "?") {
			idx = i
			break
		}
	}

	if idx == len(parts) {
		baseDir = dir
		dirPattern = ""
	} else {
		baseDir = filepath.FromSlash(strings.Join(parts[:idx], "/"))
		dirPattern = filepath.FromSlash(strings.Join(parts[idx:], "/"))
	}

	if baseDir == "" {
		baseDir = string(filepath.Separator)
	}

	return
}

func patternDir(full string) string {
	dir, _, _ := splitPathPattern(full)
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
