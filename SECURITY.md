# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in `sunspecctl`, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please email **security@otfabric.io**, or open a private advisory on GitHub (the [Security](https://github.com/otfabric/sunspecctl/security) tab → **Advisories** → **Report a vulnerability**), with:

- A description of the vulnerability
- Steps to reproduce the issue, ideally the exact command
- The affected version(s) (`sunspecctl version` output)
- Any potential impact you've identified

We will acknowledge your report within 48 hours and aim to provide a fix or mitigation within 7 days for critical issues.

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Older releases | Best effort |

## Security Considerations

### SunSpec Modbus Has No Built-In Security

SunSpec devices are read, and with `sunspecctl serve` simulated, over Modbus, which carries no authentication and no encryption on TCP, UDP, RTU or ASCII: anyone who can reach a device can read and write it, and anyone on the path can read or alter the traffic.

- **Use `sunspecctl` only on devices and networks you own or are authorized to access**
- **Keep Modbus traffic on isolated, trusted networks**; never expose port 502 of an inverter, meter or battery to the Internet
- Across untrusted networks, use Modbus over TLS (`tcp+tls://`, supported by go-modbus) where the device supports it, or a VPN or secured gateway

### Data Read From Devices Is Untrusted Input

- Model IDs, lengths, strings and values come from the device. Discovery is bounded and lengths are validated before decoding, but treat what `sunspecctl` prints as untrusted: validate ranges before using values for control decisions, and escape strings (manufacturer, model, serial number) before putting them in HTML, SQL or shell commands
- `sunspecctl` output can contain serial numbers, network addresses and production figures; treat it as sensitive operational data when storing or sharing it

### Reading Only, as a Client

As a client `sunspecctl` reads and decodes; no command writes SunSpec control points to a device. This is enforced, not only intended: `detect`, `models`, `read*`, `poll*` and `capture` create their Modbus client with only Read Holding Registers (0x03) and Read Input Registers (0x04) allowed (`sunspec.ReadFunctionCodes()` given to go-modbus's `AllowedFunctionCodes`). Any other function code is refused by the client, with `modbus.ErrFunctionCodeNotAllowed`, before anything is sent; `TestClientIsReadOnly` checks it against a device that counts writes.

### Serving a Simulated Device

`sunspecctl serve` opens a Modbus TCP listener without authentication or encryption: whoever can reach it can read every register of the simulated device and write its writable points.

- **Listen on the loopback address unless others must reach it**: `--url tcp://127.0.0.1:1502`. `tcp://0.0.0.0:...` and `tcp://[::]:...` accept connections from every network the machine is on. Never expose a served device to the Internet
- **Do not run a simulator on a production network** where a controller or a monitoring system could take it for real equipment: a device file made with `capture` carries the manufacturer, model and serial number of the original
- **A captured device file is operational data**: it can contain serial numbers, network addresses, settings and production figures. Check it before sharing or committing it
- Device files are parsed and validated before anything is served, but only serve files you wrote or reviewed

### Dependencies

`make check` runs `govulncheck`. The protocol code is in [go-sunspec](https://github.com/otfabric/go-sunspec) and [go-modbus](https://github.com/otfabric/go-modbus); a vulnerability there is fixed by a release that updates the dependency. Vulnerabilities reported in the Go standard library are fixed by building with a patched Go release.

### Verifying Downloads

`sunspecctl` release archives are published with a SHA-256 checksum file. Verify the checksum before installing.
