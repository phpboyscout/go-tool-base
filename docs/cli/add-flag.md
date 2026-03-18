---
title: Adding Flags 🚩
description: Guide on how to add flags to existing commands using the `generate add-flag` utility.
date: 2026-02-16
tags: [cli, flags, generator]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Adding Flags 🚩

Forgot to add a flag when creating a command? No problem! The `add-flag` utility is designed to inject new flags into existing commands effortlessly, keeping your manifest and code in sync.

<video controls autoplay loop muted playsinline width="100%">
  <source src="../../tapes/flags-demo.mp4" type="video/mp4">
</video>

## Usage

To add a new flag to an existing command:

```bash
go run main.go generate add-flag -c my-command -n retry -t int -d "Number of retries"
```

This command performs three key actions:

1.  Updates `.gtb/manifest.yaml` with the new flag definition.
2.  Regenerates `pkg/cmd/my-command/cmd.go` to include the flag registration and struct fields.
3.  Leaves your `pkg/cmd/my-command/main.go` untouched, ready for you to use the new flag!

!!! note "Separation of Concerns"
    We separate the boilerplate (`cmd.go`) from your logic (`main.go`) so that we can regenerate the boilerplate as you add flags or change configurations without overwriting your hard work!

## Supported Types

You can use any of the following types for your flags:

- `string`
- `bool`
- `int`
- `float64`
- `stringSlice`
- `intSlice`

## Examples

### Adding a Boolean Flag

```bash
go run main.go generate add-flag -c server -n verbose -t bool -d "Enable verbose logging"
```

### Adding a Slice Flag

```bash
go run main.go generate add-flag -c process -n tags -t stringSlice -d "Tags to apply"
```

### Adding a Persistent Flag

Persistent flags are available to the command they are defined on AND all of its subcommands.

```bash
# Add a persistent config flag to the root command
go run main.go generate add-flag -c root -n config -t string -d "Config file" --persistent
```

## Project Path

If your project root is not the current directory, use the `--path` (`-p`) flag:

```bash
go run main.go generate add-flag -c my-command -n retry -t int -d "Number of retries" -p /path/to/project
```

## Targeting Nested Commands

To add a flag to a nested command, provide the full path in the `-c` argument:

```bash
go run main.go generate add-flag -c server/start -n port -t int -d "Port to listen on"
```
