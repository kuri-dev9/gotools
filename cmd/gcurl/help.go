package main

const helpText = `
Options:
  -X, --request <method>        HTTP method (GET, POST, etc.)
  -d, --data <data>             Request body (or @file)
  -H, --header <header>         Custom header(s)
  -k, --insecure                Skip TLS verification
      --resolve <host:port:ip>  Custom DNS resolve
      --cacert <file>           CA certificate
      --cert <file>             Client certificate
      --key <file>              Client key

  -o, --output <file>           Write response to file
  -s, --silent                  Silent mode
  -v, --verbose                 Verbose output
  -i, --include                 Include response headers
  -L, --location                Follow redirects

      --retry <num>             Retry count
      --retry-delay <sec>       Retry delay
      --timeout <sec>           Timeout

  -w, --write-out <format>      Output formatting
  -h, --help                    Show this help
  -V, --version                 Show version

Examples:
  gcurl https://example.com
  gcurl -X POST -d "a=1&b=2" https://example.com
  gcurl -H "Authorization: Bearer token" https://api.example.com
`
