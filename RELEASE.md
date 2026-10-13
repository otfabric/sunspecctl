# sunspecctl Releases

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
