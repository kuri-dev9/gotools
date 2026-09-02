# gtools Development Guide

## 1. Purpose and authority

This is the repository-level development guideline for `gtools`. Read it
before adding a tool, changing a command, moving code into `pkg`, changing a
dependency, or editing a build definition.

The guide records the conventions actually present in this repository and the
compatibility constraints under which it is deployed. Tool-specific protocol,
security, or operation documents under `docs/` remain authoritative for their
own feature. If a task-specific requirement conflicts with this guide, make
the conflict explicit and follow the narrower approved requirement; do not
silently invent a third convention.

This repository contains some mixed and legacy patterns. Sections labelled
**Required** are the baseline for new work. Sections labelled **Existing
variation** describe code that must not be copied blindly or normalized as an
unrelated side effect.

## 2. Compatibility baseline

### Required

- Go language and standard-library baseline: **Go 1.14** (`go.mod` declares
  `go 1.14`).
- Primary server target: **CentOS 6, Linux amd64**.
- Linux builds default to `CGO_ENABLED=0`, `GOOS=linux`, and `GOARCH=amd64` in
  the root `Makefile`.
- Use syntax and APIs that existed in Go 1.14. In particular, do not use
  generics, `any`, type sets/unions, `//go:embed`, or later standard-library
  helpers such as `os.ReadFile`, `io.ReadAll`, `strings.Cut`, or
  `signal.NotifyContext`.
- Check compatibility against the source and vendored version in this
  repository, not against current online documentation.
- Avoid OS facilities and dynamically linked system libraries unavailable on
  CentOS 6. Prefer standard library or vendored pure-Go implementations where
  they meet the requirement.
- Keep platform-specific implementations behind Go 1.14-compatible
  `// +build` constraints. The Windows B64Drop code demonstrates this form.

Windows is a supported target only for the explicitly listed Windows outputs;
it is not an implicit requirement for every Linux command. A new Windows
target needs an explicit requirement, platform review, and build-script entry.

## 3. Distribution philosophy

`gtools` is a collection of small operational utilities intended to be copied
to old or restricted systems with little installation work.

- Prefer one executable per tool under `bin/`.
- Minimize interpreters, services, daemons, external commands, and target-host
  runtime dependencies.
- Do not wrap a system command when the requirement calls for an independent
  implementation.
- Do not claim that every tool is fully static merely because the default
  build uses `CGO_ENABLED=0`; confirm the dependency and platform behavior for
  the specific tool.
- Keep the scope at the smallest reliable solution to the current operating
  problem. Reliability, resource cleanup, error reporting, and security are
  not optional PoC features.

## 4. Repository map

```text
cmd/<tool>/       CLI entry points and tool-local presentation/orchestration
pkg/<package>/    reusable helpers or feature engines
docs/             protocol and tool-specific operational documentation
windows/          explicitly Windows-specific applications
vendor/           checked-in dependency source used by default builds
bin/              ignored build output
Makefile          primary Linux build definition
WindowMakefile.bat  selected Windows builds
go.mod/go.sum     module definition and dependency checksums
```

Current Linux commands are `gb64`, `gcurl`, `gkafka`, `gnicstat`, `gnode`,
`gsh`, `gtree`, `gvault`, `gwatch`, and `gxfer`. Every current command has a
dedicated `cmd/<tool>/help.go`; `gb64` and `gxfer` also have additional command
or job source files.

There is no repository-root README and no project-owned `*_test.go` file at
the time this guide was written. Do not interpret absence of tests as evidence
that behavior is verified or as a rule against adding tests in a separately
approved task.

## 5. Tool structure

### Required for a new command

```text
cmd/<g-prefixed-name>/
  main.go
  help.go
```

- Keep argument parsing, stream selection, help/version dispatch, and process
  exit handling in `cmd/<tool>`.
- Put substantial domain logic in an appropriate `pkg` engine when that makes
  ownership, reuse, or resource handling clearer. `cmd/gb64` with
  `pkg/b64drop`, `cmd/gkafka` with `pkg/kafka`, and `cmd/gxfer` with `pkg/xfer`
  are current examples.
- A small command may remain mostly in `main.go`; do not create interfaces and
  packages solely because another tool might hypothetically reuse them.
- Keep `help.go` separate even for a small command. Put large subcommand help
  in that file as `commandHelp` or named help constants.
- Use the `g...` executable name consistently in the directory, help,
  Makefile target, output filename, examples, and documentation.
