---
title: Tutorials
description: Learning-oriented lessons that take you from nothing to a working GTB tool, step by step.
date: 2026-08-02
tags: [tutorials, index, learning]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Tutorials

Learning-oriented lessons. Each one starts from nothing and ends with something
that runs, and each is verified against a named GTB release.

If you already know what you want to achieve and just need the steps, the
[how-to guides](../how-to/index.md) are the shorter route. To look something up,
try the [reference](../reference/index.md).

## Start here

### [Build your first CLI](build-your-first-cli.md)

Scaffold a project with the `gtb` generator, build and run it, give it a
configuration, add a command of your own, and regenerate without losing your
changes. About twenty minutes.

## Longer series, currently only on the blog

Three multi-part series cover ground this section does not yet. They are
published on the creator's blog and are being migrated here; until a page exists
under `tutorials/`, the post is the only copy.

| Series | Parts | Covers |
|---|---|---|
| [Building a CLI with go-tool-base](https://phpboyscout.uk/building-a-cli-with-go-tool-base-part-1/) | 5 | Scaffolding, configuration, exposing the CLI to AI agents, an AI-backed command, self-update. |
| [Building a web service with go-tool-base](https://phpboyscout.uk/building-a-web-service-with-go-tool-base-part-1/) | 6 | Lifecycle and graceful shutdown, gRPC with TLS, REST by hand and by spec, the gateway, generated API docs, observability. |
| [Sign your own binaries with go-tool-base](https://phpboyscout.uk/sign-your-own-binaries-with-go-tool-base-part-1/) | 7 | Signing on your laptop, a key in AWS KMS, keyless CI signing with OIDC, publishing a public key, requiring verification, GoReleaser, rotation and break-glass. |

Part 1 of the CLI series is the exception: it has been migrated, and
[Build your first CLI](build-your-first-cli.md) is the current version of it.

Those posts were written against earlier releases — the CLI series against
v0.6.0 — so a step may not match what you see. Check the
[migration notes](../reference/migration/index.md) when it does not. Where both a
post and a page under `tutorials/` exist, the page here is canonical.
