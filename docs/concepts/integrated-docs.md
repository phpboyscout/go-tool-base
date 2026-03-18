---
title: Integrated Documentation
description: The built-in TUI documentation browser and AI-powered Q&A system.
date: 2026-02-16
tags: [concepts, docs, tui, ai]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Integrated Documentation

GTB includes a powerful, built-in documentation system designed to make your CLI tools more accessible, discoverable, and user-friendly. Instead of forcing users to switch to a browser or struggle with primitive man pages, GTB brings rich, interactive documentation directly into the terminal.

## The Design Philosophy

The `docs` component was created with a specific vision:

- **CLI-First Experience**: Documentation should be lived where the work is done - the terminal.
- **Streamlined Discovery**: TUI-based navigation is more intuitive than searching through a single monolithic `--help` output.
- **AI-Powered Intelligence**: Natural language Q&A provides immediate, contextual answers without requiring the user to read 100 pages of text.

## Core Capabilities

### 1. Interactive TUI Browser

The `docs` command launches a feature-rich Terminal User Interface (TUI) powered by **Bubble Tea**. It provides:

- **Sidebar Navigation**: Automatically parsed from your tool's `mkdocs.yml` structure.
- **Rich Rendering**: Markdown is rendered with full color and styling using **Glamour**.
- **Instant Search**: Find content across all pages, including support for Regex queries.
- **Integrated Sidebar**: Toggle between the page tree and the content view seamlessly.

### 2. AI Documentation Assistant (`ask`)

The most transformative feature is the `docs ask` subcommand. By integrating with your configured AI provider (OpenAI, Claude, or Gemini):

- **Universal Context**: The framework automatically collates all local Markdown files as context for the model.
- **Direct Answers**: Instead of searching for "how to configure X", users just ask the question and get a precise, technical response.
- **Zero Hallucination**: The AI is instructed to answer *only* based on your tool's actual documentation.

## Why this is better than Man Pages

While traditional man pages are the UNIX standard, they lack the interactive and conversational features modern developers expect:

| Feature | `man` Pages / `--help` | Integrated `docs` |
| :--- | :--- | :--- |
| **Formatting** | Plain Text / Monospaced | Rich Markdown (Colors, Tables, Lists) |
| **Navigation** | Scrolling / Linear | Trees, Links, and Sidebars |
| **Search** | Grep / Pattern match | Real-time TUI search + Regex |
| **Intelligence** | Static Text | AI-powered Q&A Assistant |
| **Discovery** | Requires Knowing Name | Structural Browsing |

By embedding your project's `docs/` folder into your binary (see [Asset Management](assets.md)), your users gain a world-class documentation platform that follows them wherever they install your tool.
