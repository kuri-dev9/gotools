package main

const helpText = `
gxfer - FTP/SFTP command and job runner

Usage:
  gxfer <command> [arguments] [options]

Commands:
  check [REMOTE_DIR]   Check connection, login, and remote access
  ls [REMOTE_DIR]      List remote files
  get REMOTE LOCAL     Download files once
  put LOCAL REMOTE     Upload files once
  del REMOTE_DIR       Delete matching remote files
  monitor -c FILE      Monitor and download new files
  run -c FILE          Run ACTION jobs in READ, WRITE, REMOVE order

Config policy:
  Only run and monitor accept -c/--config.

Options:
  -h, --help           Show help
  -v, --version        Show version

Run "gxfer <command> --help" for command options.
`

const commonHelp = `
Connection:
      --protocol <protocol>     ftp or sftp (default: sftp)
      --active                  Use FTP active mode
      --passive                 Use FTP passive mode (default)
      --host <host>             FTP/SFTP host
      --port <port>             Port (default: FTP 21, SFTP 22)
      --id <user>               FTP/SFTP username
      --pw <password>           Password; prompts securely when omitted
      --timeout <seconds>       Network timeout (default: 10)

Vault:
      --vault-host <host>       Credential host in Vault
      --vault-user <user>       Credential user in Vault
      --vault-resolve <host:ip> Add a Vault endpoint
      --vault-namespace <name>  Vault namespace
      --vault-kv <path>         Vault KV mount
`

func commandHelp(command string) string {
	if command == "run" {
		return `
gxfer run

Usage:
  gxfer run -c CONFIG.ini

Options:
  -c, --config <file>           ACTION INI config
  -h, --help                    Show help
`
	}
	if command == "monitor" {
		return `
gxfer monitor

Usage:
  gxfer monitor -c CONFIG.ini

The monitor command watches every section registered in ACTION.read.
FETCH_INTERVAL is the polling interval in seconds (default: 5).

Options:
  -c, --config <file>           ACTION INI config
  -h, --help                    Show help
`
	}

	var usage string
	var options string
	switch command {
	case "check":
		usage = "  gxfer check [REMOTE_DIR] [options]"
	case "ls":
		usage = "  gxfer ls [REMOTE_DIR] [options]"
		options = patternHelp
	case "get":
		usage = "  gxfer get REMOTE LOCAL_DIR [options]"
		options = patternHelp + `
      --rename <name>           Rename downloaded file
      --mode <mode>             skip-write, overwrite, error, or once
      --overwrite               Alias for --mode overwrite
      --skip-existing           Alias for --mode skip-write
      --delete                  Delete remote source after success
      --local-mkdir             Create local directory (default: true)
`
	case "put":
		usage = "  gxfer put LOCAL REMOTE_DIR [options]"
		options = patternHelp + `
      --rename <name>           Rename uploaded file
      --overwrite               Overwrite existing remote files
      --skip-existing           Skip existing remote files
      --delete                  Delete local source after success
      --remote-mkdir            Create remote directory (default: true)
`
	case "del":
		usage = "  gxfer del REMOTE_DIR [options]"
		options = patternHelp
	}
	return "\ngxfer " + command + "\n\nUsage:\n" + usage + "\n" + options + commonHelp + `
  -h, --help                    Show help
`
}

const patternHelp = `
File selection:
      --pattern <pattern>       Glob pattern (default: *)
      --regex                   Treat pattern as regular expression
`
