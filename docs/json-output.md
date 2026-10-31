# JSON output

With `--json`, or `SUNSPECCTL_FORMAT=json`, a command writes indented JSON to stdout: one document per result. Messages and errors go to stderr, as without `--json`, so stdout stays parseable; a failed command prints no document and sets the [exit code](exitcodes.md). `SUNSPECCTL_FORMAT=json` and `SUNSPECCTL_OUTPUT_FORMAT` have the same effect.

Field names are those of the Go types: most are `PascalCase`, because the library types carry no JSON tags. They are a contract: a change in a field's name or meaning is a breaking change of this tool, and a library upgrade that changes one must be noted in the release.

## Commands

| Command | Document |
|---|---|
| `version` | [Version](#version) |
| `detect` | [Detection](#detection) |
| `models` | [Discovery](#discovery) |
| `read`, each round of `poll` | An array of [decoded models](#decoded-model) |
| `read-model`, each round of `poll-model` | One [decoded model](#decoded-model) |
| `read-point`, each round of `poll-point` | One [decoded point](#decoded-point) |
| `serve`, at the start; `serve --check` | [Served device](#served-device) |
| `serve`, per client write | [Write event](#write-event) |

The `poll` commands and `serve` write several documents in a row, not an array and not one line each: read them with a streaming parser, such as `jq`.

`capture` and `template` write a device file, not this output: its format is in the [README](../README.md) and in the schema of go-sunspec's `devicefile` package.

With `read` and `poll`, a model that cannot be read gives `warning: partial read` on stderr, and the array holds the rest.

## Version

```json
{ "version": "v0.8.0", "tag": "v0.8.0", "commit": "abc1234", "buildDate": "2026-10-31T10:00:00Z" }
```

`tag`, `commit` and `buildDate` are left out when the build did not set them.

## Detection

```json
{
  "Detected": true,
  "UnitID": 1,
  "RegType": 0,
  "BaseAddress": 40000,
  "Marker": [21365, 28243],
  "Attempts": [
    { "BaseAddress": 0, "RegType": 0, "Registers": null, "Matched": false,
      "error": "Read Holding Registers (0x03): Illegal Data Address (0x02)" },
    { "BaseAddress": 40000, "RegType": 0, "Registers": [21365, 28243], "Matched": true }
  ]
}
```

`Detected: false` is a result: `detect` exits 0. `RegType` is go-modbus's number for the table, with 0 for holding registers. `Marker` holds the two registers of "SunS". There is one `Attempts` entry per probed base address; `error` is its only lower-case field, and is left out when the read worked.

## Discovery

```json
{
  "BaseAddress": 40000,
  "RegType": 0,
  "Models": [
    {
      "Header": { "ID": 1, "Length": 66, "StartAddress": 40002, "EndAddress": 40069, "NextAddress": 40070, "IsEndModel": false },
      "Name": "Common",
      "Schema": { "ID": 1, "Name": "common", "Label": "Common", "Desc": "...", "Group": { "...": "..." } },
      "SchemaKnown": true,
      "DecodingSupported": true
    }
  ],
  "Warnings": null,
  "Raw": { "...": "go-modbus's low-level discovery result" }
}
```

`Schema` is the whole SunSpec model definition of go-sunspec's registry, with its points, which makes this document large; it is null when `SchemaKnown` is false. `DecodingSupported` false means that reads return raw registers only. `Warnings` has one note per model without a schema. `Raw` is kept for debugging and is not part of the contract.

## Decoded model

```json
{
  "ModelID": 103,
  "Name": "Inverter (Three Phase)",
  "InstanceAddress": 40070,
  "Schema": { "...": "the model definition, as in Discovery" },
  "Group": { "...": "see below" },
  "RawRegisters": [103, 50, 0],
  "Warnings": null
}
```

`InstanceAddress` is the address of the model's ID register. `Group` holds the points; `Schema` and `Group` are null for a model that was not decoded (no schema, or a failed read), and `RawRegisters` then holds the data registers that were read. For a decoded model, `RawRegisters` starts with the ID and length registers.

`Group` has `Name` (the group's name in the schema), `Index` (the 1-based number of this instance among its siblings, 0 at the top), `Points` (its own [decoded points](#decoded-point), in schema order) and `Groups` (the nested instances in register order, or null). A repeating part of a model, such as the phases of a meter or the points of a curve, is one nested instance per repetition.

## Decoded point

```json
{
  "Name": "W",
  "Type": "int16",
  "RawValue": 4200,
  "ScaledValue": 42000,
  "Units": "W",
  "SFName": "W_SF",
  "SFRawValue": 1,
  "RegisterOffset": 14,
  "RegisterCount": 1,
  "Implemented": true,
  "Symbols": null
}
```

`RawValue` is the value in the registers, with the type of `Type`: a number, or a string for string points. `ScaledValue` is `RawValue` times 10 to the power of `SFRawValue`, and null when that does not apply: no scale factor, or a point that is not implemented. `Implemented` is false when the device reports the SunSpec "not implemented" value, and `RawValue` then holds that value (-32768 for `int16`). `Symbols` lists the names of the set bits or of the enum value, or is null.

## Served device

```json
{
  "URL": "tcp://127.0.0.1:1502",
  "UnitID": 1,
  "BaseAddress": 40000,
  "Models": [
    { "ID": 1, "Name": "Common", "StartAddress": 40002, "Length": 66 },
    { "ID": 103, "Name": "Inverter (Three Phase)", "StartAddress": 40070, "Length": 50 }
  ]
}
```

`StartAddress` is the model's ID register. `Length` counts the data registers after the ID and length registers. `Name` is `unknown_<ID>` for a model without a schema.

## Write event

```json
{
  "Event": "write",
  "Client": "127.0.0.1:50344",
  "ModelID": 123,
  "Address": 40080,
  "Registers": [50],
  "Points": []
}
```

`serve` prints one for each accepted write. `Address` is the first register written, `Registers` the values, and `Points` the written points as [decoded points](#decoded-point).
