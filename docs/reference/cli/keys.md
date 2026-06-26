---
title: keys Command
description: Framework-developer command to generate, mint, and publish OpenPGP keys for release-binary signing.
date: 2026-06-26
tags: [reference, commands, keys, signing, openpgp, gtb]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# `keys` Command

`gtb keys` manages OpenPGP keys used for release-binary signing. It is part of the
**framework-developer** CLI. See the [Mint a Signing Key](../../how-to/mint-signing-key.md),
[Generate a Rotation Key](../../how-to/generate-rotation-key.md), and
[Publish WKD](../../how-to/publish-wkd.md) how-tos for the operational procedures,
and [Signing](../../explanation/components/signing.md) /
[OpenPGP key minting](../../explanation/components/openpgpkey.md) for the design.

## Usage

```bash
gtb keys <subcommand> [flags]
```

## Subcommands

| Subcommand | Purpose |
|---|---|
| `generate` | Generate a fresh keypair locally (Ed25519 or RSA) and emit both halves. |
| `mint` | Mint an ASCII-armored public key from an existing signer (e.g. a KMS key). |
| `wkd <public-key.asc>…` | Build a Web Key Directory tree from one or more public keys. |

### `keys generate`

| Flag | Default | Description |
|------|---------|-------------|
| `--algorithm` | *(Ed25519)* | Key algorithm: `ed25519` or `rsa`. |
| `--rsa-bits` | *(default)* | RSA key size when `--algorithm rsa`. |
| `--name` / `--email` | — | User-id name and email on the key. |
| `--output` | — | Path to write the public key. |
| `--private-output` | — | Path to write the private key. |
| `--created` | *(now)* | Fixed creation timestamp (for reproducible keys). |
| `--force` | `false` | Overwrite existing output files. |

### `keys mint`

Mint a public key from an existing signer (the private half never leaves its HSM/KMS).

| Flag | Default | Description |
|------|---------|-------------|
| `--backend` | — | Signing backend (e.g. `aws-kms`, `signing-local`). |
| `--key-id` | — | Key id/ARN/alias (or PEM path for the local backend). |
| `--name` / `--email` | — | User-id on the minted key. |
| `--output` | `release.asc` | Path to write the ASCII-armored public key. |
| `--created` | *(now)* | Fixed creation timestamp. |
| `--force` | `false` | Overwrite the output file. |

### `keys wkd`

Build a [Web Key Directory](../../explanation/components/openpgpkey.md) tree.
Takes one or more public-key files as arguments.

| Flag | Default | Description |
|------|---------|-------------|
| `--domain` | — | WKD domain. |
| `--email` | — | Email(s) to publish (repeatable). |
| `--output` | `./wkd-staging` | Staging directory for the generated tree. |
| `--method` | `advanced` | URL layout: `advanced` (served from `openpgpkey.<domain>`) or `direct` (from `<domain>`). |
| `--submission-address` | — | WKD submission address. |

> Run any subcommand with `--help` for the complete, authoritative flag set.
