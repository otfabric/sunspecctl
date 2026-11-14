# sunspecctl Releases

## v0.7.4

**Date:** 2026-11-14
**Previous release:** v0.7.3

No change to the command.

### Changed

- Built on go-modbus v1.15.2 (was v1.15.1) and go-sunspec v0.10.4 (was v0.10.3): go-modbus v1.15.2 backs off after a failed `Accept`, contains a metrics hook that does not return normally, refuses an RTU request longer than a frame before sending it, and does not send a request whose context ended while the deadline was set (go-modbus RELEASE.md, v1.15.2); the go-sunspec library code is unchanged.

---

## v0.7.3

**Date:** 2026-11-14
**Previous release:** v0.7.2

No change to commands or output.

### Changed

- Built on go-sunspec v0.10.3 (was v0.10.2). Its library code is the same; it is built on go-modbus v1.15.1.
- CI: otfabric/engineering policy 1.6, the shared workflows v3.8.0, and one pull request workflow with a single required check.

---

## v0.7.2

**Date:** 2026-11-13
**Previous release:** v0.7.1

### Changed

- Built on go-modbus v1.15.1 and go-sunspec v0.10.2 (was go-modbus v1.14.5 and go-sunspec v0.10.1). go-sunspec is a dependency bump only.
- `rtu://` and `ascii://`: after a timeout or an answer that does not match its request, the client discards the line until it is quiet, so the next request does not receive a late answer to an earlier one.
- `rtuovertcp://`, `asciiovertcp://` and `rtuoverudp://`: the client closes and redials the connection after a request error or a mismatched answer. Expect more reconnects behind gateways where units often time out.
- `serve` on `tcp+tls://`: when all 10 client slots are taken, a new connection replaces the oldest connection still in its TLS handshake instead of being refused; connections that completed the handshake are kept. A client must complete the TLS handshake within 10 s (was 30 s).

---

## v0.7.1

**Date:** 2026-11-13
**Previous release:** v0.7.0

### Changed

- Built on go-sunspec v0.10.1, go-modbus v1.14.5 and go-serial v0.3.0 (was go-sunspec v0.10.0, go-modbus v1.14.2, go-serial v0.2.1): go-serial rejects out-of-range serial parameters instead of truncating them. No other change.

---

## v0.7.0

**Date:** 2026-11-09
**Previous release:** v0.6.1

### Summary

Two flags on every command that connects to a device set how the connection is opened: `--proxy` reaches a plant network through a SOCKS5 proxy such as an SSH jump host, and `--source-addr` picks the local interface. Built on go-modbus v1.14.2, whose `Config.Dialer` opens the connections of every network transport.

### Added

- `--proxy socks5://host:port` (`SUNSPECCTL_PROXY`): connect through a SOCKS5 proxy (RFC 1928 CONNECT, no authentication); `socks5h://` sends host names to the proxy. For `tcp://`, `tcp+tls://` (the TLS handshake runs through the proxy and is verified against the host of `--url`), `rtuovertcp://` and `asciiovertcp://`. With `udp://` or `rtuoverudp://` it is a usage error (exit 2): SOCKS5 UDP ASSOCIATE is not implemented. Proxy credentials, other schemes, a missing port or a path are usage errors. A refusal by the proxy is a connection failure (exit 4) naming the proxy and its reply.
- `--source-addr <ip>` (`SUNSPECCTL_SOURCE_ADDR`): connect from that local IP address, over TCP and UDP; with `--proxy`, the connection to the proxy.
- Both on `detect`, `models`, `read`, `read-model`, `read-point`, `poll`, `poll-model`, `poll-point` and `capture`. Either flag with `rtu://` or `ascii://`, or on `serve`, is a usage error; `serve` ignores the environment variables.

### Changed

- Built on go-modbus v1.14.2 (was v1.13.1): `Config.Dialer`; a failed connect no longer repeats `dial tcp <addr>:` in its message (exit codes unchanged). `version` lists `go-modbus 1.14.2`.
- `make build` marks the commit `-dirty` when the working tree has changes, as `go build` does; release builds are unchanged.

### Tests

- `internal/netdial`: flag validation, source address binding over TCP and UDP, SOCKS5 CONNECT for IPv4, IPv6 and host names (`socks5` and `socks5h`), every reply and handshake failure, a silent proxy against the deadline, cancel. `internal/socks5test` is the scripted test proxy. Both are at 100 % coverage.
- Every client command through the test proxy to a fake SunSpec device, `socks5h` name forwarding, `tcp+tls` through the proxy (and a URL host the certificate does not hold failing the handshake), a refusing and a silent proxy, `--source-addr` on TCP, UDP and RTU over UDP, the environment variables, and every invalid value and transport mismatch as a usage error on every client command, with nothing sent.

