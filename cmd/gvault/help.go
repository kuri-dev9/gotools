package main

const helpText = `
gvault - retrieve credentials from Vault

Usage:
  gvault <command> [options]

Commands:
  get                 Get credentials for a host and user

Options:
  -h, --help          Show help
  -v, --version       Show version

Run "gvault get --help" for command options.
`

const getHelpText = `
gvault get - retrieve credentials for a host and user

Usage:
  gvault get HOST USER [options]

Options:
      --key <keys>         Print one value or selected keys (comma-separated)
      --namespace <name>   Vault namespace (default: prod_T-PANI)
      --kv <path>          KV mount path (default: tpanikv)
      --timeout <seconds>  Request timeout (default: 5)
      --resolve <host:ip>  Add a Vault endpoint (repeatable)
  -h, --help               Show help

Examples:
  gvault get sss-008 ftpuser_probe
  gvault get sss-008 ftpuser_probe --key password
  gvault get sss-008 ftpuser_probe --key ip,port,username
`