- Prefer a `run(args)` function returning an error (and, only when required,
  an exit status) so `main` owns final stderr output and `os.Exit`. Recent
  `gb64`, `gkafka`, `gvault`, `gxfer`, and `gsh` code follows this shape.

### Existing variation

Older commands use package-global flags and perform work and `os.Exit` directly
inside `main`. Do not refactor them merely while adding another tool. Some
commands call `runtime.GOMAXPROCS(1)`, while `pkg/cli` has a separate CPU-count
policy. This is not consistent enough to impose as a universal rule; match the
nearest analogous tool unless concurrency is part of the design, and document
the reason for a new policy.

## 6. Common packages and ownership

Search both `pkg/` and existing `cmd/` code before implementing a helper.

| Package | Current responsibility | Current consumers / reuse guidance |
|---|---|---|
| `pkg/auth` | Echo-free terminal password input and local secondary authentication | Used by `gvault`, `gxfer`, and `pkg/gsh`. Reuse `ReadPassword` for terminal secrets instead of duplicating terminal echo handling. `Authenticate` is the repository-specific local allowlist check; do not apply it to unrelated remote authentication. |
| `pkg/b64drop` | B64DROP protocol, chunking, validation, restore, and temporary transfer storage | Shared by `cmd/gb64` and the Windows B64Drop application. It is the protocol engine and `docs/B64DROP.md` is the protocol source of truth. |
| `pkg/cli` | Help-print helpers; also declares `cli.Log` and a runtime CPU policy | Help helpers are used by many commands. `cli.Log`/logrus currently has no command consumer, so logging through it is not an established repository-wide convention. |
| `pkg/fileutil` | Tree node/build helpers | Currently not imported by a command. Inspect before duplicating tree traversal, but do not assume an unused API is the current `gtree` engine. |
| `pkg/filewatch` | Config discovery/parsing and file-watch reports | Engine for `gwatch`. |
| `pkg/gsh` | SSH authentication, host-key policy, sessions, PTY, and resize handling | Engine for `gsh`; it reuses `pkg/auth`. Its strict host-key policy is not interchangeable with the SFTP compatibility policy in `pkg/xfer`. |
| `pkg/hwinfo` | Linux hardware and usage collection | Engine for `gnode`. |
| `pkg/kafka` | Kafka parsing, client creation, metadata, offsets, and safe output support | Engine for `gkafka`. |
| `pkg/netutil` | Custom resolve parsing, TLS configuration, and HTTP transport creation | Used by `gcurl`; inspect it before adding HTTP/TLS/resolve code. |
| `pkg/nicstat` | Linux NIC/protocol statistics | Engine for `gnicstat`; reuses `pkg/tui`. |
| `pkg/tui` | Color and table presentation | Used by `gnode`, `gnicstat`, and `gwatch`. Keep presentation out of machine-readable modes. |
| `pkg/vault` | HashiCorp Vault credential client, endpoint resolution, and response decoding | Shared by `gvault` and `gxfer`. Reuse rather than creating another Vault HTTP client. |
| `pkg/version` | Tool version plus injected Git/build metadata and runtime/OS information | Used by all current command entry points. Mandatory for a new versioned command. |
| `pkg/xfer` | FTP/SFTP clients, legacy INI parsing, jobs, patterns, and monitoring | Engine for `gxfer`. It contains SSH transport code for SFTP but is not a general SSH package. |

### Reuse decision

Before adding code, check in this order:

1. Is it already provided by the Go 1.14 standard library?
2. Is there an existing repository package with the same responsibility?
3. Is similar code tool-specific because its policy differs?
4. Do at least two real consumers need one stable responsibility?
5. Would extraction reduce policy duplication rather than merely line count?

If similar logic exists in multiple places but has not been made common, do
not silently refactor every consumer as part of an unrelated task. Report the
duplication and request scope when the change affects behavior or policy.
Examples currently requiring care are SSH configuration (`pkg/gsh` versus
`pkg/xfer/sftp.go`) and local byte clearing, which appears in more than one
scope but has no exported common helper.

## 7. Version management

There is no single repository-wide product version. Each command owns a
semantic-looking version string in its `main` package, then assigns it to
`pkg/version.Version` before calling `version.Print()`.

The printed information is:

```text
Version
GitCommit
BuildDate
GoVersion
OS/Arch
```

The Linux `Makefile` injects `pkg/version.GitCommit` and
`pkg/version.BuildDate` using `-ldflags -X`. `WindowMakefile.bat` injects the
same metadata for `gb64.exe` and `gcurl.exe`. The tool-owned version is set in
source, not injected by the current build definitions.

