---
title: sign Command
description: Framework-developer command to produce an OpenPGP detached signature for a file using a configured backend.
date: 2026-06-26
tags: [reference, commands, sign, signing, openpgp, gtb]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# `sign` Command

`gtb sign <input-file>` produces an ASCII-armored OpenPGP **detached** signature
for a file using a configured signing backend. It is part of the
**framework-developer** CLI. See [Sign Releases](../../how-to/sign-releases.md) for
the CI procedure and [Signing](../../explanation/components/signing.md) for the
backend model.

## Usage

```bash
gtb sign <input-file> [flags]
```

Exactly one input file is required. The signature is written next to the input (or
to `--output`).

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--backend` | — | Signing backend (`aws-kms`, `signing-local`, …). |
| `--key-id` | — | Key id/ARN/alias (or PEM path for the local backend). |
| `--public-key` | — | Path to the public key the signature identifies. |
| `--output` | *(input + `.asc`)* | Path to write the detached signature. |
| `--created` | *(now)* | Fixed signature creation timestamp (for reproducibility). |

> Run with `--help` for the complete, authoritative flag set.
