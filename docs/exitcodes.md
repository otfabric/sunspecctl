# Process exit codes

sunspecctl's exit codes are a script-facing contract (`exit.go`). They follow the organisation table (otfabric/engineering `standards/api-design.md#cli-exit-codes`). The error is printed on stderr.

| Code | Kind | Meaning |
|------|------|---------|
| 0 | ok | Success. `poll` and `serve` also exit 0 after SIGINT or SIGTERM |
| 1 | findings | The device answered and is not a SunSpec device (`detect` itself reports this as a result and exits 0) |
| 2 | usage | Unknown command or flag, missing argument, invalid serial setting, an invalid `SUNSPECCTL_FORMAT` |
| 3 | invalid_input | An invalid URL, device file, TLS file, unknown point name, or a model without a schema for `template` |
| 4 | transport | Connecting failed or the connection was lost; `serve` cannot listen |
| 5 | timeout | `--timeout` passed |
| 6 | protocol | A Modbus exception, a malformed frame, registers that do not decode as SunSpec |
| 8 | unsupported | The device does not have the requested model |
| 9 | permission | The operating system refused an operation |
| 10 | internal | The output could not be written, or something unexpected happened |
| 130 | interrupted | SIGINT or SIGTERM before a result |

Exit 7 (partial) is not used.

`SUNSPECCTL_FORMAT=json` (or `SUNSPECCTL_OUTPUT_FORMAT`; `_FORMAT` wins) has the effect of `--json`.

## Mapping from versions before v0.3.0

Every failure exited 1. Only a device that is not SunSpec stays at 1.
