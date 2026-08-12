package xfer

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// LegacySettings contains values mapped from sftp_controller.py INI files.
type LegacySettings struct {
	Protocol           Protocol
	Host               string
	ServerName         string
	VaultUser          string
	Port               int
	Username           string
	Password           string
	UseVault           bool
	VaultNamespace     string
	VaultKV            string
	VaultResolve       []string
	FTPMode            FTPMode
	FTPModeSet         bool
	SourceDir          string
	SourcePattern      string
	SourceTemplate     string
	DestinationDir     string
	DestinationPattern string
	Regex              bool
	RemoveSource       bool
	Offset             int
	FetchInterval      int
	Timeout            int
	DownloadMode       DownloadMode
	CurrentSourceFile  string
}

// LoadLegacyINI loads one action section and its DEFAULT_* fallback. If
// section is empty, the first section named by ACTION for the command is used.
func LoadLegacyINI(filename, command, section string, now time.Time) (LegacySettings, error) {
	sections, err := readINI(filename)
	if err != nil {
		return LegacySettings{}, err
	}
	action, defaultSection := legacyAction(command)
	if section == "" {
		section = firstCSV(sections["ACTION"][action])
		if section == "" {
			section = defaultSection
		}
	}

	values := mergeINI(sections[defaultSection], sections[strings.ToUpper(section)])
	if len(values) == 0 {
		return LegacySettings{}, fmt.Errorf("INI section %q not found", section)
	}

	port, err := optionalInt(values["PORT"])
	if err != nil {
		return LegacySettings{}, fmt.Errorf("invalid PORT: %w", err)
	}
	offset, err := optionalInt(values["OFFSET"])
	if err != nil {
		return LegacySettings{}, fmt.Errorf("invalid OFFSET: %w", err)
	}
	fetchInterval, err := optionalInt(firstValue(values, "FETCH_INTERVAL", "INTERVAL"))
	if err != nil || fetchInterval < 0 {
		return LegacySettings{}, fmt.Errorf("invalid FETCH_INTERVAL %q", firstValue(values, "FETCH_INTERVAL", "INTERVAL"))
	}
	if fetchInterval == 0 {
		fetchInterval = 5
	}
	timeout, err := optionalInt(values["TIMEOUT"])
	if err != nil || timeout < 0 {
		return LegacySettings{}, fmt.Errorf("invalid TIMEOUT %q", values["TIMEOUT"])
	}
	regex, err := optionalBool(values["USE_REGULAR_EXPRESSION"])
	if err != nil {
		return LegacySettings{}, fmt.Errorf("invalid USE_REGULAR_EXPRESSION: %w", err)
	}
	remove, err := optionalBool(values["REMOVE"])
	if err != nil {
		return LegacySettings{}, fmt.Errorf("invalid REMOVE: %w", err)
	}
	downloadMode, err := normalizeDownloadMode(DownloadMode(strings.ToLower(
		firstValue(values, "DOWNLOAD_MODE", "EXISTING_MODE"),
	)))
	if err != nil {
		return LegacySettings{}, err
	}
	useCurrentTimeFile, err := optionalBool(values["USE_CURRENT_TIME_FILE"])
	if err != nil {
		return LegacySettings{}, fmt.Errorf("invalid USE_CURRENT_TIME_FILE: %w", err)
	}
	if useCurrentTimeFile {
		downloadMode = DownloadModeOnce
	}
	useVault, err := optionalBool(values["VAULT"])
	if err != nil {
		return LegacySettings{}, fmt.Errorf("invalid VAULT: %w", err)
	}
	if firstValue(values, "VAULT_HOSTNAME", "SERVER_NAME") != "" {
		useVault = true
	}
	ftpMode := FTPModePassive
	ftpModeSet := false
	passiveValue := firstValue(values, "FTP_PASSIVE", "PASSIVE")
	if passiveValue != "" {
		passive, err := optionalBool(passiveValue)
		if err != nil {
			return LegacySettings{}, fmt.Errorf("invalid FTP_PASSIVE: %w", err)
		}
		ftpModeSet = true
		if !passive {
			ftpMode = FTPModeActive
		}
	}

	password := firstValue(values, "PW", "USER_PW")
	encoded, err := optionalBool(values["ENCODE"])
	if err != nil {
		return LegacySettings{}, fmt.Errorf("invalid ENCODE: %w", err)
	}
	if encoded && password != "" {
		decoded, err := base64.StdEncoding.DecodeString(password)
		if err != nil {
			return LegacySettings{}, fmt.Errorf("decode USER_PW: %w", err)
		}
		password = string(decoded)
	}

	when := now.Add(time.Duration(offset) * time.Minute)
	sourcePattern := values["SRC_FILE_PATTERN"]
	if regex {
		sourcePattern = TimePatternRegex(sourcePattern)
	} else {
		sourcePattern = ExpandTime(sourcePattern, when)
	}
	return LegacySettings{
		Protocol: Protocol(strings.ToLower(values["PROTOCOL"])),
		Host:     firstValue(values, "HOSTNAME", "SERVER_IP", "HOST"),
		Port:     port, Username: firstValue(values, "USER", "USER_ID"), Password: password,
		ServerName: firstValue(values, "VAULT_HOSTNAME", "SERVER_NAME"),
		VaultUser:  firstValue(values, "VAULT_USER"), UseVault: useVault,
		VaultNamespace: values["VAULT_NAMESPACE"], VaultKV: values["VAULT_KV"],
		VaultResolve: splitCSV(values["VAULT_RESOLVE"]),
		FTPMode:      ftpMode, FTPModeSet: ftpModeSet,
		SourceDir: ExpandTime(values["SRC_DIR"], when), SourcePattern: sourcePattern,
		SourceTemplate:     values["SRC_FILE_PATTERN"],
		DestinationDir:     ExpandTime(values["DST_DIR"], when),
		DestinationPattern: values["DST_FILE_PATTERN"],
		Regex:              regex, RemoveSource: remove, Offset: offset,
		DownloadMode: downloadMode, CurrentSourceFile: ExpandTime(values["SRC_FILE_PATTERN"], when),
		FetchInterval: fetchInterval, Timeout: timeout,
	}, nil
}

