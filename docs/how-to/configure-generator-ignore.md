---
title: Configure Generator Ignore Rules
description: How to use .gtb/ignore to prevent the generator from overwriting customised files during regeneration.
date: 2026-03-31
tags: [how-to, generator, ignore, regeneration]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Configure Generator Ignore Rules

When you run `regenerate`, the GTB generator walks all skeleton template files and either writes or prompts to overwrite each one. If you've heavily customised certain files (CI workflows, build configs, Dockerfiles), you'll be prompted to decline overwrites every time.

The `.gtb/ignore` file lets you permanently mark files as "hands off" — the generator will skip them without prompting.

---

## Step 1: Create the Ignore File

Create `.gtb/ignore` in your project's `.gtb/` directory (alongside `manifest.yaml`):

```bash
touch .gtb/ignore
```

## Step 2: Add Patterns

The syntax is gitignore-like. Add one pattern per line:

```
# Ignore my custom CI workflows
.github/workflows/**

# But keep the release workflow managed by the generator
!.github/workflows/release.yml

# Ignore my custom build config
justfile

# Ignore Docker files
Dockerfile
docker-compose.yml
```

### Pattern Types

| Pattern | What it matches |
|---------|----------------|
| `justfile` | Exact filename in any directory |
| `*.yml` | All `.yml` files in any directory |
| `.github/**` | Everything under `.github/` |
| `.github/workflows/test.yml` | Exact path only |
| `!pattern` | Re-includes a file excluded by an earlier pattern |
| `# comment` | Ignored (comment line) |

Patterns are evaluated top-to-bottom. Later patterns override earlier ones — this is how negation works.

## Step 3: Regenerate

Run `regenerate` as normal:

```bash
gtb regenerate project
```

Ignored files will be skipped silently. You'll see debug output for each skipped file if you run with `--debug`:

```
DEBU Ignored by .gtb/ignore: justfile
DEBU Ignored by .gtb/ignore: .github/workflows/test.yml
```

## How Hashing Works

Ignored files are **still tracked** in the manifest. The generator reads the current on-disk content of each ignored file and records its hash. This means:

- The manifest stays accurate — it reflects what's actually on disk
- Future regenerations know the file exists and hasn't been touched by the generator
- If you remove a file from `.gtb/ignore` later, the generator can detect whether you've modified it since the last regeneration

If an ignored file doesn't exist on disk (e.g. you deleted it), no hash is recorded.

## Common Patterns

### Protect All CI, Keep Release Managed

```
.github/workflows/**
!.github/workflows/release.yml
```

### Protect Build Configuration

```
justfile
Makefile
Dockerfile
docker-compose.yml
```

### Protect Everything Except Go Code

```
*.yml
*.yaml
*.json
!go.mod
```

## Notes

- The `--force` flag does **not** override ignore rules. Ignored files stay ignored regardless.
- Missing `.gtb/ignore` is valid — the generator behaves exactly as before (no files ignored).
- Blank lines and lines starting with `#` are ignored.
- Patterns without a `/` match by filename (basename) in any directory.
- Patterns with a `/` match against the full relative path.

---

## Related Documentation

- [Generator Package](../components/internal/generator.md) — full generator architecture and ignore file format
- [Generator Ignore File Spec](../development/specs/2026-03-31-generator-ignore-file.md) — design specification
