---
title: "gtb wizard"
description: "Run the generation wizard again over an existing project: every page pre-filled from the manifest, the answers validated like the flags and applied with the same sync as regenerate."
---

# `gtb wizard`

The generation wizard, over an existing project.

```bash
gtb wizard              # revisit the settings, write the manifest, sync the generated files
gtb wizard --dry-run    # print which settings would change, write nothing
```

Every page is pre-filled from `.gtb/manifest.yaml`; accept a page to keep it,
change an answer to change the setting. The pages keep their feature gates, so
ticking a feature on the first page shows its pages in the same run, and
unticking one clears that page's answers. What the wizard writes is validated
the way the `generate project` flags are validated, and the generated files
(root command, adapter files, signing files, derived fields) are brought into
line in the same run.

## What a revisit asks differently

| First run | Revisit |
|---|---|
| Asks the project name | Shows it. Renaming moves `cmd/<name>` and every import path, which is not the wizard's job. |
| Asks the destination path | Does not: the project is where it is. |
| Signing page asks "require a verified checksum" | Asks that and "require a valid signature", because a signed release may have shipped by now. The description says not before it has. |
| MCP page asks the publication mode | Asks that and which commands stay on the MCP surface, pre-ticked from each command's `mcp_enabled`. A changed tick is the same write as `gtb enable mcp <path>` / `gtb disable mcp <path>`; a protected command is not listed. `--dry-run` names each change as `mcp surface: <path> exposed` or `withheld`. |

## Flags

| Flag | Default | Description |
|---|---|---|
| `--path, -p` | `.` | Project root. |
| `--dry-run` | `false` | Print the settings that would change as `path: old -> new` and write nothing. |

`gtb wizard` needs an interactive terminal. In a script, `gtb set` is the
non-interactive way to change one setting. Spec
[0197](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0197-author-settings-as-one-surface)
D13.
