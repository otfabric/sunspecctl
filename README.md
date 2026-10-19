# sunspecctl — SunSpec Modbus Command-Line Tool for Solar Inverters, Meters and Batteries

[![Go](https://img.shields.io/badge/Go-1.23%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI](https://github.com/otfabric/sunspecctl/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/sunspecctl/actions/workflows/ci.yml)
[![Code Coverage](https://codecov.io/gh/otfabric/sunspecctl/graph/badge.svg)](https://codecov.io/gh/otfabric/sunspecctl)
[![Release](https://img.shields.io/github/v/release/otfabric/sunspecctl?label=release)](https://github.com/otfabric/sunspecctl/releases)

**sunspecctl is a command-line tool for reading and simulating SunSpec devices over Modbus.** It detects whether a solar inverter, energy meter, battery or other DER device speaks SunSpec, lists the SunSpec models it exposes, and reads and decodes every point into scaled values with units, over Modbus TCP, RTU (RS-485), ASCII or a serial-to-Ethernet gateway. It can also be the device: `sunspecctl serve` runs a simulated SunSpec device from a JSON file, which `sunspecctl capture` writes from a real one. It is a single static binary for Linux, macOS and Windows, with the official [SunSpec Alliance](https://sunspec.org/) model definitions built in.

Use it to commission and troubleshoot a device, to check what it really exposes before writing integration code, to poll values into a script or a pipeline as JSON, or to develop and test an EMS, a logger or a SCADA integration against a simulated inverter, meter or battery. It is built on the [otfabric/go-sunspec](https://github.com/otfabric/go-sunspec) library, which is what to use from Go code.

**Quick links:** [Quick start](#quick-start) · [Install](#install) · [Commands](#commands) · [Global flags](#global-flags) · [Polling](#polling) · [Serving / simulating a device](#serving--simulating-a-device) · [FAQ](#faq)

---

## Quick start

```bash
# Is this a SunSpec device, and at which base address?
sunspecctl detect --url tcp://192.168.1.100:502

# Which SunSpec models does it implement?
sunspecctl models --url tcp://192.168.1.100:502

# Read and decode everything
sunspecctl read --url tcp://192.168.1.100:502

# Read AC power from the inverter model every 5 seconds, as JSON
sunspecctl poll-point --model 103 --point W --interval 5s --json --url tcp://192.168.1.100:502

# The same device on RS-485, 9600 8N1, Modbus address 3
sunspecctl read --url rtu:///dev/ttyUSB0 --baud 9600 --stop-bits 1 --unit-id 3

# Copy the device into a file, and serve that copy on this machine
sunspecctl capture --url tcp://192.168.1.100:502 -o device.json
sunspecctl serve --file device.json --url tcp://127.0.0.1:1502
```

```
sunspecctl
├── detect       Detect SunSpec device presence
├── models       List discovered SunSpec models
├── read         Read and decode all models
├── read-model   Read and decode a specific model by ID
├── read-point   Read a single named point from a model
├── poll         Repeatedly read and decode all models
├── poll-model   Repeatedly read and decode a specific model by ID
├── poll-point   Repeatedly read a single named point from a model
├── serve        Serve a simulated SunSpec device from a device file
├── capture      Read a device and write it as a device file for serve
├── template     Write a device file skeleton for the given models
├── completion   Generate shell completion script (bash/zsh/fish/powershell)
└── version      Print the sunspecctl version
```

## Install

The easiest way to install `sunspecctl` is to download a pre-built binary from the [GitHub Releases page](https://github.com/otfabric/sunspecctl/releases). No Go toolchain is required.

| Platform | CPU | Download |
|---|---|---|
| Linux | x86-64 | `sunspecctl-<version>-linux-amd64.tar.gz` |
| Linux | 64-bit ARM (Raspberry Pi 4/5 with a 64-bit OS, AWS Graviton) | `sunspecctl-<version>-linux-arm64.tar.gz` |
| Linux | 32-bit ARM (Raspberry Pi with a 32-bit OS) | `sunspecctl-<version>-linux-armv7.tar.gz` |
| macOS | Apple Silicon (M1 and later) | `sunspecctl-<version>-darwin-arm64.tar.gz` |
| macOS | Intel | `sunspecctl-<version>-darwin-amd64.tar.gz` |
| Windows | x86-64 | `sunspecctl-<version>-windows-amd64.zip` |
| Windows | ARM64 | `sunspecctl-<version>-windows-arm64.zip` |

Each release also contains `sunspecctl-<version>-checksums.txt` (SHA-256) so you can verify your download. The commands below fetch the **latest** release, verify its checksum and install it. Afterwards, check that it works:

```console
sunspecctl version
```

### Linux

```bash
# 1. Find the latest version and your CPU architecture
VERSION=$(curl -fsSLI -o /dev/null -w '%{url_effective}' https://github.com/otfabric/sunspecctl/releases/latest | sed 's|.*/v||')
case "$(uname -m)" in
  x86_64)         ARCH=amd64 ;;
  aarch64|arm64)  ARCH=arm64 ;;
  armv7l)         ARCH=armv7 ;;
  *) echo "Unsupported architecture: $(uname -m)" ;;
esac

# 2. Download the archive and the checksum file
FILE="sunspecctl-${VERSION}-linux-${ARCH}.tar.gz"
BASE="https://github.com/otfabric/sunspecctl/releases/download/v${VERSION}"
curl -fsSLO "${BASE}/${FILE}"
curl -fsSLO "${BASE}/sunspecctl-${VERSION}-checksums.txt"

# 3. Verify the download (should print "OK")
grep "${FILE}" "sunspecctl-${VERSION}-checksums.txt" | sha256sum -c -

# 4. Extract and install (the binary in the archive carries the version and platform in its name)
mkdir -p sunspecctl-dl && tar -xzf "${FILE}" -C sunspecctl-dl
sudo install -m 0755 "$(find sunspecctl-dl -type f -name 'sunspecctl*' | head -n1)" /usr/local/bin/sunspecctl
```

No `sudo`? Install into your home directory instead (and make sure `~/.local/bin` is in your `PATH`):

```bash
mkdir -p ~/.local/bin
install -m 0755 "$(find sunspecctl-dl -type f -name 'sunspecctl*' | head -n1)" ~/.local/bin/sunspecctl
```

To talk to a device on a serial port (RS-485 adapter) without `sudo`, your user needs access to the port, usually through the `dialout` group: `sudo usermod -aG dialout "$USER"`, then log out and back in.

### macOS

```bash
# 1. Find the latest version and your CPU architecture (arm64 = Apple Silicon, x86_64 = Intel)
VERSION=$(curl -fsSLI -o /dev/null -w '%{url_effective}' https://github.com/otfabric/sunspecctl/releases/latest | sed 's|.*/v||')
case "$(uname -m)" in
  arm64)   ARCH=arm64 ;;
  x86_64)  ARCH=amd64 ;;
esac

# 2. Download the archive and the checksum file
FILE="sunspecctl-${VERSION}-darwin-${ARCH}.tar.gz"
BASE="https://github.com/otfabric/sunspecctl/releases/download/v${VERSION}"
curl -fsSLO "${BASE}/${FILE}"
curl -fsSLO "${BASE}/sunspecctl-${VERSION}-checksums.txt"

# 3. Verify the download (should print "OK")
grep "${FILE}" "sunspecctl-${VERSION}-checksums.txt" | shasum -a 256 -c -

# 4. Extract and install (the binary in the archive carries the version and platform in its name)
mkdir -p sunspecctl-dl && tar -xzf "${FILE}" -C sunspecctl-dl
sudo install -m 0755 "$(find sunspecctl-dl -type f -name 'sunspecctl*' | head -n1)" /usr/local/bin/sunspecctl
```

If you downloaded the archive with a web browser instead of `curl` and macOS refuses to run the binary ("cannot be verified"), remove the quarantine flag: `xattr -d com.apple.quarantine /usr/local/bin/sunspecctl`.

### Windows (PowerShell)

```powershell
# 1. Find the latest version and your CPU architecture
$version = (Invoke-RestMethod https://api.github.com/repos/otfabric/sunspecctl/releases/latest).tag_name.TrimStart('v')
$arch    = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }

# 2. Download the archive and the checksum file
$file = "sunspecctl-$version-windows-$arch.zip"
$base = "https://github.com/otfabric/sunspecctl/releases/download/v$version"
Invoke-WebRequest "$base/$file" -OutFile "$env:TEMP\$file"
Invoke-WebRequest "$base/sunspecctl-$version-checksums.txt" -OutFile "$env:TEMP\sunspecctl-checksums.txt"

# 3. Verify the download (throws an error on mismatch)
$expected = ((Select-String -Path "$env:TEMP\sunspecctl-checksums.txt" -Pattern $file).Line -split '\s+')[0]
$actual   = (Get-FileHash "$env:TEMP\$file" -Algorithm SHA256).Hash
if ($expected -ne $actual) { throw "Checksum mismatch for $file" }

# 4. Extract to a per-user folder and name the program sunspecctl.exe
#    (the .exe in the archive carries the version and platform in its name)
$dir = "$env:LOCALAPPDATA\Programs\sunspecctl"
New-Item -ItemType Directory -Force -Path $dir | Out-Null
Expand-Archive "$env:TEMP\$file" -DestinationPath "$env:TEMP\sunspecctl-dl" -Force
$exe = Get-ChildItem "$env:TEMP\sunspecctl-dl" -Recurse -Filter 'sunspecctl*.exe' | Select-Object -First 1
Copy-Item $exe.FullName "$dir\sunspecctl.exe" -Force

# 5. Add sunspecctl to your user PATH (then open a new terminal)
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($userPath -notlike "*$dir*") {
  [Environment]::SetEnvironmentVariable('Path', "$userPath;$dir", 'User')
}
```

If Windows SmartScreen warns about an unrecognised app, choose **More info → Run anyway**.

### Manual download

1. Open the [latest release](https://github.com/otfabric/sunspecctl/releases/latest).
2. Download the archive for your platform from the table above.
3. Extract it, rename the binary to `sunspecctl` (`sunspecctl.exe` on Windows) and move it to a folder on your `PATH`.

### Update

Run the same commands again. They always fetch the latest release and overwrite the previous binary.

### With Go

If you have Go 1.23 or later, you can build and install the latest release straight from source:

```bash
go install github.com/otfabric/sunspecctl@latest
```

### Coming from go-sunspec

`sunspecctl` used to be released from the [go-sunspec](https://github.com/otfabric/go-sunspec) repository, last as v0.4.2. Releases now come from this repository, and **the version numbering starts again at v0.1.0**: sunspecctl v0.1.0 is the successor of the sunspecctl that shipped with go-sunspec v0.4.2, although its number is lower. The two series are told apart by where they come from, not by comparing numbers:

- The install commands above replace the old ones; they always take the latest release of this repository.
- `go install github.com/otfabric/go-sunspec/cmd/sunspecctl@latest` stays at v0.4.2 for good. Use `go install github.com/otfabric/sunspecctl@latest`.
- A script or package recipe that only upgrades to a higher version number will not move from 0.4.2 to 0.1.0 by itself: point it at the new repository once.

The existing commands, their flags and their output are unchanged.

## Global flags

| Flag | Default | Description |
|------|---------|-------------|
| `--url` | `tcp://localhost:502` | Modbus device URL |
| `--unit-id` | `1` | Modbus unit ID |
| `--timeout` | `10s` | Operation timeout |
| `--json` | `false` | Output as JSON |
| `--raw` | `false` | Include raw register hex in output |
| `--baud` | `0` (19200) | Serial baud rate, for `rtu://` and `ascii://` URLs |
| `--data-bits` | `0` (8 for RTU, 7 for ASCII) | Serial data bits, 5–8 |
| `--parity` | `none` | Serial parity: `none`, `even` or `odd` |
| `--stop-bits` | `0` (2 without parity, 1 with) | Serial stop bits, 1 or 2 |
| `--tls-cert` | | Certificate (PEM) this side presents, for `tcp+tls://` URLs |
| `--tls-key` | | Private key (PEM) of `--tls-cert` |
| `--tls-ca` | | CA certificates (PEM) that the other side's certificate must chain to |

For `serve`, `--url` is the address to listen on and `--unit-id` overrides the unit ID of the device file; see [Serving / simulating a device](#serving--simulating-a-device).

`--url` selects the transport: `tcp://host:port`, `rtu://<device>` (for example `rtu:///dev/ttyUSB0` or `rtu://COM3`), `ascii://<device>`, `udp://host:port`, `rtuovertcp://host:port` for a serial-to-Ethernet gateway, and the other [go-modbus URL schemes](https://github.com/otfabric/go-modbus#transport-modes). The serial flags are ignored for network URLs.

Many SunSpec devices on RS-485 use 9600 baud, 8 data bits, no parity and 1 stop bit, which is not the default: pass `--baud 9600 --stop-bits 1` (and the device's Modbus address as `--unit-id`).

## Commands

```bash
# Detect SunSpec presence
sunspecctl detect --url tcp://192.168.1.100:502

# List discovered models
sunspecctl models --url tcp://192.168.1.100:502

# Read and decode all models
sunspecctl read --url tcp://192.168.1.100:502

# Read a specific model by ID
sunspecctl read-model --id 101 --url tcp://192.168.1.100:502

# Read a single point from a model
sunspecctl read-point --model 101 --point W --url tcp://192.168.1.100:502

# SunSpec over Modbus RTU (RS-485) at 9600 8N1, device address 3
sunspecctl models --url rtu:///dev/ttyUSB0 --baud 9600 --stop-bits 1 --unit-id 3

# A serial device behind an RS-485-to-Ethernet gateway
sunspecctl read --url rtuovertcp://192.168.1.50:4001 --unit-id 3
```

## Polling

The `poll`, `poll-model`, and `poll-point` commands work like their `read` counterparts but repeat at a configurable interval.

| Flag | Default | Description |
|------|---------|-------------|
| `--interval` | `30s` | Interval between polls (supports `s`, `m`, `h`) |
| `--count` | `0` | Number of polls (`0` = infinite, stop with Ctrl-C) |

```bash
# Poll all models every 10 seconds
sunspecctl poll --interval 10s --url tcp://192.168.1.100:502

# Poll model 101 five times, once per minute
sunspecctl poll-model --id 101 --interval 1m --count 5 --url tcp://192.168.1.100:502

# Poll a single point every 5 seconds as JSON (great for piping)
sunspecctl poll-point --model 101 --point W --interval 5s --json --url tcp://192.168.1.100:502
```

## Serving / simulating a device

`sunspecctl serve` runs a SunSpec device over Modbus TCP from a JSON *device file*: its models, their point values, its unit ID and base address. Anything that speaks SunSpec Modbus can read it — `sunspecctl` itself, your EMS, a logger, a SCADA driver.

```bash
# Serve the device described in device.json on port 1502 of this machine
sunspecctl serve --file device.json --url tcp://127.0.0.1:1502

# In another terminal: read it like any device
sunspecctl read --url tcp://127.0.0.1:1502
```

```console
$ sunspecctl serve --file device.json --url tcp://127.0.0.1:1502
Serving SunSpec device on tcp://127.0.0.1:1502 (unit ID 1, base address 40000)
ID     NAME                                    START  LENGTH
1      Common                                  40002  66
103    Inverter (Three Phase)                  40070  50
160    Multiple MPPT Inverter Extension Model  40122  48
64900  unknown_64900                           40172  3
```

The device runs until you press Ctrl-C or the process is terminated (SIGINT or SIGTERM); it then closes its connections, prints `shutting down` on standard error and exits with status 0. The file is validated completely before the device starts listening: an error names the model and point it concerns and the exit status is 1.

| Flag | Default | Description |
|------|---------|-------------|
| `--file`, `-f` | (required) | The device file |
| `--url` | `tcp://localhost:502` | Address to listen on: `tcp://host:port`, or `tcp+tls://host:port` with `--tls-cert`, `--tls-key` and `--tls-ca` (see [Serving over TLS](#serving-over-tls)). Ports below 1024 usually need privileges; use `tcp://0.0.0.0:1502` to accept connections from other machines |
| `--unit-id` | from the file, else `1` | Modbus unit ID the device answers (overrides the file) |
| `--base-address` | from the file, else `40000` | Register address of the `SunS` marker (overrides the file) |
| `--check` | `false` | Validate the file, print the layout and exit without listening |
| `--json` | `false` | Print the layout, and each client write, as JSON |

The device is served over Modbus TCP, or over Modbus/TCP Security (`tcp+tls://`, see [Serving over TLS](#serving-over-tls)). Plain Modbus TCP has no authentication: whoever can reach the port can read the device and write its writable points. See [SECURITY.md](SECURITY.md).

### The device file

```json
{
  "unit_id": 1,
  "base_address": 40000,
  "models": [
    {
      "id": 1,
      "points": { "Mn": "Acme", "Md": "PV-5000", "SN": "A-0001" }
    },
    {
      "id": 103,
      "points": {
        "A": 18.3, "A_SF": -1,
        "PhVphA": 230.4, "V_SF": -1,
        "W": 4200, "W_SF": 0,
        "Hz": 50.01, "Hz_SF": -2,
        "St": "MPPT",
        "Evt1": ["GROUND_FAULT"]
      }
    },
    {
      "id": 160,
      "points": { "DCW_SF": 0 },
      "groups": {
        "module": [
          { "points": { "ID": 1, "DCW": 2150 } },
          { "points": { "ID": 2, "DCW": 2050 } }
        ]
      }
    },
    { "id": 64900, "registers": [1, 2, 3] }
  ]
}
```

| Member | Meaning |
|---|---|
| `unit_id` | Modbus unit ID, 1–255. Default 1. |
| `base_address` | Register address of the `SunS` marker. Default 40000. Clients find a device at 0, 40000 or 50000 without being told. |
| `models` | The models in chain order. Each has an `id` and either `points` (and `groups`) or `registers`. A model ID may occur more than once. |
| `points` | Point values by SunSpec point name, as in the model definition (`sunspecctl read` shows the names). |
| `groups` | Repeating groups: group name → list of instances, each with its own `points` and `groups`. Count points such as `NPt` or `NCrv` are filled in from the number of instances; give one explicitly to reserve more instances than you fill. |
| `registers` | The model's data registers (what follows the ID and length registers), as numbers 0–65535. For vendor models that have no schema, and for anything you want served bit for bit. |
| `name` | Ignored. `capture` and `template` write the model's name here for the reader. |

Values:

| Point type | Write it as |
|---|---|
| Numbers (`int16` … `uint64`, `acc*`, `float*`, `sunssf`, `count`) | A JSON number. A point with a scale factor takes the **engineering value**: with `"A_SF": -1`, `"A": 18.3` is stored as 183. Give the scale factor point as well; without it the number is stored as it is. A value that does not fit the point after scaling is an error. |
| `enum16`, `enum32` | The symbol name (`"MPPT"`) or the number. |
| `bitfield16` … `bitfield64` | A list of the names of the set bits (`["GROUND_FAULT", "OVER_TEMP"]`, `[]` for none) or the number. |
| `string` | A JSON string. |
| `ipaddr`, `ipv6addr`, `eui48` | A string: `"192.168.1.10"`, `"fe80::1"`, `"00:11:22:33:44:55"`. |
| Any point | A list of register numbers, exactly as many as the point has registers: `"TmpCab": [65036]`. |
| Not implemented | `null`, or leave the point out. |

Unknown members, a point given twice and point names the model does not have are errors, so a typo does not silently serve an empty value.

The format is implemented by the [`devicefile`](https://github.com/otfabric/go-sunspec/tree/main/devicefile) package of go-sunspec, which Go programs can use to read, write and serve device files themselves. Its JSON Schema, [`devicefile/devicefile.schema.json`](https://github.com/otfabric/go-sunspec/blob/main/devicefile/devicefile.schema.json) (`$id` `https://otfabric.io/schemas/sunspec/devicefile-1.schema.json`), describes the form of a file for editors and validators; whether the models and points exist and the values fit is checked by `sunspecctl serve --check`.

### Getting a device file

```bash
# Copy a real device: everything it exposes, as it is now
sunspecctl capture --url tcp://192.168.1.100:502 -o device.json

# Or start from the models you want, every point present and null
sunspecctl template --model 1,103,160 -o device.json

# Check a file without serving it
sunspecctl serve --file device.json --check
```

`capture` writes models that decode cleanly as point values, which are easy to read and edit. A model without a known schema, or one that does not match its schema exactly (shorter, longer, a value that would not read back the same), is written as `registers`. Either way, serving the file gives a device that reads the same as the original: the same points, values, scale factors and not-implemented points at the same addresses. With `--raw`, every model is written as `registers` and the copy is register-for-register identical. `capture` writes enum and bitfield points as numbers, so that values the model definition does not name survive.

A capture is a snapshot: the served values do not change by themselves. It can contain serial numbers and network addresses of the device; look before you share it.

### Unit IDs

The device answers one unit ID: the one of the device file, or `--unit-id`. A request for another unit ID is answered with Modbus exception 0x0B (gateway target device failed to respond), so a client learns at once that the unit is not there. A broadcast (unit ID 0) is not answered and not carried out.

### Serving over TLS

`tcp+tls://` is Modbus/TCP Security: TLS in which both sides present a certificate. `serve` and the client commands take the same three flags, each for its own side: `--tls-cert` and `--tls-key` are the certificate and key that side presents, `--tls-ca` the CA certificates the other side's certificate must chain to. All three are required for a `tcp+tls://` URL and ignored for any other. The device certificate must name the host or IP address the clients connect to.

```bash
# The device presents device.pem and accepts clients with a certificate of ca.pem
sunspecctl serve --file device.json --url tcp+tls://0.0.0.0:802 \
  --tls-cert device.pem --tls-key device.key --tls-ca ca.pem

# A client presents its own certificate and checks the device's against the same CA
sunspecctl read --url tcp+tls://192.168.1.100:802 \
  --tls-cert client.pem --tls-key client.key --tls-ca ca.pem
```

A client without a certificate of that CA, and a plain `tcp://` client, cannot read the device.

### Writes

A client can write the points SunSpec defines as writable (access `RW` in the model definition), with Modbus function codes 6 and 16. Each accepted write is printed, and later reads return the written value:

```
write from 127.0.0.1: model 123 at 40176: WMaxLimPct = 50.5
```

Writes to read-only points, to model headers, to the points that count group instances, to part of a multi-register point, to models given as `registers` and to unmapped addresses are refused with Modbus exception 2 (illegal data address). The simulated device stores what is written; it does not act on it — writing a power limit does not change the served power.

## Output formats

```bash
# Default: human-readable table
sunspecctl models --url tcp://192.168.1.100:502

# JSON output (structured, suitable for piping to jq)
sunspecctl models --url tcp://192.168.1.100:502 --json

# Include raw register hex alongside decoded values
sunspecctl read --url tcp://192.168.1.100:502 --raw
```

## FAQ

### What is SunSpec?

[SunSpec Modbus](https://sunspec.org/sunspec-modbus-specifications/) is an open standard that defines how solar inverters, meters, batteries and other distributed energy resources lay out their data in Modbus registers: a marker (`SunS`) at a known base address, followed by a chain of numbered *models*, each a block of named points. A SunSpec reader works with any compliant device, whatever the manufacturer.

### My device is not detected. Where does SunSpec data start?

`sunspecctl detect` probes the common base addresses (0, 40000 and 50000, and their off-by-one neighbours) in holding registers for the `SunS` marker, and prints every attempt and what the device answered. Check the Modbus unit ID (`--unit-id`; many inverters do not answer on unit 1), that Modbus TCP is enabled on the device, and on serial lines the baud rate, parity and stop bits.

### Does it work over Modbus RTU / RS-485?

Yes: `--url rtu:///dev/ttyUSB0` (or `rtu://COM3` on Windows) with `--baud`, `--data-bits`, `--parity` and `--stop-bits`. For a device behind a serial-to-Ethernet gateway use `rtuovertcp://host:port`.

### What happens with models sunspecctl does not know?

They are still discovered and listed: `sunspecctl models` shows them with `SCHEMA no`, and reading them returns their raw registers with a warning instead of decoded points.

### Can sunspecctl write SunSpec points (controls)?

No. As a client it reads and decodes only; no command writes to a device. The Modbus client of `detect`, `models`, `read*`, `poll*` and `capture` is created with only the read function codes (3 and 4) allowed, so it refuses any other request before sending it. A device simulated with `sunspecctl serve` does accept writes *from* other clients, to the points SunSpec defines as writable.

### Can I test my software without a real inverter?

Yes: [`sunspecctl serve`](#serving--simulating-a-device) runs a simulated device from a JSON file. Write the file by hand, generate a skeleton with `sunspecctl template`, or copy a real device once with `sunspecctl capture` and serve the copy wherever you need it.

### How do I use the output in a script?

Add `--json` to any command for structured output suitable for `jq`. The exit status is 0 on success and 1 when a command fails.

### How are the model definitions kept up to date?

They are compiled into [go-sunspec](https://github.com/otfabric/go-sunspec) from the official [sunspec/models](https://github.com/sunspec/models) repository, and reach `sunspecctl` with a go-sunspec update.

## Building

```bash
make build         # ./bin/sunspecctl
make build-all     # cross-compile for Linux amd64/arm64/armv7, macOS amd64/arm64, Windows amd64/arm64
make install       # build and install to /usr/local/bin
```

## Development

```bash
make check         # format, vet, lint, vulnerability scan, race tests, coverage gate
make test          # tests with the race detector
```

Tests need no hardware: they run against the SunSpec fixture server of go-sunspec's `testutil` package and against the devices `serve` runs in-process. See [CONTRIBUTING.md](CONTRIBUTING.md).

## Requirements

- Go 1.23+ to build from source (not needed for the pre-built binaries)
- [otfabric/go-sunspec](https://github.com/otfabric/go-sunspec) v0.6.0+ and [otfabric/go-modbus](https://github.com/otfabric/go-modbus) v1.5.0+

## Contributing

Bug reports, device-compatibility notes and pull requests are welcome. Please [open an issue](https://github.com/otfabric/sunspecctl/issues) to discuss larger changes first, and run `make check` before submitting. See [CONTRIBUTING.md](CONTRIBUTING.md).

Please report security vulnerabilities privately, as described in [SECURITY.md](SECURITY.md), not in a public issue.

## License

This project is licensed under the MIT License. See [LICENSE](./LICENSE).
