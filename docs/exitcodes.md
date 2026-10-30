# Process exit codes

sunspecctl's exit codes are a script-facing contract. Changes are noted in RELEASE.md.

| Code | Meaning |
|------|---------|
| 0 | Success. `serve` also exits 0 after SIGINT or SIGTERM |
| 1 | Any failure: invalid flags, device connection, decoding, an invalid `serve` file |

The error is printed on stderr (`main.go`).
