# Process exit codes

sunspecctl's exit codes are a script-facing contract (`exit.go`). They follow the organisation table (otfabric/engineering `standards/api-design.md#cli-exit-codes`). The error is printed on stderr.

| Code | Kind | Meaning |
|------|------|---------|
| 0 | ok | Success. `poll` and `serve` also exit 0 after SIGINT or SIGTERM |
| 1 | findings | The device answered and is not a SunSpec device: no probed base address holds the "SunS" marker. This includes `detect`, which then prints no result |
| 2 | usage | Unknown command or flag, missing argument, invalid serial setting, an invalid `SUNSPECCTL_FORMAT`, a `--timeout` or `--interval` that is not positive, a negative `--count`, `--unit-id 0` for a client command, a `tcp+tls://` URL without `--tls-cert`, `--tls-key` or `--tls-ca` (for `serve` as for the client commands; `serve` exited 3 before v0.9.0), an invalid `--proxy` or `--source-addr` (or `SUNSPECCTL_PROXY`, `SUNSPECCTL_SOURCE_ADDR`), `--proxy` with a UDP or serial URL, `--source-addr` with a serial URL, either flag on `serve` |
| 3 | invalid_input | An invalid URL, device file, TLS file (also one over its size limit: 16 MiB for a device file, 4 MiB for a PEM file), unknown point name, or a model without a schema for `template` |
| 4 | transport | Connecting failed or the connection was lost, also a `--proxy` that refuses the connection; `serve` cannot listen |
| 5 | timeout | `--timeout` passed |
| 6 | protocol | A Modbus exception, a malformed frame, registers that do not decode as SunSpec |
| 8 | unsupported | The device does not have the requested model |
| 9 | permission | The operating system refused an operation |
| 10 | internal | The output could not be written (standard output, text or JSON, or the `-o` file of `capture` and `template`, also when it is a symbolic link or not a regular file), or something unexpected happened |
| 130 | interrupted | SIGINT or SIGTERM before a result |

Exit 7 (partial) is not used.

`SUNSPECCTL_FORMAT=json` (or `SUNSPECCTL_OUTPUT_FORMAT`; `_FORMAT` wins) has the effect of `--json`.

## `read` and `poll` after discovery

When the device answers discovery, `read` and each round of `poll` read every model it reported:

- A model that cannot be read is printed with its error (in its `Warnings` with `--json`), stderr has `warning: partial read: <first error>`, and the exit code is 0; `poll` goes on with the next round.
- When not one model can be read, the command fails with the first error and its code, as `read-model` does: 5 when `--timeout` passed, 4 when the connection was lost, 6 when the device answers every read with a Modbus exception. It prints no models and no JSON document, and `poll` stops after that round. Before v0.9.0 this case exited 0 too, with the warning and the models without values.

## Mapping from versions before v0.3.0

Every failure exited 1. Only a device that is not SunSpec stays at 1.
