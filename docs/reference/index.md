---
title: Reference
description: Lookup material for GTB — every command, every configuration key, the API stability policy, and the migration notes for each breaking change.
date: 2026-08-02
tags: [reference, index, overview]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Reference

Information-oriented material you look things up in: what a flag does, what a
configuration key defaults to, what happens when a value is wrong, and what
changed in a release that needs your attention.

If you are trying to *accomplish* something rather than look something up, start
at the [how-to guides](../how-to/index.md). If you want to understand *why*
something works the way it does, start at [explanation](../explanation/index.md).

## Commands

**[CLI reference](cli/index.md)** — every built-in command a GTB tool ships,
with its flags, defaults and behaviour.

The pages most people arrive at:

- **[Root command](cli/root.md)** — the global flags (`--config`, `--debug`,
  `--ci`, `--output`), signal handling and exit codes.
- **[`config`](cli/config.md)** — reading, writing, validating and trusting
  configuration from the command line.
- **[`init`](cli/init.md)** — first-run bootstrap and the per-subsystem wizards.
- **[`update`](cli/update.md)** — self-update, and the verification it can be
  made to require.
- **[`doctor`](cli/doctor.md)** — health checks and the redacted support bundle.

## Configuration

**[Configuration keys](config/index.md)** — every key the framework reads, its
type, its default, and what happens when it is set to something unusable. Also
the precedence order between flags, environment, project-local file, config
files and embedded defaults, and the rule that decides whether an environment
variable reaches the key you meant.

## API stability

**[API stability policy](api-stability.md)** — what "pre-1.0" commits GTB to,
which surfaces are stable, and how deprecations are staged.

## Migration notes

**[Migration guides](migration/index.md)** — one note per breaking change, each
naming the release, what moved, and the mechanical edit that fixes a consumer.
Start here when an upgrade stops compiling.
