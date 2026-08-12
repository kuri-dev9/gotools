package main

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gtools/pkg/auth"
	"gtools/pkg/cli"
	"gtools/pkg/vault"
	"gtools/pkg/version"
	"gtools/pkg/xfer"

	"github.com/spf13/pflag"
)

const versionInfo = "1.2.3"

type commonOptions struct {
	protocol     string
	active       bool
	passive      bool
	host         string
	port         int
	username     string
	password     string
	vaultHost    string
	vaultUser    string
	vaultResolve []string
	namespace    string
	kvPath       string
	timeout      int
}

type commandContext struct {
	legacy xfer.LegacySettings
	config xfer.Config
	client xfer.Client
}

func init() {
	runtime.GOMAXPROCS(1)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		if _, reported := err.(reportedError); !reported {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
}

type reportedError struct {
	err error
}

func (e reportedError) Error() string {
	return e.err.Error()
}

func run(args []string) error {
	if len(args) == 0 {
		cli.PrintCustomHelp(helpText)
		return fmt.Errorf("missing command")
	}
	switch args[0] {
	case "help", "-h", "--help":
		cli.PrintCustomHelp(helpText)
		return nil
	case "version", "-v", "--version":
		version.Version = versionInfo
		version.Print()
		return nil
	case "check", "ls", "get", "put", "del":
		return runCommand(args[0], args[1:])
	case "monitor":
		return runConfigCommand(true, args[1:])
	case "run":
		return runConfigCommand(false, args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runCommand(command string, args []string) error {
	flags := pflag.NewFlagSet("gxfer "+command, pflag.ContinueOnError)
	flags.SetInterspersed(true)
	flags.SetOutput(os.Stderr)
	common := registerCommonFlags(flags)
	help := flags.BoolP("help", "h", false, "show help")

	pattern := flags.String("pattern", "*", "file pattern")
	useRegex := flags.Bool("regex", false, "treat pattern as regular expression")
	rename := flags.String("rename", "", "destination file name; {name} preserves source name")
	overwrite := flags.Bool("overwrite", false, "overwrite existing destination")
	skipExisting := flags.Bool("skip-existing", false, "skip existing destination")
	downloadMode := flags.String("mode", string(xfer.DownloadModeSkipWrite), "download mode (skip-write|overwrite|error|once)")
	deleteSource := flags.Bool("delete", false, "delete source after successful transfer")
	localMkdir := flags.Bool("local-mkdir", true, "create local destination directory")
	remoteMkdir := flags.Bool("remote-mkdir", true, "create remote destination directory")
	flags.Usage = func() { cli.PrintCustomHelp(commandHelp(command)) }

	if err := flags.Parse(args); err != nil {
		return err
	}
	if *help {
		cli.PrintCustomHelp(commandHelp(command))
		return nil
	}
	if err := validateCommandArgs(command, flags.Args()); err != nil {
		return err
	}

	context, err := prepareContext(flags, common, xfer.LegacySettings{}, true)
	if err != nil {
		if command == "check" {
			printCheckFailure(common, err)
			return reportedError{err: err}
		}
		return err
	}
	defer context.close()

	positionals := flags.Args()
	switch command {
	case "check":
		remoteDir, err := argument(positionals, 0, context.legacy.SourceDir, ".")
		if err != nil {
			return err
		}
		if err := context.client.Check(remoteDir); err != nil {
			printCheckResult(context.config, remoteDir, false, err)
			return reportedError{err: err}
		}
		printCheckResult(context.config, remoteDir, true, nil)
		return nil

	case "ls":
		remoteDir, err := argument(positionals, 0, context.legacy.SourceDir, ".")
		if err != nil {
			return err
		}
		applyPatternDefaults(flags, pattern, useRegex, context.legacy)
		files, err := context.client.List(xfer.ListOptions{Directory: remoteDir, Pattern: *pattern, Regex: *useRegex})
		if err != nil {
			return err
		}
		for _, file := range files {
			fmt.Println(file.Path)
		}
		return nil

	case "get":
		if len(positionals) < 2 {
			return fmt.Errorf("get requires REMOTE and LOCAL_DIR")
		}
		remote, err := argument(positionals, 0, context.legacy.SourceDir, "")
		if err != nil || remote == "" {
			return fmt.Errorf("remote path is required")
		}
		localDir, err := argument(positionals, 1, context.legacy.DestinationDir, ".")
		if err != nil {
			return err
		}
		applyPatternDefaults(flags, pattern, useRegex, context.legacy)
		if !flags.Changed("rename") && context.legacy.DestinationPattern != "" &&
			!strings.Contains(context.legacy.DestinationPattern, "%") {
			*rename = context.legacy.DestinationPattern
		}
		if !flags.Changed("delete") {
			*deleteSource = context.legacy.RemoveSource
		}
		renameFunc, err := legacyRename(flags, *rename, context.legacy)
		if err != nil {
			return err
		}
		mode := xfer.DownloadMode(strings.ToLower(strings.TrimSpace(*downloadMode)))
		if !flags.Changed("mode") {
			mode = context.legacy.DownloadMode
		}
		if *overwrite {
			mode = xfer.DownloadModeOverwrite
		}
		if *skipExisting {
			mode = xfer.DownloadModeSkipWrite
		}
		onceFile := context.legacy.CurrentSourceFile
		if mode == xfer.DownloadModeOnce && onceFile == "" {
			onceFile = xfer.ExpandTime(*pattern, time.Now().Add(time.Duration(context.legacy.Offset)*time.Minute))
		}
		effectivePattern, effectiveRegex := *pattern, *useRegex
		if mode == xfer.DownloadModeOnce {
			effectivePattern = onceFile
			effectiveRegex = false
		}
		results, err := context.client.Download(xfer.DownloadOptions{
			Remote: remote, LocalDir: localDir, Pattern: effectivePattern, Regex: effectiveRegex,
			Rename: *rename, RenameFunc: renameFunc,
			Mode: mode, OnceFile: onceFile,
			Overwrite: *overwrite, SkipExisting: *skipExisting,
			RemoveRemote: *deleteSource, CreateLocalDir: *localMkdir,
		})
		return printResults(results, err)

	case "put":
		if len(positionals) < 2 {
			return fmt.Errorf("put requires LOCAL and REMOTE_DIR")
		}
		local, err := argument(positionals, 0, context.legacy.SourceDir, "")
		if err != nil || local == "" {
			return fmt.Errorf("local path is required")
		}
		remoteDir, err := argument(positionals, 1, context.legacy.DestinationDir, ".")
		if err != nil {
			return err
		}
		applyPatternDefaults(flags, pattern, useRegex, context.legacy)
		if !flags.Changed("rename") && context.legacy.DestinationPattern != "" &&
			!strings.Contains(context.legacy.DestinationPattern, "%") {
			*rename = context.legacy.DestinationPattern
		}
		if !flags.Changed("delete") {
			*deleteSource = context.legacy.RemoveSource
		}
		renameFunc, err := legacyRename(flags, *rename, context.legacy)
		if err != nil {
			return err
		}
		results, err := context.client.Upload(xfer.UploadOptions{
			Local: local, RemoteDir: remoteDir, Pattern: *pattern, Regex: *useRegex,
			Rename: *rename, RenameFunc: renameFunc,
			Overwrite: *overwrite, SkipExisting: *skipExisting,
			RemoveLocal: *deleteSource, CreateRemote: *remoteMkdir,
		})
		return printResults(results, err)

	case "del":
		if len(positionals) < 1 {
			return fmt.Errorf("del requires REMOTE_DIR")
		}
		remoteDir, err := argument(positionals, 0, context.legacy.SourceDir, "")
		if err != nil || remoteDir == "" {
			return fmt.Errorf("remote directory is required")
		}
		applyPatternDefaults(flags, pattern, useRegex, context.legacy)
		removed, err := context.client.Remove(xfer.RemoveOptions{
			Directory: remoteDir, Pattern: *pattern, Regex: *useRegex,
		})
		if err != nil {
			return err
		}
		for _, name := range removed {
			fmt.Println(name)
		}
		return nil

	}
	return nil
}

func validateCommandArgs(command string, args []string) error {
	switch command {
	case "get", "put":
		if len(args) != 2 {
			return fmt.Errorf("%s requires exactly two arguments", command)
		}
	case "del":
		if len(args) != 1 {
			return fmt.Errorf("del requires exactly one REMOTE_DIR")
		}
	case "check", "ls":
		if len(args) > 1 {
			return fmt.Errorf("%s accepts at most one REMOTE_DIR", command)
		}
	}
	return nil
}

func normalizeLegacyArgs(args []string) []string {
	normalized := make([]string, len(args))
	for index, arg := range args {
		switch {
		case arg == "-config":
			normalized[index] = "--config"
		case strings.HasPrefix(arg, "-config="):
			normalized[index] = "--config=" + strings.TrimPrefix(arg, "-config=")
		default:
			normalized[index] = arg
		}
	}
	return normalized
}

func prepareContext(flags *pflag.FlagSet, common *commonOptions, legacy xfer.LegacySettings, connect bool) (*commandContext, error) {
	context := &commandContext{legacy: legacy}

	protocol := xfer.Protocol(strings.ToLower(common.protocol))
	if !flags.Changed("protocol") && context.legacy.Protocol != "" {
		protocol = context.legacy.Protocol
	}
	if common.active && common.passive {
		return nil, fmt.Errorf("--active and --passive cannot be used together")
	}
	ftpMode := xfer.FTPModePassive
	if context.legacy.FTPModeSet {
		ftpMode = context.legacy.FTPMode
	}
	if common.active {
		ftpMode = xfer.FTPModeActive
	}
	if common.passive {
		ftpMode = xfer.FTPModePassive
	}
	if protocol != xfer.ProtocolFTP && (common.active || common.passive) {
		return nil, fmt.Errorf("--active and --passive are available only with FTP")
	}

	host, port, username, password := context.legacy.Host, context.legacy.Port, context.legacy.Username, context.legacy.Password
	if flags.Changed("host") {
		host = common.host
	}
	if flags.Changed("port") {
		if common.port <= 0 || common.port > 65535 {
			return nil, fmt.Errorf("--port must be between 1 and 65535")
		}
		port = common.port
	}
	if flags.Changed("id") {
		username = common.username
	}
	if flags.Changed("pw") {
		password = common.password
	}

	vaultHost, vaultUser := common.vaultHost, common.vaultUser
	vaultNamespace, vaultKV := common.namespace, common.kvPath
	vaultResolve := common.vaultResolve
	useConfigVault := context.legacy.UseVault && !flags.Changed("host")
	if vaultHost == "" && vaultUser == "" && useConfigVault {
		vaultHost = context.legacy.ServerName
		vaultUser = context.legacy.VaultUser
		if vaultUser == "" {
			vaultUser = username
		}
		if !flags.Changed("vault-namespace") && context.legacy.VaultNamespace != "" {
			vaultNamespace = context.legacy.VaultNamespace
		}
		if !flags.Changed("vault-kv") && context.legacy.VaultKV != "" {
			vaultKV = context.legacy.VaultKV
		}
		if !flags.Changed("vault-resolve") {
			vaultResolve = context.legacy.VaultResolve
		}
	}

	if vaultHost != "" || vaultUser != "" {
		if vaultHost == "" || vaultUser == "" {
			return nil, fmt.Errorf("--vault-host and --vault-user must be used together")
		}
		credentials, err := vaultCredentials(vaultOptions{
			host: vaultHost, user: vaultUser, namespace: vaultNamespace,
			kvPath: vaultKV, resolve: vaultResolve, timeout: common.timeout,
		})
		if err != nil {
			return nil, err
		}
		if !flags.Changed("host") {
			host = credentials["ip"]
		}
		if !flags.Changed("id") {
			username = credentials["username"]
		}
		if !flags.Changed("pw") {
			password = credentials["password"]
		}
		if !flags.Changed("port") && credentials["port"] != "" {
			port, err = strconv.Atoi(credentials["port"])
			if err != nil {
				return nil, fmt.Errorf("Vault port is invalid: %w", err)
			}
			if port <= 0 || port > 65535 {
				return nil, fmt.Errorf("Vault port must be between 1 and 65535")
			}
		}
	}

	if host == "" || username == "" {
		return nil, fmt.Errorf("--host and --id are required when Vault is not used")
	}
	if password == "" {
		secret, err := auth.ReadPassword("Password: ")
		if err != nil {
			return nil, fmt.Errorf("read password: %w", err)
		}
		password = string(secret)
		clearBytes(secret)
	}

	if port == 0 {
		defaultPort, err := xfer.DefaultPortFor(protocol)
		if err != nil {
			return nil, err
		}
		port = defaultPort
	}
	clientConfig := xfer.Config{
		Protocol: protocol, Host: host, Port: port, Username: username,
		Password: password, Timeout: time.Duration(common.timeout) * time.Second,
		FTPMode: ftpMode,
	}
	if !flags.Changed("timeout") && context.legacy.Timeout > 0 {
		clientConfig.Timeout = time.Duration(context.legacy.Timeout) * time.Second
	}
	client, err := xfer.New(clientConfig)
	if err != nil {
		return nil, err
	}
	context.config = clientConfig
	context.client = client
	if !connect {
		return context, nil
	}
	if err := client.Connect(); err != nil {
		client.Close()
		return nil, err
	}
	return context, nil
}

func registerCommonFlags(flags *pflag.FlagSet) *commonOptions {
	options := &commonOptions{}
	flags.StringVar(&options.protocol, "protocol", string(xfer.ProtocolSFTP), "transfer protocol (ftp|sftp)")
	flags.BoolVar(&options.active, "active", false, "use FTP active mode")
	flags.BoolVar(&options.passive, "passive", false, "use FTP passive mode (default)")
	flags.StringVar(&options.host, "host", "", "FTP/SFTP host")
	flags.IntVar(&options.port, "port", 0, "FTP/SFTP port (default: 21 for FTP, 22 for SFTP)")
	flags.StringVar(&options.username, "id", "", "FTP/SFTP username")
	flags.StringVar(&options.password, "pw", "", "FTP/SFTP password")
	flags.StringVar(&options.vaultHost, "vault-host", "", "credential host in Vault")
	flags.StringVar(&options.vaultUser, "vault-user", "", "credential user in Vault")
	flags.StringArrayVar(&options.vaultResolve, "vault-resolve", nil, "Vault endpoint in host:ip form")
	flags.StringVar(&options.namespace, "vault-namespace", vault.DefaultNamespace, "Vault namespace")
	flags.StringVar(&options.kvPath, "vault-kv", vault.DefaultKVPath, "Vault KV mount")
	flags.IntVar(&options.timeout, "timeout", 10, "network timeout seconds")
	return options
}

type vaultOptions struct {
	host      string
	user      string
	namespace string
	kvPath    string
	resolve   []string
	timeout   int
}

func vaultCredentials(options vaultOptions) (map[string]string, error) {
	client := vault.New(
		vault.WithNamespace(options.namespace),
		vault.WithKVPath(options.kvPath),
		vault.WithTimeout(time.Duration(options.timeout)*time.Second),
	)
	for _, resolve := range options.resolve {
		parts := strings.SplitN(resolve, ":", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("invalid --vault-resolve %q (expected host:ip)", resolve)
		}
		client.AddResolve(parts[0], parts[1])
	}
	data, err := client.Get(options.host, options.user)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"ip", "username", "password"} {
		if data[key] == "" {
			return nil, fmt.Errorf("Vault credential key %q is missing", key)
		}
	}
	return data, nil
}

func applyPatternDefaults(flags *pflag.FlagSet, pattern *string, regex *bool, legacy xfer.LegacySettings) {
	if !flags.Changed("pattern") && legacy.SourcePattern != "" {
		*pattern = legacy.SourcePattern
	}
	if !flags.Changed("regex") {
		*regex = legacy.Regex
	}
}

func legacyRename(flags *pflag.FlagSet, rename string, legacy xfer.LegacySettings) (func(string) (string, error), error) {
	if flags.Changed("rename") || rename != "" {
		return nil, nil
	}
	return xfer.LegacyRename(legacy.SourceTemplate, legacy.DestinationPattern)
}

func argument(args []string, index int, fallback, defaultValue string) (string, error) {
	if len(args) > index {
		return args[index], nil
	}
	if fallback != "" {
		return fallback, nil
	}
	return defaultValue, nil
}

func printResults(results []xfer.Result, err error) error {
	if err != nil {
		return err
	}
	for _, result := range results {
		if !result.Skipped {
			fmt.Println(result.Destination)
		}
	}
	return nil
}

func (c *commandContext) close() {
	if c.client != nil {
		_ = c.client.Close()
	}
}

func clearBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

func printCheckFailure(options *commonOptions, err error) {
	protocol := strings.ToUpper(options.protocol)
	host := options.host
	if host == "" {
		host = "-"
	}
	port := options.port
	if port == 0 {
		port, _ = xfer.DefaultPortFor(xfer.Protocol(strings.ToLower(options.protocol)))
	}
	fmt.Printf("Protocol : %s\n", protocol)
	fmt.Printf("Host     : %s\n", formatHostPort(host, port))
	fmt.Println("Login    : FAILED")
	fmt.Println("Status   : FAILED")
	fmt.Printf("Reason   : %s\n", err)
}

func printCheckResult(config xfer.Config, remoteDir string, success bool, reason error) {
	fmt.Printf("Protocol : %s\n", strings.ToUpper(string(config.Protocol)))
	fmt.Printf("Host     : %s\n", formatHostPort(config.Host, config.Port))
	fmt.Println("Login    : OK")
	fmt.Printf("Remote   : %s\n", remoteDir)
	if success {
		fmt.Println("Status   : SUCCESS")
		return
	}
	fmt.Println("Status   : FAILED")
	fmt.Printf("Reason   : %s\n", reason)
}

func formatHostPort(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}
