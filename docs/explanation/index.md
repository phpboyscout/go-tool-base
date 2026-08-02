---
title: Explanation
description: Why GTB is shaped the way it is — the architectural concepts behind the framework, and how each component is wired into it.
date: 2026-08-02
tags: [explanation, index, overview, concepts, components]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Explanation

Understanding-oriented material: why the framework is shaped the way it is, what
each decision buys, and what it costs.

Nothing here is a set of steps. For those, see the
[how-to guides](../how-to/index.md); for lookups, the
[reference](../reference/index.md).

## Concepts: how the framework thinks

**[Concepts](concepts/index.md)** covers the patterns that recur across every
part of GTB, rather than any one package.

- **[Architecture fundamentals](concepts/architecture.md)** — the command
  registry, the Props container, and the order things happen in.
- **[Functional options](concepts/functional-options.md)** — why constructors
  take options rather than growing parameters.
- **[Interface design](concepts/interface-design.md)** — what GTB makes an
  interface, and what it deliberately leaves concrete.
- **[Service orchestration](concepts/service-orchestration.md)** — how a
  long-running tool starts, stays healthy, and shuts down.
- **[The manifest](concepts/manifest.md)** and
  **[regeneration](concepts/regeneration.md)** — how the generator knows what
  your project contains and what it is allowed to rewrite.
- **[Documentation layout](concepts/documentation-layout.md)** — why generated
  docs land in Diátaxis quadrants, and the `docs_layout` setting that controls
  it.

## Components: how each piece is wired

**[Components](components/index.md)** describes the libraries a GTB tool is
built from — both the packages in this repository and the standalone
`go/…` modules the framework consumes.

The distinction matters when you go looking for API detail. A `pkg/…` package is
versioned with GTB and covered by its
[API stability policy](../reference/api-stability.md). A `go/…` module has its
own repository, release cadence and documentation site; the page here explains
**how GTB wires it**, and links out for the API itself.

Frequently read:

- **[Props](components/props.md)** — the dependency-injection container every
  command receives.
- **[Config](components/config/index.md)** — the layered store, the
  project-local layer, and the trust gate on its security keys.
- **[Credentials](components/credentials.md)** — the env / keychain / literal
  resolution chain.
- **[Setup](components/setup/index.md)** — features, initialisers and middleware.
- **[Update](components/update.md)** — self-update, and the signature and
  checksum verification around it.
- **[Errors](components/errors.md)** — the sentinel error catalogue, which is
  lint-enforced against the source.

## What explanation pages are not

They are not a substitute for the reference. If you want the exact default of a
key, the reference has it and this section deliberately does not repeat it —
duplicated defaults drift.

They are also not design records. A specification captures a decision at a point
in time; these pages describe how the software behaves now. Where the two
disagree, the code wins and the page is wrong.