### Required

- Give a new command its own source version and use `pkg/version` for output.
- Increment the tool version when behavior or CLI surface changes. Use the
  smallest SemVer-style increment appropriate to compatibility; do not change
  other tool versions.
- Preserve `GitCommit` and `BuildDate` injection when adding a Makefile or
  Windows build target.
- Document the version option in `help.go`.

### Existing variation

- Constant names are mixed: `version_info` and `versionInfo` both exist.
- Most older tools use `-v` for version; tools where `-v` means verbose, such
  as `gcurl` and `gsh`, use `-V` for version. `gb64` also uses `-V`.
- Some subcommand tools accept a `version` command in addition to flags.

Do not rename existing constants or flags merely for visual consistency. For a
new tool, reserve `-v` for verbose when verbose output is plausible and use
`-V/--version`; otherwise follow the closest tool without creating a flag
collision. Always support the long name `--version` when a version option is
provided.

## 8. CLI conventions

All current commands use vendored `github.com/spf13/pflag`.

### Required

- Use `pflag`; do not introduce a second CLI framework for one command.
- Support `-h/--help` and make help a successful operation.
- Validate required arguments, argument counts, numeric bounds, incompatible
  flags, and unexpected positionals before performing external work.
- Use `pflag.NewFlagSet(..., pflag.ContinueOnError)` for subcommands or a
  testable `run(args)` flow. Send parser diagnostics to stderr.
- Use familiar Unix/OpenSSH/curl-style option names when the semantics match.
  Do not reuse conventional flags such as `-l`, `-p`, `-t`, or `-v` with a
  surprising meaning.
- Preserve whether options after a positional are data or flags. For example,
  SSH remote command arguments require deliberate interspersed-flag handling.
- Keep normal output scriptable. A `--json`, raw, or file-output mode must not
  receive decorative text on stdout.
- Treat commands that mutate operational systems as explicit, documented
  actions. `gkafka produce` is an example whose help warns about external
  state.

Subcommand tools may dispatch `help`, `version`, and command names before
constructing per-command FlagSets, as `gb64`, `gkafka`, `gvault`, and `gxfer`
do. Single-mode tools may use one FlagSet. Match the shape of the nearest
analogous command rather than forcing one parser layout on both kinds.

## 9. Help convention

Every command keeps help content in `cmd/<tool>/help.go`.

### Required content

- tool name and one-line purpose;
- exact `Usage:` forms;
- `Commands:` for a subcommand tool;
- `Options:` with short and long names, value placeholders, and meaningful
  defaults;
- important safety or output semantics;
- representative `Examples:` where usage is not obvious;
- `-h/--help` and the chosen version form.

Use stable plain text without ANSI formatting so it works through old
terminals and automation. Keep indentation aligned within one help block.
Subcommand-specific help should show only relevant options and tell the user
how to reach it from top-level help.

### Existing variation

`pkg/cli.PrintHelp` prepends a generic usage line, while
`pkg/cli.PrintCustomHelp` prints complete blocks. `gb64` and `gkafka` also use
`fmt.Print` for self-contained help. These are all current patterns. For new
help that already contains exact usage, use a complete-block approach and do
not prepend a second contradictory usage line. Do not reformat unrelated help
files during feature work.

## 10. Output, errors, and exit status

### Streams

- **stdout:** successful command results, payloads, JSON, paths, version/help
  output, or other explicitly consumable data.
- **stderr:** errors, warnings, progress prompts, metadata diagnostics, and
  verbose/debug output.
- Do not mix progress or decoration into a raw payload. `gkafka` and `gb64`
  have output paths where byte preservation matters.
- Never print credentials, private-key contents, authorization headers,
  tokens, Vault values, or other secrets in verbose output.

### Error handling

- Return errors through the call chain where practical; let `main` print one
  concise prefix and choose the process status.
- A failed operation must return a non-zero exit status. Invalid CLI use should
  be distinguishable from successful help when the tool needs that distinction.
- Preserve a meaningful remote/child status when it is part of the command's
  contract, as `gsh` does for remote commands.
- Add operation and target context to errors without exposing secrets.
- Do not use panic for expected input, filesystem, network, authentication, or
  protocol failures.
- Close files, response bodies, sessions, sockets, clients, encoders, and
  consumers on every applicable path. Check close errors when they determine
  data integrity.

Error wrapping is mixed between `%w` and `%v`. Use `%w` in new Go 1.14 code
when callers benefit from the error chain; do not convert unrelated existing
errors as cleanup.