func normalizeDownloadMode(mode DownloadMode) (DownloadMode, error) {
	if mode == "" {
		return DownloadModeSkipWrite, nil
	}
	switch mode {
	case DownloadModeSkipWrite, DownloadModeOverwrite, DownloadModeError, DownloadModeOnce:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid MODE %q (expected skip-write, overwrite, error, or once)", mode)
	}
}

// ExpandTime expands Python-compatible filename time directives.
func ExpandTime(value string, when time.Time) string {
	replacer := strings.NewReplacer(
		"%Y", when.Format("2006"), "%m", when.Format("01"), "%d", when.Format("02"),
		"%H", when.Format("15"), "%M", when.Format("04"), "%S", when.Format("05"),
	)
	return replacer.Replace(value)
}

// TimePatternRegex converts Python time directives to regex fragments while
// retaining existing regular expression syntax used by legacy configurations.
func TimePatternRegex(value string) string {
	replacer := strings.NewReplacer(
		"%Y", `([0-9]{4})`, "%m", `([0-9]{2})`, "%d", `([0-9]{2})`,
		"%H", `([0-9]{2})`, "%M", `([0-9]{2})`, "%S", `([0-9]{2})`,
		"*", `([0-9A-Za-z _+><-]*)`,
	)
	return replacer.Replace(value)
}

