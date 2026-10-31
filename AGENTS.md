# AGENTS.md — sunspecctl

## What this is

Command-line tool for reading and simulating SunSpec devices over Modbus, built on [go-sunspec](https://github.com/otfabric/go-sunspec) and [go-modbus](https://github.com/otfabric/go-modbus). Pure Go, one package at the repository root. Profile: **cli**.

## Organisation standards

This repository follows **otfabric/engineering policy 1.2**, profile **cli** (`.otfabric/policy.yaml`, mode `enforce`). Where this file and the organisation standards disagree, this file wins here — say so in your report.

## Where the truth lives

| Question | Source |
|---|---|
| Exit codes (script-facing API) | docs/exitcodes.md |
| Commands and flags | README.md |
| Security guidance | SECURITY.md |
| SunSpec behaviour | go-sunspec |
| Release history | RELEASE.md (`## vX.Y.Z` … `---`) |

## Hard rules

1. Exit codes and `--json` output are a public contract: changing them is a breaking change with a RELEASE.md note.
2. The client side only reads: `clientConfig` limits the Modbus client to read function codes (`sunspec.ReadFunctionCodes`). Keep it so.
3. Never lower `COVERAGE_MIN`, add lint suppressions, or skip or delete tests to make CI pass.
4. go-sunspec and go-modbus are pinned to released tags; no `replace` directives.

## Commands

| When you change… | Run |
|---|---|
| anything | `make check` |
| the command line, exit codes or `main` | `make e2e` (builds the binary and runs it) |
| the coverage gate only | `make coverage-check` |

## Definition of Done

`make check` green; tests for new commands; README.md updated for new flags; RELEASE.md section for user-visible changes.

## Git and releases

Branches and PRs. Do not push to `main`, tag or run the Release workflow unless the maintainer asks in the session.