## 11. Security baseline

- Do not accept passwords, private keys, or reusable tokens as CLI arguments
  unless the approved tool contract explicitly requires it and documents the
  process-list exposure. Prefer terminal prompts, protected configuration, or
  the existing Vault client.
- Use `pkg/auth.ReadPassword` for echo-free terminal secret input.
- Never print credentials in normal, error, or verbose output.
- Do not disable TLS certificate or SSH host-key verification by default for a
  new security-sensitive client. Any compatibility escape hatch must be
  explicit, narrow, documented, and approved.
- Use restrictive permissions for credential or sensitive state files.
- Validate paths and filenames, prevent traversal, avoid unintended overwrite,
  and use atomic publication where integrity matters. `pkg/b64drop` documents
  these boundaries for its protocol.
- Bound input sizes, timeouts, retries, and memory where data may be remote or
  untrusted.
- Treat command construction, subprocess arguments, templates, regexes,
  archive contents, and remote names as untrusted input.
- Clear mutable secret buffers where practical, but do not claim that Go
  strings or garbage-collected memory provide guaranteed secure erasure.

## 12. Dependency and vendor policy

`go.mod`, `go.sum`, `vendor/modules.txt`, and the checked-in `vendor/` tree are
one dependency set. Default Linux and Windows build definitions use
`-mod=vendor`. `go.mod` also pins replacement versions for `x/crypto` and
`x/sys`; these replacements must not be removed or upgraded casually.

Before adding a dependency:

1. use Go 1.14 standard library if adequate;
2. search existing `pkg` code;
3. search the current vendor tree;
4. consider a small, maintainable local implementation;
5. only then propose a new module.

For a proposed module, report Go 1.14 compatibility, CentOS 6/runtime impact,
transitive size, maintenance status, license, and binary-size implications.
Do not upgrade unrelated modules, run `go get ...@latest`, regenerate all of
vendor, or edit vendored source as a side effect of one tool.

`make_vendor1.14.bat` is not the normal build path: it deletes `vendor/` and
`go.sum`, downloads dependencies, and currently attempts to build the empty
`cmd/tool` path. Treat it as a legacy/destructive maintenance script requiring
separate review and explicit authorization, not as the documented dependency
update procedure.

## 13. Build and distribution definitions

### Linux

The root `Makefile` is the primary definition:

- `BINS` lists Linux tools;
- one target calls the shared `build_bin` macro;
- default output is `bin/<tool>`;
- default environment is `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`;
- `USE_VENDOR=1` selects `-mod=vendor` by default;
- link flags strip symbols and inject Git commit/build date;
- `all` builds every listed tool.

When adding an approved Linux command, update both `BINS` and its target using
`build_bin`; do not add a tool-local build system.

### Windows

`WindowMakefile.bat` currently builds only `bin\gb64.exe`, `bin\gcurl.exe`, and
the GUI `bin\B64Drop.exe`. The GUI uses `-H=windowsgui`; the two CLI binaries
receive version metadata. Add a Windows output only when Windows support is in
scope, and update the progress count, failure handling, and final output list
together.

Build artifacts belong under `bin/` and are ignored. Do not commit generated
binaries, archives, logs, local configuration, certificates, or keys.

## 14. Documentation policy

- Put repository-wide development rules here.
- Put a tool's operation, limitations, and user verification in
  `docs/<TOOL>.md` when more than help text is needed.
- Keep protocol specifications separate from UI/CLI documentation.
  `docs/B64DROP.md` is the B64DROP wire-format source of truth;
  `docs/GB64.md` and `docs/B64DROP_WINDOWS.md` describe its frontends.
- Update help, tool documentation, and build examples in the same scoped
  change when CLI or distribution behavior changes.
- State what was not built or verified. Do not turn static inspection into a
  claim of runtime success.

## 15. Development workflow

1. Read this guide and applicable `docs/` files.
2. Inspect `git status`; preserve unrelated user changes and untracked work.
3. Inspect the closest `cmd` implementation and its `help.go`.
4. Search `pkg/` and existing commands for equivalent behavior.
5. Inspect `go.mod`, `vendor/modules.txt`, and relevant vendored source.
6. Record compatibility, security, output, and scope constraints.
7. Decide what stays in `cmd` and what belongs in an existing or justified
   `pkg` package.
8. Implement only the approved scope.
9. Update the tool version, help, build target, and documentation when
   applicable.
10. Perform the static review below.
11. Report files changed, design choices, risks, and user-run verification.

