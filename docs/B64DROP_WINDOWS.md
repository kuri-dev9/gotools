# B64Drop for Windows

B64Drop is a native Windows desktop frontend for the shared `pkg/b64drop`
engine. The protocol source of truth is [B64DROP.md](B64DROP.md).

## Main window

Starting `B64Drop.exe` opens the main window and immediately registers it with
`AddClipboardFormatListener`. Closing the window unregisters the listener and
terminates the process; there is no tray icon or background mode.

The window provides:

- current clipboard monitoring and restore status;
- output directory selection and Explorer access;
- a saved-file ListView ordered by newest modification time;
- manual refresh, file open, and confirmed deletion;
- a read-only, scrollable preview of the first 64 KiB;
- session-only `SHA-256 OK` status for files restored by this process.
- B64DROP v2 progress, per-Chunk symbols, and Missing/Failed Chunk numbers.

Preview supports valid UTF-8 and UTF-8 BOM. Content containing NUL, invalid
UTF-8, or unsafe control characters is reported as binary and is not rendered.
The program never loads an entire file for preview.

Clipboard decode, gzip expansion, validation, and file writing run in a worker
goroutine. Completion is posted back to the main Windows message loop, keeping
UI updates on the UI thread. While one payload is processing, additional
clipboard updates are ignored. Valid duplicate v2 Chunks are accepted and
shown as re-received.

Verified compressed Chunks are stored at
`%TEMP%\B64Drop\transfers\<transfer_id>`. They may arrive out of order and
remain available while failed or missing Chunks are copied again. Completed
transfers are removed immediately; incomplete transfers older than seven days
are removed at application startup.

## Configuration

`b64drop.ini` is stored next to `B64Drop.exe`. Selecting a new folder in the UI
writes it immediately. An example is available at
`windows/B64Drop/b64drop.example.ini`.

```ini
[general]
output_dir=D:\B64Drop
notification=true
```

The notification value remains compatible with the existing configuration;
the visible main-window status is the primary notification in this version.

## Windows build

Install Go and run this command from the repository root:

```bat
WindowMakefile.bat
```

The script checks that Go is available, creates `bin`, and builds all three
current Windows binaries. Any failed build returns a non-zero exit code.

```text
bin\gb64.exe
bin\gcurl.exe
bin\B64Drop.exe
```

The root `Makefile` contains Linux targets only.

## Security boundaries

B64Drop processes text only when the clipboard owner resolves to `putty.exe`
and both B64DROP markers are present. File publication, validation, collision
handling, and filename protection are provided by `pkg/b64drop`. UI deletion
is limited to regular files scanned from the configured output directory and
requires explicit confirmation.

The application contains no network transport, service, administrator
requirement, polling loop, web UI, or global keyboard hook.