---

## v0.6.1

**Date:** 2026-11-08
**Previous release:** v0.6.0

### Changed

- Built on go-modbus v1.13.1 (was v1.11.1). v1.12.0 to v1.13.1 change only go-modbus's bundled `modbus-cli` (organisation exit codes, the one-line `version` and `version --json`, shell completion); the library code sunspecctl uses is the same, so sunspecctl behaves as before. `version` lists `go-modbus 1.13.1`.
- Release binaries are built with the latest Go 1.26 patch release (otfabric/.github release workflow v3.6.0); they were built with the exact Go version in `go.mod` (1.26.0).

---

## v0.6.0

**Date:** 2026-11-07
**Previous release:** v0.5.1

### Added

- Shell completion completes flag values: `--parity`, `--data-bits`, `--stop-bits`, `--baud`, the URL schemes of `--url`, the model IDs with a known schema for `--id` and `--model` (with their names), the point names of the chosen model for `--point`, JSON files for `serve --file`, PEM files for `--tls-ca`, `--tls-cert` and `--tls-key`; numbers and durations no longer complete file names. The bash script is cobra's V2 script, with descriptions.

### Changed

- **Breaking for scripts that read `version`:** `version` prints one line, `sunspecctl 0.6.0 (tag v0.6.0, commit <sha>, built <date>, go-modbus 1.11.1, go-serial 0.2.1, go-sunspec 0.10.0, go1.26.10, linux/amd64)`, with the OT Fabric libraries it is built with,, instead of a `tag:`, `commit:` and `built:` block; the version has no leading `v`. `version --json` (and `--json version`, `SUNSPECCTL_FORMAT=json`) prints one line with the keys `name`, `version`, `tag`, `commit`, `built`, `libraries`, `go`, `platform`, always present and in this order; `buildDate` is now `built`. A development build reports `dev` and the commit from the Go VCS stamp (`-dirty` for a modified tree); `go install …@vX.Y.Z` reports that version.
- `make build` stamps a version and tag only when HEAD is a tag (a build off a tag reports `dev`), and the full commit SHA.

---

## v0.5.1

**Date:** 2026-11-06
**Previous release:** v0.5.0

### Changed

- Built on go-sunspec v0.10.0, which adds `Device.WritePoint` to the library. The commands do not use it: the client commands still read only. No other change.

---

## v0.5.0

**Date:** 2026-11-05
**Previous release:** v0.4.4

### Security

- Text output and error messages escape control characters, escape sequences, bidirectional overrides and invalid UTF-8 in device data (shown as `\x1b`, `\n`, `\u202e`, `\xff`), so a device can no longer clear the screen, set the window title or forge output lines. `serve`'s write log is escaped too. JSON output is unchanged.
- `capture -o` and `template -o` write the file with mode 0600 through a temporary file and a rename, so the old file is kept if writing fails, and refuse a symbolic link or a non-regular file such as a directory or `/dev/null` (exit 10). `-o /dev/stdout` no longer works: leave out `-o` instead.
- `serve --file` reads at most 16 MiB, and `--tls-cert`, `--tls-key` and `--tls-ca` at most 4 MiB each; a larger file is exit 3 "file too large".

### Fixed

- Ctrl-C or SIGTERM during discovery stops `poll`, `poll-model` and `poll-point` at once (exit 0), instead of after `--timeout`.
- Text output that cannot be written exits 10 instead of 0, and `poll*` stops instead of polling on. JSON output already did this.
- Usage errors (exit 2): a `--timeout` that is not positive (was exit 5), an `--interval` that is not positive (polled with no pause), a negative `--count` (exit 0 with no output) and `--unit-id 0` on client commands (silently read unit 1).
- The docs said `detect` on a device without the SunSpec marker prints `Detected: false` and exits 0; it exits 1 and prints no result, as before.

### Changed

- Go 1.26 or later is required to build from source (was 1.23).
- Built on go-sunspec v0.9.0 (scale factors outside -10..10 are not applied) and go-modbus v1.11.1 (serial, UDP and server fixes).

---

## v0.4.4

**Date:** 2026-11-03
**Previous release:** v0.4.3

### Changed

- Built on go-modbus v1.10.6 (codec count checks compare the full length of a slice) and go-sunspec v0.8.5. No other change.

---

## v0.4.3

**Date:** 2026-11-01
**Previous release:** v0.4.2

### Changed