// LegacyRename returns a filename converter compatible with the time and
// wildcard placeholders used by sftp_controller.py.
func LegacyRename(sourceTemplate, destinationTemplate string) (func(string) (string, error), error) {
	if sourceTemplate == "" || destinationTemplate == "" || sourceTemplate == destinationTemplate {
		return nil, nil
	}
	expression, tokens := legacyTemplateRegex(sourceTemplate)
	compiled, err := regexp.Compile("^" + expression + "$")
	if err != nil {
		return nil, fmt.Errorf("invalid legacy source pattern: %w", err)
	}
	return func(name string) (string, error) {
		matches := compiled.FindStringSubmatch(name)
		if matches == nil {
			return "", fmt.Errorf("filename %q does not match legacy pattern %q", name, sourceTemplate)
		}
		values := make(map[string][]string)
		for index, token := range tokens {
			values[token] = append(values[token], matches[index+1])
		}
		used := make(map[string]int)
		var output strings.Builder
		for index := 0; index < len(destinationTemplate); {
			token := templateToken(destinationTemplate[index:])
			if token == "" {
				output.WriteByte(destinationTemplate[index])
				index++
				continue
			}
			position := used[token]
			if position >= len(values[token]) {
				return "", fmt.Errorf("destination pattern %q requires unavailable %s value", destinationTemplate, token)
			}
			output.WriteString(values[token][position])
			used[token] = position + 1
			index += len(token)
		}
		return output.String(), nil
	}, nil
}

func legacyTemplateRegex(template string) (string, []string) {
	var expression strings.Builder
	var literal strings.Builder
	var tokens []string
	flushLiteral := func() {
		expression.WriteString(regexp.QuoteMeta(literal.String()))
		literal.Reset()
	}
	for index := 0; index < len(template); {
		token := templateToken(template[index:])
		if token == "" {
			literal.WriteByte(template[index])
			index++
			continue
		}
		flushLiteral()
		switch token {
		case "%Y":
			expression.WriteString(`([0-9]{4})`)
		case "*":
			expression.WriteString(`(.*)`)
		default:
			expression.WriteString(`([0-9]{2})`)
		}
		tokens = append(tokens, token)
		index += len(token)
	}
	flushLiteral()
	return expression.String(), tokens
}

func templateToken(value string) string {
	for _, token := range []string{"%Y", "%m", "%d", "%H", "%M", "%S", "*"} {
		if strings.HasPrefix(value, token) {
			return token
		}
	}
	return ""
}

func readINI(filename string) (map[string]map[string]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open config %q: %w", filename, err)
	}
	defer file.Close()

	sections := make(map[string]map[string]string)
	current := "JOB"
	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if comment := strings.Index(line, "#"); comment >= 0 {
			line = strings.TrimSpace(line[:comment])
		}
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.ToUpper(strings.TrimSpace(line[1 : len(line)-1]))
			if current == "" {
				return nil, fmt.Errorf("config line %d: empty section", lineNumber)
			}
			if sections[current] == nil {
				sections[current] = make(map[string]string)
			}
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			parts = strings.SplitN(line, ":", 2)
		}
		if len(parts) != 2 {
			return nil, fmt.Errorf("config line %d: expected key=value", lineNumber)
		}
		if sections[current] == nil {
			sections[current] = make(map[string]string)
		}
		sections[current][strings.ToUpper(strings.TrimSpace(parts[0]))] = strings.TrimSpace(parts[1])
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return sections, nil
}

func legacyAction(command string) (string, string) {
	switch command {
	case "put":
		return "WRITE", "DEFAULT_WRITE"
	case "rm":
		return "REMOVE", "DEFAULT_REMOVE"
	default:
		return "READ", "DEFAULT_READ"
	}
}

func mergeINI(base, override map[string]string) map[string]string {
	result := make(map[string]string, len(base)+len(override))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range override {
		result[key] = value
	}
	return result
}

func firstValue(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func firstCSV(value string) string {
	if index := strings.Index(value, ","); index >= 0 {
		value = value[:index]
	}
	return strings.ToUpper(strings.TrimSpace(value))
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func optionalInt(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	return strconv.Atoi(value)
}

func optionalBool(value string) (bool, error) {
	if value == "" {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("expected boolean, got %q", value)
	}
}
