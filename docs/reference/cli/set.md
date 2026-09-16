---
title: "gtb set / unset / get"
description: "Change, clear or read one author setting of a generated project by its manifest path, with the same validation as the generate flags and the same derived-file sync as regenerate."
---

# `gtb set` / `gtb unset` / `gtb get`

One setter over the manifest's author settings. The path is the manifest's own
dotted path; the `properties.` prefix may be left off.

```bash
gtb set chat.default.provider openai
gtb set telemetry.endpoint https://telemetry.example.internal
gtb set bootstrap.skip_config_check version doctor     # a list: several values, or comma-separated
gtb set bootstrap.auto_initialise true                 # a bool: true or false
gtb set release_source.repo neworg/newname             # org/name, spread over owner and repo
gtb set version.go 1.27
gtb unset chat.default.model
gtb get chat.providers                                 # one value per line
```

## What happens on `set`

1. The path is resolved against the author-settings table
   (`cli/pkg/generator/author_settings.go`). A path the table does not name is
   refused with the list of settable paths.
2. The value is written into the manifest in memory and the whole manifest is
   validated the way `regenerate` validates it, which is the way the generate
   flags are validated. A refused value writes nothing.
3. The derived files are synced (root command, adapter files, signing files,
   derived fields), the manifest is written, and on a real filesystem the
   touched files are formatted. No `regenerate` is needed afterwards.

## Refused paths

| Path | Why | Use instead |
|---|---|---|
| `features` | features have their own verbs | `gtb enable <feature>`, `gtb disable <feature>` |
| `templates` | overlays have their own verbs | `gtb template add`, `update`, `remove` |
| `hashes`, `commands`, `docs_layout`, `module_published`, `version.gtb` | recorded by the generator | nothing; they describe what gtb did |

## Flags

| Flag | Default | Description |
|---|---|---|
| `--path, -p` | `.` | Project root. |

## Settable paths

Every "recorded as" field in the [generate reference](generate.md) is settable.
The list `gtb set` prints on an unknown path is generated from the table and is
the authority. Spec
[0197](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0197-author-settings-as-one-surface)
D6.
