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

### The CLI series

Each part builds on the `mytool` project from Build your first CLI.

- [Configure your CLI](configure-your-cli.md): settings with shipped defaults,
  overrides from the environment and from layered files, validation, and a
  setup step that asks the user. About twenty-five minutes.
- [Expose your CLI to AI agents](expose-your-cli-to-ai-agents.md): serve your
  commands over MCP and see exactly what an agent can reach. About fifteen
  minutes.
- [Build an AI-backed command](build-an-ai-command.md): a command that calls a
  model, with the rules, mechanics and state kept in your code. About thirty
  minutes, and it needs an API key.
- [Ship self-updates](ship-self-updates.md): watch the tool replace itself, and
  make it refuse an update it cannot verify. About fifteen minutes.

### The web-service series

Turns the same `mytool` project into a long-running service, one part at a
time.

- [Run a long-lived service](run-a-long-lived-service.md): a `serve` command
  that runs a service until it's stopped, shuts down cleanly on a signal, and
  exits non-zero when the service fails for good. About twenty minutes.
- [Serve gRPC over TLS](serve-grpc-over-tls.md): a protobuf contract, code
  generated with buf, a gRPC service with health and reflection on the same
  controller, then TLS from config alone. About forty minutes.
- [Add a REST API](add-a-rest-api.md): JSON over HTTPS beside the gRPC service,
  by hand on net/http and then from an OpenAPI spec, with health endpoints for
  both. About thirty-five minutes.
- [Serve a web page](serve-a-web-page.md): escaped HTML and static files from
  the same server, embedded in the binary. About fifteen minutes.
- [Put a gateway in front of gRPC](put-a-gateway-in-front.md): REST routes in
  the proto, a generated grpc-gateway, and the hand-written REST layer deleted.
  About thirty minutes.
- [Serve live API docs](serve-api-docs.md): an OpenAPI document generated from
  the proto, and an interactive docs site whose console calls the running API.
  About twenty minutes.
- [Trace a request end to end](trace-a-request.md): OpenTelemetry on both
  transports and in your own code, and one REST request followed through the
  gateway, gRPC and the store in Jaeger. About thirty minutes.

### The signing series

Signs your releases with a key the release host can't reach, so a tool can
refuse an update it can't verify. It doesn't need the `mytool` project.

- [Sign and verify on your laptop](sign-and-verify-on-your-laptop.md): make a
  key, mint the public key you'll publish, sign a checksums file, and verify it
  with plain `gpg`. About fifteen minutes, with no cloud account.


## Longer series, currently only on the blog

Most of the signing series is still published only on the creator's blog and
is being migrated here; until a page exists under `tutorials/`, the post is
the only copy.

| Series | Parts | Covers |
|---|---|---|
| [Sign your own binaries with go-tool-base](https://phpboyscout.uk/sign-your-own-binaries-with-go-tool-base-part-2/) | 2 to 7 | A key in AWS KMS, keyless CI signing with OIDC, publishing a public key, requiring verification, GoReleaser, rotation and break-glass. |

Those posts were written against earlier releases, so a step may not match what
you see. Check the [migration notes](../reference/migration/index.md) when it
does not. Where both a post and a page under `tutorials/` exist, the page here
is canonical.
