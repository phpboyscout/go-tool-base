---
title: Scaffolding a New CLI
description: Quick start guide for scaffolding a new CLI project.
date: 2026-02-16
tags: [how-to, quickstart, scaffolding, new-project]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Scaffolding a New CLI

The fastest way to get started with GTB is to use its dedicated generator. This handles all the project structure, boilerplate, and built-in command registration for you.

## Step 1: Install the CLI

Ensure you have the `gtb` binary installed on your path.

## Step 2: Initialize a Project

Run the `generate skeleton` command:

```bash
gtb generate skeleton --name mytool --github-org my-org
```

This will create a new directory `mytool` with the following structure:

- `cmd/mytool/main.go`: The orchestration layer.
- `cmd/mytool/assets/`: Default configuration files.
- `pkg/cmd/`: Implementation of your custom commands.
- `go.mod`: Initialized with the correct project name.

## Step 3: Add your first command

Instead of writing the boilerplate manually, use the command generator:

```bash
gtb generate command --name hello --summary "A simple greeting"
```

The generator will:
1. Create `pkg/cmd/hello/hello.go`.
2. Register the command in your `main.go`.
3. Add a placeholder test file.

## Step 4: Run it!

```bash
go run ./cmd/mytool hello
```

You now have a fully functional CLI with built-in support for updates, configuration, and AI agent integration!