- Dependency update, no other change. Picks up the go-modbus fix for a response that arrives during `Client.Close`.
- Built on go-modbus v1.10.5, go-sunspec v0.8.3, go-serial v0.2.1.

---

## v0.4.2

**Date:** 2026-11-01
**Previous release:** v0.4.1

### Changed

- Dependency update, no other change.
- Built on go-modbus v1.10.3, go-sunspec v0.8.2, go-serial v0.2.1.

---

## v0.4.1

**Date:** 2026-10-31
**Previous release:** v0.4.0

### Changed

- Policy 1.2: build output in `bin/` and `release/`, a per-package coverage floor, and more tests and CI. No change in behaviour.
- Built on go-modbus v1.10.3, go-sunspec v0.8.1, go-serial v0.2.1.

---

## v0.4.0

**Date:** 2026-10-30
**Previous release:** v0.3.0

### Dependencies

- `go-modbus` v1.7.2 → v1.10.0, `go-sunspec` v0.6.1 → v0.7.1.

---

## v0.3.0

**Date:** 2026-10-30
**Previous release:** v0.2.1

## Summary

Exit codes converge on the organisation table (ADR-0006). **Scripts that test for exit 1 must change.**

| Case | Old | New |
|---|---|---|
| Device is not SunSpec | 1 | 1 |
| Invalid flags or arguments | 1 | 2 |
| Invalid URL, device file, TLS file, point name | 1 | 3 |
| Connection failure, `serve` cannot listen | 1 | 4 |
| Timeout | 1 | 5 |
| Modbus exception, malformed frame, undecodable registers | 1 | 6 |
| Model not on the device | 1 | 8 |
| Permission refused by the operating system | 1 | 9 |
| Output or unexpected error | 1 | 10 |
| Interrupted before a result | killed by the signal | 130 |

### Changed

- SIGINT and SIGTERM now cancel every command through a root context, not only `serve` and `poll`.

### Added

- `SUNSPECCTL_FORMAT` (canonical) and `SUNSPECCTL_OUTPUT_FORMAT`: `json` has the effect of `--json`.

---

## v0.2.1

**Date:** 2026-10-29
**Previous release:** v0.2.0

## Summary

Dependency update for SunSpec devices on Modbus RTU: go-serial v0.1.8 fixes two POSIX serial I/O bugs. No command or flag changes.

### Dependencies

- go-sunspec v0.6.0 → v0.6.1, go-modbus v1.5.0 → v1.7.2, go-serial v0.1.6 → v0.1.8.

### Fixes (from go-serial v0.1.8)

- `Write` no longer loses data when the serial port's output buffer is
  full. It used to return a short count with a nil error (or `n == -1`
  with `EAGAIN`); it now blocks until every byte is written.
- `Read` works when the port's file descriptor is 1024 or higher. It used
  to return a spurious `io.EOF` at once.

### Other

- go-modbus v1.5.0 → v1.7.2 also brings the go-modbus fixes of v1.5.1–v1.7.2; see go-modbus's release notes.
- Documentation: README serve section covers `tcp+tls://`; security contact is security@otfabric.io.

---

## v0.2.0

**Date:** 2026-10-14
**Previous release:** v0.1.0

## Summary

`sunspecctl` moves to go-sunspec v0.6.0 and go-modbus v1.5.0. The device file code now comes from go-sunspec's new `devicefile` package, which also publishes a JSON Schema of the format; the files `capture` and `template` write, the files `serve` accepts and, but for one, the error messages are those of v0.1.0. The reading commands now use a Modbus client that can only send read requests.

## Changes

### Changed

- **Dependencies** — built on go-sunspec v0.6.0 and go-modbus v1.5.0 (v0.1.0: go-sunspec v0.5.1, go-modbus v1.4.2).
- **Device file code from go-sunspec** — parsing, validation, serving, capture, templates and writing of device files are now go-sunspec's [`devicefile`](https://github.com/otfabric/go-sunspec/tree/main/devicefile) package, the same code the OT Fabric SunSpec Explorer uses. Files written and accepted, and the messages for invalid files, do not change, with one exception: a member of the wrong type is reported by Go's JSON decoder, whose message names the Go type, now `File` instead of `deviceFile` (`cannot unmarshal string into Go struct field File.models.0.id of type uint16`). The format has a JSON Schema, `devicefile/devicefile.schema.json` in go-sunspec (`$id` `https://otfabric.io/schemas/sunspec/devicefile-1.schema.json`), referenced from the README.
- **Read-only client** — `detect`, `models`, `read`, `read-model`, `read-point`, `poll`, `poll-model`, `poll-point` and `capture` create their Modbus client with only function codes 0x03 and 0x04 allowed (`sunspec.ReadFunctionCodes()`). No command sent anything else before; now the client refuses any other request before it is sent. `serve` is a server and is not affected.

