---
title: "Change a generated tool's settings after generation"
description: "Use gtb set, enable and disable to change what a generated project records, without editing YAML or running regenerate by hand."
---

# Change a generated tool's settings after generation

Every author setting of a generated project lives in `.gtb/manifest.yaml`, and
every command that changes it also brings the generated files into line, so you
never edit YAML and then remember to regenerate.

## Change a value

```bash
gtb set chat.default.provider openai
gtb set chat.default.model gpt-5
gtb set telemetry.endpoint https://telemetry.example.internal
```

`gtb set` validates the value the way the generate flags are validated: a chat
default the tool does not link, a malformed endpoint or an unknown config layer
is refused and nothing is written. The path is the manifest's (see the
[set reference](../../reference/cli/set.md)); `gtb set bogus x` prints the full
list.

## Turn a feature on or off

```bash
gtb enable ai
gtb disable keychain
gtb enable signing --email release@example.com
```

`enable` and `disable` re-render the root command, the adapter files and any
derived field the feature needs in the same run. Enabling `ai` records the
default provider list and writes `cmd/<name>/chat.go`; disabling `keychain`
removes `cmd/<name>/keychain.go`.

## Clear a value or read one back

```bash
gtb unset chat.default.model
gtb get chat.providers
```

## Why not delete the generated file?

Every `DO NOT EDIT` file under `cmd/<name>/` and `pkg/cmd/root/signing.go` is
re-emitted from the manifest by every command that writes it, so a deleted file
comes back. The durable override is the manifest field: `chat.providers: []`
to ship no provider, `gtb disable keychain` to drop the keychain, `gtb disable
signing` to turn signing off. See the
[manifest concept](../../explanation/concepts/manifest.md).