Do not expand a focused feature into framework work, repository-wide naming
cleanup, dependency upgrades, or speculative options.

## 16. Static review checklist

Select the applicable items; not every tool uses files, goroutines, networks,
or credentials.

### Language and repository

- [ ] All syntax and standard-library APIs exist in Go 1.14.
- [ ] Every imported third-party package exists in the current vendor tree.
- [ ] Existing `pkg` functionality was searched before adding a duplicate.
- [ ] Package ownership is real, not speculative abstraction.
- [ ] Help, tool version, docs, and build definitions agree.
- [ ] No unrelated dirty-worktree file was changed.

### CLI and output

- [ ] Help/version paths succeed without performing operational work.
- [ ] Required, extra, conflicting, and bounded arguments are checked.
- [ ] stdout remains machine-consumable; diagnostics go to stderr.
- [ ] Failures return non-zero and meaningful remote statuses are preserved.
- [ ] Error messages contain useful operation context without secret data.

### Resources and concurrency

- [ ] Files, bodies, encoders, sockets, sessions, and clients are closed.
- [ ] Partial output and temporary files are removed or safely retained by an
      explicit recovery design.
- [ ] Close/flush/rename errors that affect integrity are handled.
- [ ] Goroutines, signal subscriptions, timers, contexts, and channels have a
      defined termination path.
- [ ] Terminal state is restored on every return after entering raw mode.

### Compatibility and security

- [ ] Linux/CentOS 6 syscalls and files used by the feature are available.
- [ ] Platform-specific code has Go 1.14-compatible build constraints.
- [ ] Credentials and sensitive headers cannot reach logs or verbose output.
- [ ] Verification is not silently disabled.
- [ ] Files containing sensitive data use suitable permissions.
- [ ] Untrusted paths, sizes, formats, patterns, and command arguments are
      validated and bounded.

## 17. Codex execution restrictions

The default Codex workflow in this repository is:

```text
analysis -> design -> implementation -> static review -> user report
```

### Default prohibition

Unless the user explicitly authorizes the exact operation for the current
task, Codex must not run:

- `go build`, `go test`, or `go run`;
- `make` or another compiler/build command;
- an existing or newly produced binary;
- project or Expect scripts;
- a real server connection or network behavior test.

Formatting source and reading/searching repository files are not runtime
verification. Build/test/run authorization from a previous task does not carry
forward automatically. When execution is not authorized, provide commands and
a verification procedure for the user instead.

Never report “build succeeded,” “tests passed,” or “works on CentOS 6” when the
corresponding verification was not actually authorized and performed.

## 18. New tool checklist

- [ ] Name and scope are narrow and use the repository's `g...` convention.
- [ ] `cmd/<tool>/main.go` and `cmd/<tool>/help.go` exist.
- [ ] Closest commands and every relevant `pkg` package were inspected.
- [ ] Go 1.14, CentOS 6, Linux amd64, and runtime dependencies were reviewed.
- [ ] `pflag`, help, version, stdout/stderr, and exit behavior follow this guide.
- [ ] The tool uses `pkg/version` and has an intentional version flag.
- [ ] No existing vendored or common implementation was duplicated without a
      documented policy reason.
- [ ] New dependencies, if any, received explicit review and approval.
- [ ] The Linux Makefile target was added; Windows definitions changed only if
      Windows support is in scope.
- [ ] Tool/protocol documentation and user-run verification steps are present.
- [ ] Static review is complete and unverified runtime claims are absent.

## 19. Known inconsistencies and review candidates

These observations are not authorization to change existing code:

- version constants use both `version_info` and `versionInfo`;
- version shorthand is split between `-v` and `-V` according to tool history
  and verbose-flag conflicts;
- help is printed through `PrintHelp`, `PrintCustomHelp`, or direct `fmt.Print`;
- error flow ranges from a testable `run(args)` to direct `os.Exit` in `main`;
- runtime CPU initialization is not uniform;
- `cli.Log` introduces logrus but currently has no project consumer;
- `pkg/fileutil` currently has no importing command;
- SSH client construction exists under both `pkg/gsh` and `pkg/xfer`, with
  materially different host-key policies and no shared transport abstraction;
- there are currently no project-owned Go tests;
- `make_vendor1.14.bat` is destructive and references the empty `cmd/tool`
  path, so its intended future role requires a separate decision;
- some tracked/source text contains legacy encoding or formatting artifacts.

Resolve these only in explicitly scoped maintenance work with behavior and
compatibility review. Do not combine them with an unrelated tool feature.