### Unchanged

- No changes to the commands, flags, text output, JSON output, device files or exit status, other than the type-error message above.

---

## v0.1.0

**Date:** 2026-10-13
**Previous release:** sunspecctl v0.4.2, released from [otfabric/go-sunspec](https://github.com/otfabric/go-sunspec)

## Summary

First release from this repository, and the first that can **serve**: `sunspecctl serve` runs a simulated SunSpec device from a JSON device file, `sunspecctl capture` writes such a file from a real device, and `sunspecctl template` writes a skeleton for the models you choose.

`sunspecctl` used to live in the go-sunspec repository as `cmd/sunspecctl` and was released together with the library, last as v0.4.2. It is now a project of its own with its own releases, and **its version numbering starts again at v0.1.0**. v0.1.0 is the successor of v0.4.2 although its number is lower: the two series are distinguished by repository and module path, not by comparing version numbers. The existing commands, their flags and their output are those of v0.4.2.

## Changes

### Added

- **`serve`** — serves a SunSpec device over Modbus TCP from a device file: models, point values by SunSpec name in engineering units, repeating groups, unit ID and base address, on the address given with `--url`. The file is validated completely before listening; errors name the model and point. `--unit-id` and `--base-address` override the file, `--check` validates and prints the layout without listening, `--json` prints the layout and client writes as JSON. The device shuts down cleanly on SIGINT and SIGTERM with exit status 0.
- **Device file format** — JSON, documented in the README: numbers with scale factors applied for you, enum and bitfield values by symbol name, `null` for not-implemented points, nested `groups`, raw `registers` for vendor models or single points.
- **TLS (`tcp+tls://`)** — new global flags `--tls-cert`, `--tls-key` and `--tls-ca` for Modbus/TCP Security (mutual TLS). `serve` uses them to serve a device over TLS and to accept only clients with a certificate of the given CA; the client commands (`detect`, `models`, `read*`, `poll*`, `capture`) use them to present a client certificate and verify the device. All three are required for a `tcp+tls://` URL, which earlier versions could not use at all, and ignored for other URLs.

- **`capture`** — reads every model of the device at `--url` and writes a device file that `serve` turns back into the same device. Models that decode cleanly are written as point values; models without schema or that do not match their schema are written as raw registers, as are all models with `--raw`. `-o` writes to a file.
- **`template`** — writes a device file with the models given with `--model`, every point present and `null`, one instance of each repeating group.
- **Client writes to a served device** — clients can write the points SunSpec defines as writable; accepted writes are printed. Everything else is refused with Modbus exception 2.

### Changed

- **Repository and module** — the source is `github.com/otfabric/sunspecctl`. Install from source with `go install github.com/otfabric/sunspecctl@latest`; the old path `github.com/otfabric/go-sunspec/cmd/sunspecctl` stays at v0.4.2.
- **Version numbering** — restarts at v0.1.0 in this repository. Tools that only upgrade to a higher version number (package recipes, update scripts comparing `sunspecctl version` output) do not get from 0.4.2 to 0.1.0 by themselves and need to be pointed at this repository once.
- **Downloads** — release archives and checksum files are published on the [releases page of this repository](https://github.com/otfabric/sunspecctl/releases), with the same file name pattern as before (`sunspecctl-<version>-<os>-<arch>.tar.gz` / `.zip`, `sunspecctl-<version>-checksums.txt`). Scripts that download from `otfabric/go-sunspec` need the new URL; see the install section of the README.
- **Dependencies** — built on go-sunspec v0.5.1 (serving was added in v0.5.0) and go-modbus v1.4.2 (v0.4.2 was built on go-modbus v1.3.0; wire format and client behaviour are unchanged).
- **Reads notice a device that changed** — from go-sunspec v0.5.0: when a device no longer has the model that was discovered at an address, `read`, `read-model`, `read-point` and the `poll` commands report `model changed since discovery` for it instead of decoding its registers with the old layout. This matters for long-running `poll` commands, which discover once.
- **Partial read message** — the position in `partial read: read at <address>+<offset>` now counts from the model's ID register rather than from its first data register, because the two header registers are read with the data.

### Unchanged

- No changes to the flags, text output, JSON output or exit status of `detect`, `models`, `read`, `read-model`, `read-point`, `poll`, `poll-model`, `poll-point`, `completion` and `version`, other than the two messages above.

---
