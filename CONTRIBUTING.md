# Contributing to sunspecctl

Thank you for your interest in contributing to `sunspecctl`! This document covers the guidelines for contributing.

## Getting Started

1. Fork the repository on GitHub
2. Clone your fork locally:
   ```sh
   git clone git@github.com:<your-username>/sunspecctl.git
   cd sunspecctl
   ```
3. Create a feature branch:
   ```sh
   git checkout -b feature/my-change
   ```

## Development Setup

### Prerequisites

- Go 1.23+ (see [go.mod](go.mod)); the code must build and pass on Go 1.23, so do not use newer language or standard-library features
- [staticcheck](https://staticcheck.dev/), [golangci-lint](https://golangci-lint.run/) and [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) for the checks
- No hardware: tests use the SunSpec fixture server of go-sunspec's [`testutil`](https://pkg.go.dev/github.com/otfabric/go-sunspec/testutil) package, and `serve` itself running in-process

### Build & Test

```sh
# Run all checks (format, vet, lint, vulnerability scan, race tests, coverage gate)
make check

# Run tests with the race detector
make test

# Coverage report
make coverage

# Build ./bin/sunspecctl
make build
```

Run `make` (or `make help`) for the full list of targets.

## Making Changes

### Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Start every Go file with `// SPDX-License-Identifier: MIT`
- Keep functions focused and small
- Comments end with a period (enforced by the linter)

### Formatting

`make fmt` formats with the `gofmt` of the Go version in `go.mod`, which is the version CI uses, not necessarily the one you have installed. `make fmt-check` (part of `make check`) fails if either that `gofmt` or your local one would change a file: `gofmt` output differs slightly between Go releases, and a file the two disagree on fails CI. If they disagree, restructure the code (for example, move trailing comments onto their own lines) rather than picking a side.

### Where a change belongs

Decoding, encoding, discovery, the served device, the device file format, model definitions and the Modbus transports live in [go-sunspec](https://github.com/otfabric/go-sunspec) and [go-modbus](https://github.com/otfabric/go-modbus). A wrongly decoded or encoded value, a missing model or a device file problem is fixed there and reaches `sunspecctl` with a dependency update; the device file format is go-sunspec's [`devicefile`](https://github.com/otfabric/go-sunspec/tree/main/devicefile) package, with its JSON Schema. This repository holds the commands, flags and output.

To develop against a go-sunspec that is not released yet, do not add a `replace` directive to `go.mod`. Put both checkouts in a Go workspace outside the repositories and pass it to make: `make check GOWORK=/path/to/go.work` (the Makefile sets `GOWORK=off` by default, so that the module builds standalone).

### Commit Messages

Use clear, concise commit messages:

```
component: short description

Optional longer explanation of the change, why it was made,
and any relevant context.
```

Examples:
- `poll: add --count to poll-point`
- `read: print repeating groups as tables`
- `serve: add --check`
- `deps: go-sunspec v0.5.0`

### Testing

- Add tests for new functionality in the appropriate `_test.go` file; the repository uses only the standard `testing` package
- Use go-sunspec's `testutil` package for anything that talks to a device: it serves a SunSpec model chain over Modbus TCP. For `serve`, start the command in-process on a free port (`startServe` in `serve_test.go`) and read it back with the other commands
- A change to `capture`, `serve` or `template`, or a go-sunspec update, must keep `TestCaptureServeReproducesTheDevice` meaningful: capture → serve → read gives the same decoded values
- Assert behaviour: the printed output, the JSON document, the returned error — not just that the code ran
- Keep tests fast, deterministic and race-clean; CI runs them with `-race` on every Go version from 1.23 and a 120 s per-package timeout
- Coverage is enforced by `make check` and Codecov; new code should come with tests
- If your change affects what is read from a device, verify against real hardware when possible and say which device in the pull request

### Documentation

When you change a command, a flag, the output or the device file format, update the command's help text, [README.md](README.md) and [RELEASE.md](RELEASE.md), calling out anything that breaks existing scripts or files (flag names, JSON field names, device file members, exit status).

## Submitting Changes

1. Push your branch to your fork:
   ```sh
   git push origin feature/my-change
   ```
2. Open a Pull Request against the `main` branch
3. Describe what your change does and why
4. Reference any related issues

For larger changes, please open an issue first to discuss the approach.

## Reporting Issues

- Use GitHub Issues to report bugs or suggest features
- Include the `sunspecctl version` output and the operating system
- Provide the exact command that reproduces the problem
- For device problems, include the manufacturer and model, the transport (TCP, RTU, …) and the output of `sunspecctl detect` and `sunspecctl models`; `sunspecctl read --raw` shows the registers behind a wrongly decoded value
- Report security vulnerabilities privately: see [SECURITY.md](SECURITY.md)

## Project Structure

```
main.go        Root command, global flags, detect / models / read* commands, output
poll.go        poll, poll-model and poll-point
serve.go       serve: run a device from a device file, report client writes
capture.go     capture and template: write device files (format: go-sunspec/devicefile)
*_test.go      Tests against the go-sunspec fixture server and in-process serve
```

## License

By contributing to `sunspecctl`, you agree that your contributions will be licensed under the [MIT License](LICENSE).
