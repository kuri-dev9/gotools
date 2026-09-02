# gsh

`gsh` is a small SSH client for Go 1.14 and Linux amd64. It uses the vendored
`golang.org/x/crypto/ssh` implementation directly and does not execute the
system `ssh`, OpenSSL, or NSS clients.

## Scope

Supported options are `-l/--login`, `-p/--port`, `-i/--identity`, `-t/--tty`,
`-v/--verbose`, `-h/--help`, and `-V/--version`. Version output uses the shared
`pkg/version` build metadata. With no command, `gsh` requests a PTY and starts
an interactive shell. Remaining arguments are joined with spaces and executed
as a remote command.

Remote commands do not allocate a PTY by default. `-t` forces one PTY and
applies local raw mode, the current rows/columns, terminal restoration, and
`SIGWINCH` resize forwarding to the remote command. Interactive shells always
allocate a PTY, whether or not `-t` is present. A local terminal (including an
Expect pseudo-terminal) is required when PTY allocation is used. Repeated `-t`
does not have OpenSSH `-tt` semantics.

Password authentication is prompted as `user@host's Password:` without echo.
The prompt intentionally matches the Expect pattern `*?assword:*`. Unencrypted
private keys and passphrase-protected key formats supported by the vendored SSH
package are accepted. Neither passwords nor private key contents are logged.
Password and key-passphrase input reuse the repository's shared
`pkg/auth.ReadPassword` terminal helper.

## Host key policy

Host key checking is mandatory. `gsh` reads and appends plain, exact host
entries in `~/.ssh/known_hosts`. A first connection displays the SHA-256
fingerprint and asks `Continue connecting (yes/no)?`, which is compatible with
the existing Expect pattern. An accepted key is appended with mode 0600; a
different known key is rejected.

This first implementation intentionally does not implement the complete
OpenSSH known_hosts grammar. Hashed hosts, wildcard/negated patterns, markers,
host certificates, revoked-key directives, aliases, and canonical-name/IP
cross-checks are ignored. If an existing file contains only a hashed entry for
a host, `gsh` treats that host as unknown and appends a plain entry after user
confirmation.

The vendored SSH source declares `ssh.KeyAlgoECDSA256` as
`ecdsa-sha2-nistp256` and includes it in the default host-key algorithms, so no
legacy algorithm is enabled and no dependency change is required.

## Interactive behavior

Interactive mode connects stdin/stdout/stderr, requests an `xterm` PTY, enters
local raw mode, and restores the saved terminal state on every return after raw
mode succeeds. `SIGWINCH` updates the remote PTY. An Expect-created pseudo
terminal is recognized by `x/term` and receives the same behavior. `gsh` does
not read `HOSTPW` and does not implement sudo/root automation.

## User-run build and verification

No build, test, binary execution, Expect execution, or SSH connection was
performed while implementing this feature. The user can later build with:

    make gsh

or, with Go 1.14 selected:

    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=vendor -o bin/gsh ./cmd/gsh

Suggested manual checks:

    gsh -h
    gsh --help
    gsh -V
    gsh --version
    gsh user@host
    gsh -l user host
    gsh -p 22 user@host
    gsh -i key user@host
    gsh user@host "hostname"
    gsh -t user@host "command requiring a terminal"

Verify password and private-key authentication, an
`ecdsa-sha2-nistp256`-only server, Ctrl+C, resize, logout, forced disconnect
terminal restoration, remote exit status, first-use acceptance, and mismatch
rejection. Also compare remote commands with and without `-t`, including
initial terminal size and resize forwarding. For Expect, replace only
`spawn ssh $user@$host` with
`spawn gsh $user@$host`, then verify password login, the user shell, `sudo -i`,
the sudo password, the root shell, and `interact`.

CentOS 6 runtime behavior, server interoperability, and Expect behavior remain
unverified until those user-run checks are completed.
