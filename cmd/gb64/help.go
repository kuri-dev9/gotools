package main

const helpText = `Usage:
  gb64 [options] <file>
  gb64 [options] -n <filename>
  gb64 encode [file]
  gb64 decode [file] [-o output]
  gb64 drop [options] [file]

Commands:
  encode  Write standard Base64 to stdout
  decode  Decode standard Base64 from a file or stdin
  drop    Explicitly create a B64DROP transfer

The default command is drop. Small payloads use B64DROP v1; larger payloads
automatically use interactive B64DROP v2 chunks.

Use "gb64 <command> --help" for command options.
`

const encodeHelp = `Usage: gb64 encode [file]

Reads file, or stdin when file is omitted, and writes standard Base64 to stdout.
`

const decodeHelp = `Usage: gb64 decode [file] [-o output]

Options:
  -o, --output <file>  Write decoded bytes to file (default: stdout)
  -h, --help           Show this help
`

const dropHelp = `Usage:
  gb64 [options] [file]
  gb64 drop [options] [file]

Options:
  -n, --name <name>       Logical filename; required for stdin
      --chunk-size <size> Maximum Base64 payload per chunk (default: 5M)
      --no-chunk          Force a single B64DROP v1 envelope
      --chunk <number>    Reprint one chunk from a valid file cache
      --verify-source     Recalculate source SHA-256 before cache reuse
  -h, --help              Show this help

The 5 MiB default is an initial value, not a universally verified PuTTY limit.
Adjust --chunk-size for the terminal and scrollback configuration in use.
`
