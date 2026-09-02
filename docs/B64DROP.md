# B64DROP Protocol

B64DROP is a plain-text envelope for transferring one gzip-compressed file through a terminal clipboard. Version 1 has no network transport and no ANSI control sequences.

## Envelope

The canonical form is:

```text
-----BEGIN B64DROP-----
version=1
filename=example.dat
original_size=123
compression=gzip
encoding=base64
sha256=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef

H4sI...
-----END B64DROP-----
```

The begin marker and end marker must each occupy their own line. Metadata starts on the line after the begin marker. Exactly one empty line separates metadata from the payload. Receivers locate the complete block and ignore clipboard text before the begin marker or after the end marker. Missing or nested markers are rejected.

Encoders must emit LF (`\n`) line endings. Decoders must accept LF and CRLF, including clipboard text whose line endings have been normalized consistently or independently.

## Metadata

Each metadata line is an ASCII key, one `=`, and a value. Keys are case-sensitive. The six required keys must occur exactly once and in the canonical order shown above. A decoder must reject missing or duplicate required keys and unsupported values. Unknown keys are reserved for later protocol versions and must be rejected by a version 1 decoder.

- `version` must be `1`.
- `filename` is the percent-encoded UTF-8 basename described below.
- `original_size` is the uncompressed byte count as an unsigned base-10 integer with no sign or leading zeroes, except that zero is written `0`.
- `compression` must be `gzip`.
- `encoding` must be `base64`.
- `sha256` is the SHA-256 digest of the original, uncompressed bytes, written as exactly 64 lowercase hexadecimal characters.

Metadata lines must not contain leading or trailing whitespace. Metadata values must not contain raw control characters.

## Filename

The logical filename is a single basename, encoded as UTF-8. It must not be empty, `.` or `..`. Before serialization, both `/` and `\` are treated as path separators and only the final non-empty component is retained. This rule applies on every operating system, so a Windows path cannot bypass a Unix encoder and vice versa.

In the metadata value, UTF-8 bytes in the RFC 3986 unreserved set (`A-Z`, `a-z`, `0-9`, `-`, `.`, `_`, `~`) are written literally. Every other byte is written as `%HH` using uppercase hexadecimal. A decoder must reject malformed percent escapes or invalid UTF-8, apply the basename rule again, and reject decoded NUL or control characters. A receiver may additionally replace names forbidden by its local filesystem, but must never interpret the value as a path.

For file input, the encoder uses the input path's basename. For stdin, `-n` or `--name` is mandatory and supplies the logical filename.

## Payload

The payload is the standard RFC 4648 Base64 encoding, with the standard alphabet and required `=` padding, of one gzip member. Encoders wrap Base64 at exactly 76 characters per non-final line; the final line may be shorter and is followed by LF.

Decoders may ignore ASCII space, horizontal tab, CR, and LF within the Base64 payload. They must reject all other non-alphabet characters and invalid padding. Missing or altered payload data is detected by decompression, size, and digest validation.

## Gzip member

The gzip stream uses Go's `compress/gzip` default compression level. For canonical output its header fields are fixed as follows:

- modification time: zero
- original name: empty
- comment: empty
- extra data: empty
- operating system: 255 (`unknown`)

Decoders must accept any valid gzip header, but must require exactly one complete gzip member and reject trailing compressed data or bytes.

## Validation and saving

A receiver processes data in this order:

1. Locate and validate the complete envelope.
2. Parse and validate all metadata.
3. Base64-decode the payload.
4. Gzip-decompress it while counting bytes and computing SHA-256.
5. Compare both `original_size` and `sha256` with the metadata.
6. Only after both comparisons succeed, atomically publish the temporary file under a collision-free basename.

Any failure removes the temporary file. Existing files are never overwritten; receivers choose `name_1.ext`, `name_2.ext`, and so on using exclusive creation so concurrent receivers cannot claim the same target.

## Streaming implications

Metadata precedes the payload but contains the source size and digest. A regular file encoder therefore makes one streaming pass to calculate metadata and a second streaming pass to gzip/Base64-encode it. For stdin, the encoder streams input into a temporary spool file while calculating metadata, then encodes that spool file and removes it. This bounds memory usage without weakening validation.

## Version 2 chunk transfer

Version 2 is used when the final Base64 payload exceeds the configured Chunk
threshold. The original is compressed once into one gzip stream. That binary
stream is divided into Chunks, and each Chunk is independently Base64 encoded.

```text
-----BEGIN B64DROP CHUNK-----
version=2
transfer_id=0123456789abcdef0123456789abcdef
filename=example.dat
original_size=123456789
original_sha256=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
compression=gzip
encoding=base64
compressed_size=45678901
transfer_size=60905204
chunk_index=1
chunk_total=12
chunk_size=3932160
chunk_sha256=abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789

H4sI...
-----END B64DROP CHUNK-----
```

All fields are required in the order shown. `transfer_id` is 128 random bits
written as 32 lowercase hexadecimal characters. `chunk_index` is one-based.
`chunk_size` and `chunk_sha256` describe the decoded compressed-binary Chunk.
`transfer_size` is the sum of the unwrapped Base64 character counts.

Base64 uses the v1 alphabet, padding, 76-character wrapping, and whitespace
rules. Text outside a complete marker pair is ignored. Missing or changed
payload is never guessed or repaired.

### Chunk sizing

For Base64 limit `L`, the maximum compressed-binary Chunk is
`floor(L / 4) * 3` bytes. The initial CLI default is 5 MiB. It is configurable
and is not claimed to be safe for every PuTTY scrollback configuration; test
the actual environment and adjust `--chunk-size`.

### Receive, retry, and completion

Receivers group data only by `transfer_id` and require all metadata to match.
Order is irrelevant. Valid duplicate Chunks are accepted as re-received.
Invalid Chunks do not remove already verified data and may be copied again.

Chunk binary is kept in a transfer-specific temporary directory. After every
Chunk is present, the receiver concatenates them in index order, confirms
`compressed_size`, decompresses the gzip stream, validates `original_size` and
`original_sha256`, and only then atomically publishes a collision-free name.
Completed temporary data is removed. Incomplete transfers older than seven
days may be removed.

Existing output is never overwritten. Examples are `file.dat`, `file_1.dat`,
and `archive.tar.gz`, `archive_1.tar.gz`.
