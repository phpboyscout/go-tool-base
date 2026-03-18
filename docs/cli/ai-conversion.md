---
title: AI Script Conversion 🤖
description: Documentation for the AI-powered script conversion and prompt-based command generation features.
date: 2026-02-16
tags: [cli, ai, generator, script-conversion]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# AI Script Conversion 🤖

Building a production-ready CLI shouldn't feel like a chore. Sometimes you have a perfectly good Python script or Bash prototype that you want to elevate into a robust, high-performance Go utility. With the `gtb` AI conversion engine, that transition is now just a single command away!

### Step 3: (Optional) AI Conversion or Prompting

Instead of manually implementing the command logic, you can have the AI do it for you in two ways:

#### A. Script Conversion
Provide an existing script (e.g., bash, python) that describes the logic:

```bash
gtb generate command --name backup --script ./tools/backup.sh
```

<video controls autoplay loop muted playsinline width="100%">
  <source src="../../tapes/script-demo.mp4" type="video/mp4">
</video>

#### B. Prompt-Based Generation
Provide a natural language description of what the command should do:

```bash
gtb generate command --name greeting --prompt "Implement a command that greets the user by name and optionally includes the current time if a --time flag is provided"
```

<video controls autoplay loop muted playsinline width="100%">
  <source src="../../tapes/prompt-demo.mp4" type="video/mp4">
</video>

You can also provide a path to a file containing the prompt:

```bash
gtb generate command --name complex-logic --prompt ./docs/prompts/logic.txt
```

This doesn't just "copy-paste" code; it re-imagines your logic as an idiomatic Go function, complete with structured logging, error handling, and unit tests.

## Multi-Provider AI Support 🧠

The conversion engine supports multiple AI providers, giving you the flexibility to choose the best model for your needs.

### Supported Providers

| Provider | Default Model | Env Var for API Key |
| :--- | :--- | :--- |
| **OpenAI** (Default) | `gpt-5.2` | `OPENAI_API_KEY` |
| **Claude** | `claude-sonnet-4-5` | `ANTHROPIC_API_KEY` |
| **Gemini** | `gemini-3-flash-preview` | `GEMINI_API_KEY` |

### Configuration ⚙️

You can configure the AI provider and model using CLI flags (recommended) or environment variables.

#### 1. Using CLI Flags (Priority)

Flags allow you to switch providers on the fly for a specific command generation.

```bash
# Use Claude with default model
go run main.go generate command -n restore --script ./restore.sh --provider claude

# Use Gemini with a specific model
go run main.go generate command -n backup --script ./backup.sh --provider gemini --model gemini-1.5-pro
```

#### 2. Using Environment Variables

For needed persistent configuration, you can set environment variables. Note that CLI flags will always override these settings.

```bash
export AI_PROVIDER=gemini
export AI_MODEL=gemini-3-flash-preview
export GEMINI_API_KEY=your_key_here

go run main.go generate command -n backup --script ./backup.sh
```

## The Autonomous Repair Agent 🛠️

We don't just generate code and hope for the best. Every AI-generated command is verified by an **Autonomous Agent** that uses a self-correcting ReAct (Reasoning and Acting) loop to ensure it meets our high standards for quality and stability.

### How it Works:

1.  **Drafting**: The AI generates the initial Go implementation and unit tests.
2.  **Autonomous Verification**: A dedicated repair agent takes over. It has access to a restricted set of tools to:
    -   `go_build`: Check for compilation errors.
    -   `go_test`: Run unit tests for functional correctness.
    -   `go_get`: Resolve any missing dependencies.
    -   `golangci_lint`: Ensure best practices and formatting.
3.  **Self-Correction**: Instead of a simple retry, the agent *analyzes* the error output, reads the relevant code files, and applies targeted fixes until the project is stable.
4.  **Completion**: The loop finishes when the project builds and tests pass successfully, or when the agent reaches its maximum reasoning steps.

!!! important "Autonomous Reliability"
    The agent operates in a **secure, restricted environment**. It cannot execute arbitrary shell commands, but it has everything it needs to ensure your Go code is production-ready.

### Falling Back to Legacy Mode

If you prefer the original, more predictable retry loop over the autonomous agent, you can use the `--agentless` flag:

```bash
go run main.go generate command -n "my-cmd" --script "./script.sh" --agentless
```

!!! tip "When to use Agentless"
    Legacy mode is useful if the autonomous agent is over-thinking simple fixes or if you want a faster, single-shot retry behavior without the multi-step reasoning loop.

## AI Troubleshooting

### Common Issues

1.  **Rate Limiting (429 Errors)**: If the AI provider is busy, you might see rate limit errors.

    -   Wait a minute and try again.
    -   Check your API quota.
    -   Try a different provider if available.

2.  **"Generation Failed"**: If the AI produces invalid code that the autonomous agent cannot repair:

    -   The generator will bail out safely.
    -   It will leave the best-attempt code in `main.go`.
    -   The logic will be commented out to prevent compilation errors.
    -   **Action**: Uncomment the code and fix the remaining syntax errors manually.

For more details, consult the [Troubleshooting Guide](../troubleshooting.md).

## Unit Test Generation 🧪

The AI conversion engine is particularly skilled at writing tests. It understands the `Props` architecture and will automatically:

-   Use `afero.NewMemMapFs()` for safe, in-memory filesystem testing.
-   Use a real logger with `io.Discard` for clean test output.
-   Follow a table-driven test pattern for maximum coverage.

## Pro-Tips for AI Conversion 💡

-   **Clean Scripts**: The better your source script is organized, the better the Go output will be.
-   **Review Recommendations**: Along with code, the AI often provides "Recommendations" for performance or architecture improvements. Check your terminal output!
-   **Iterate**: If the first conversion isn't perfect, you can refine your source script or manually tweak the generated `main.go` and let the linter do the rest.

Elevate your scripts today and let the AI do the heavy lifting! 🚀
