package main

const helpText = `
gsh - small SSH client for interactive shells and remote commands

Usage:
  gsh [options] [user@]host [command...]

Options:
  -l, --login <user>       Login user
  -p, --port <port>        SSH port (default: 22)
  -i, --identity <file>    Private key file
  -t, --tty                Force PTY allocation for a remote command
  -v, --verbose            Print connection diagnostics
  -h, --help               Show help
  -V, --version            Show version

Host keys are verified using gsh's private ~/.gsh/known_hosts file.
New host keys are trusted automatically on first use. Changed host keys
require confirmation before the stored key is replaced.

Examples:
  gsh user@192.168.1.10
  gsh -l user 192.168.1.10
  gsh -p 2222 user@192.168.1.10
  gsh -i ~/.ssh/id_rsa user@192.168.1.10
  gsh user@192.168.1.10 "hostname"
  gsh -t user@192.168.1.10 "sudo systemctl status service"
`
