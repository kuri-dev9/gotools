package main

import (
	"fmt"
	"os"
	"os/user"
	"runtime"
	"strconv"
	"strings"

	"gtools/pkg/cli"
	"gtools/pkg/gsh"
	"gtools/pkg/version"

	"github.com/spf13/pflag"
)

const version_info = "1.1.0"

func init() {
	runtime.GOMAXPROCS(1)
}

func main() {
	exitCode, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "gsh:", err)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func run(args []string) (int, error) {
	flags := pflag.NewFlagSet("gsh", pflag.ContinueOnError)
	flags.SetInterspersed(false)
	flags.SetOutput(os.Stderr)
	login := flags.StringP("login", "l", "", "login user")
	port := flags.IntP("port", "p", 22, "SSH port")
	identity := flags.StringP("identity", "i", "", "private key file")
	forcePTY := flags.BoolP("tty", "t", false, "force PTY allocation")
	verbose := flags.BoolP("verbose", "v", false, "verbose diagnostics")
	help := flags.BoolP("help", "h", false, "show help")
	versionFlag := flags.BoolP("version", "V", false, "show version")
	flags.Usage = func() { cli.PrintCustomHelp(helpText) }

	args = normalizeHelp(args)
	if err := flags.Parse(args); err != nil {
		return 2, err
	}
	if *help {
		cli.PrintCustomHelp(helpText)
		return 0, nil
	}
	if *versionFlag {
		version.Version = version_info
		version.Print()
		return 0, nil
	}
	positionals := flags.Args()
	if len(positionals) == 0 {
		cli.PrintCustomHelp(helpText)
		return 2, fmt.Errorf("host is required")
	}
	if *port < 1 || *port > 65535 {
		return 2, fmt.Errorf("port must be between 1 and 65535")
	}

	hostUser, host, err := splitDestination(positionals[0])
	if err != nil {
		return 2, err
	}
	if *login == "" {
		*login = hostUser
	}
	if *login == "" {
		current, userErr := user.Current()
		if userErr != nil || current.Username == "" {
			return 2, fmt.Errorf("login user is required (use -l or user@host)")
		}
		*login = current.Username
	}

	config := gsh.Config{
		Host: host, Port: *port, User: *login, IdentityFile: *identity,
		ForcePTY: *forcePTY, Verbose: *verbose,
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
	}
	command := ""
	if len(positionals) > 1 {
		command = strings.Join(positionals[1:], " ")
	}
	return gsh.Run(config, command)
}

func normalizeHelp(args []string) []string {
	if len(args) == 1 && args[0] == "--help" {
		return []string{"-h"}
	}
	return args
}

func splitDestination(value string) (string, string, error) {
	if strings.TrimSpace(value) == "" {
		return "", "", fmt.Errorf("host is required")
	}
	parts := strings.Split(value, "@")
	if len(parts) > 2 || len(parts) == 2 && (parts[0] == "" || parts[1] == "") {
		return "", "", fmt.Errorf("invalid destination %q", value)
	}
	if len(parts) == 2 {
		return parts[0], parts[1], nil
	}
	if _, err := strconv.Atoi(value); err == nil {
		return "", value, nil
	}
	return "", value, nil
}
