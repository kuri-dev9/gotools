package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"gtools/pkg/auth"
	"gtools/pkg/cli"
	"gtools/pkg/vault"
	"gtools/pkg/version"

	"github.com/spf13/pflag"
)

const versionInfo = "1.0.0"

func init() {
	runtime.GOMAXPROCS(1)
}

func main() {
	args, noAuth := extractNoAuth(os.Args[1:])
	if err := run(args, noAuth); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// extractNoAuth handles the intentionally undocumented development option
// before normal command and flag parsing.
func extractNoAuth(args []string) ([]string, bool) {
	filtered := make([]string, 0, len(args))
	noAuth := false
	for _, arg := range args {
		if arg == "--no-auth" {
			noAuth = true
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered, noAuth
}

func run(args []string, noAuth bool) error {
	if len(args) == 0 {
		cli.PrintCustomHelp(helpText)
		return fmt.Errorf("missing command")
	}
	switch args[0] {
	case "get":
		if isHelpRequest(args[1:]) {
			cli.PrintCustomHelp(getHelpText)
			return nil
		}
		if !noAuth {
			if err := auth.Authenticate(); err != nil {
				return err
			}
		}
		return runGet(args[1:])
	case "help", "-h", "--help":
		cli.PrintCustomHelp(helpText)
		return nil
	case "version", "-v", "--version":
		version.Version = versionInfo
		version.Print()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func isHelpRequest(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func runGet(args []string) error {
	flags := pflag.NewFlagSet("gvault get", pflag.ContinueOnError)
	flags.SetInterspersed(true)
	flags.SetOutput(os.Stderr)
	keys := flags.String("key", "", "key or comma-separated keys")
	namespace := flags.String("namespace", vault.DefaultNamespace, "Vault namespace")
	kvPath := flags.String("kv", vault.DefaultKVPath, "KV mount path")
	timeout := flags.Int("timeout", int(vault.DefaultTimeout/time.Second), "timeout seconds")
	resolves := flags.StringArray("resolve", nil, "Vault endpoint in host:ip form")
	help := flags.BoolP("help", "h", false, "show help")
	flags.Usage = func() { cli.PrintCustomHelp(getHelpText) }

	if err := flags.Parse(args); err != nil {
		return err
	}
	if *help {
		cli.PrintCustomHelp(getHelpText)
		return nil
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("usage: gvault get HOST USER [--key key[,key...]]")
	}
	if *timeout <= 0 {
		return fmt.Errorf("--timeout must be greater than zero")
	}

	client := vault.New(
		vault.WithNamespace(*namespace),
		vault.WithKVPath(*kvPath),
		vault.WithTimeout(time.Duration(*timeout)*time.Second),
	)
	for _, value := range *resolves {
		parts := strings.SplitN(value, ":", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return fmt.Errorf("invalid --resolve %q (expected host:ip)", value)
		}
		client.AddResolve(parts[0], parts[1])
	}

	data, err := client.Get(flags.Arg(0), flags.Arg(1))
	if err != nil {
		return err
	}
	return printData(data, *keys)
}

func printData(data map[string]string, keyList string) error {
	if strings.TrimSpace(keyList) == "" {
		return printJSON(data)
	}

	var keys []string
	for _, key := range strings.Split(keyList, ",") {
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("empty key in --key")
		}
		if _, ok := data[key]; !ok {
			return fmt.Errorf("key %q not found", key)
		}
		keys = append(keys, key)
	}
	if len(keys) == 1 {
		fmt.Println(data[keys[0]])
		return nil
	}

	selected := make(map[string]string, len(keys))
	for _, key := range keys {
		selected[key] = data[key]
	}
	return printJSON(selected)
}

func printJSON(data map[string]string) error {
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode output: %w", err)
	}
	fmt.Println(string(out))
	return nil
}
