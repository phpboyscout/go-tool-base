---
title: AI Provider Setup
description: Setting up and configuring AI providers (Claude, OpenAI, Gemini, Claude Local, and OpenAI-compatible endpoints).
date: 2026-03-18
tags: [how-to, ai, configuration, setup]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# AI Provider Setup

Global AI features (like documentation Q&A) require a configured AI provider.

## Step 1: Enable AI

In your `main.go`, ensure that the AI feature is enabled:

```go
        Features: props.SetFeatures(
            props.Enable(props.AiCmd),
        ),
```

## Step 2: Configure Provider

Run your tool's built-in initialization command specifically for AI:

```bash
mytool init ai
```

This will launch an interactive form where you can:
1. Select your preferred provider (**Claude**, **OpenAI**, **Gemini**, **Claude Local**, or an **OpenAI-compatible** endpoint).
2. Enter your API key (not required for **Claude Local**).

The configuration will be saved to your local `config.yaml` under the appropriate provider section.

### Environment Variable Overrides

For CI/CD environments or temporary overrides, you can configure the AI client using environment variables. These always take precedence over the `config.yaml`.

| Feature | Environment Variable | Valid Values |
| :--- | :--- | :--- |
| **Provider Selection** | `AI_PROVIDER` | `openai`, `claude`, `gemini`, `claude-local`, `openai-compatible` |
| **OpenAI Key** | `OPENAI_API_KEY` | — |
| **Claude Key** | `ANTHROPIC_API_KEY` | — |
| **Gemini Key** | `GEMINI_API_KEY` | — |

!!! note
    If an environment variable is set, it will automatically override the value found in your local configuration files. `claude-local` and `openai-compatible` providers do not require an API key via environment variable.

## Step 3: Use AI Documentation

Once configured, you can query your tool's documentation using natural language:

```bash
mytool docs ask "How do I add a new command?"
```

The tool will use the configured provider to analyze the local documentation and provide a structured, relevant answer.

---

## Provider-Specific Configuration

Each AI provider has its own characteristics. This section provides guidance for optimal configuration with each.

### OpenAI

OpenAI is the default provider and offers excellent general-purpose performance.

**Recommended Models:**

| Model | Best For | Notes |
| :--- | :--- | :--- |
| `gpt-4o` | General use, complex reasoning | Best overall quality |
| `gpt-4o-mini` | Fast responses, cost efficiency | Good for simple queries |
| `gpt-4-turbo` | Long context tasks | Handles large documentation |

**Configuration Example:**

```yaml
# config.yaml
ai:
  provider: openai
openai:
  api:
    key: "sk-..."
  model: gpt-4o
```

**Environment Variables:**

```bash
export AI_PROVIDER=openai
export OPENAI_API_KEY=sk-...
```

### Claude (Anthropic)

Claude excels at nuanced understanding and following complex instructions.

**Recommended Models:**

| Model | Best For | Notes |
| :--- | :--- | :--- |
| `claude-sonnet-4-6` | General use, balanced | Recommended default |
| `claude-haiku-4-5` | Fast responses | More economical |
| `claude-opus-4-6` | Complex analysis | Highest capability |

**Configuration Example:**

```yaml
# config.yaml
ai:
  provider: claude
anthropic:
  api:
    key: "sk-ant-..."
  model: claude-sonnet-4-6
```

**Environment Variables:**

```bash
export AI_PROVIDER=claude
export ANTHROPIC_API_KEY=sk-ant-...
```

### Gemini (Google)

Gemini offers strong performance and massive context windows.

**Recommended Models:**

| Model | Best For | Notes |
| :--- | :--- | :--- |
| `gemini-2.0-flash` | Fast responses | Good for simple queries |
| `gemini-2.5-pro` | Complex reasoning | Larger context window |

**Configuration Example:**

```yaml
# config.yaml
ai:
  provider: gemini
gemini:
  api:
    key: "AIza..."
  model: gemini-2.0-flash
```

**Environment Variables:**

```bash
export AI_PROVIDER=gemini
export GEMINI_API_KEY=AIza...
```

### Claude Local

`ProviderClaudeLocal` routes requests through the locally installed `claude` CLI binary instead of the Anthropic API. This is valuable in **secure or air-gapped environments** where direct outbound HTTPS to `api.anthropic.com` is blocked, but the pre-authenticated `claude` binary is permitted.

**Requirements:**
- `claude` CLI installed and authenticated (`claude login`)
- Binary must be in `PATH`
- No API key required

**Installation:**

```bash
# Install the Claude CLI (macOS/Linux)
npm install -g @anthropic-ai/claude-code

# Authenticate once
claude login
```

**Configuration Example:**

```yaml
# config.yaml
ai:
  provider: claude-local
  model: claude-sonnet-4-6  # optional; uses claude's default if empty
```

**Environment Variables:**

```bash
export AI_PROVIDER=claude-local
# No API key needed
```

!!! note "Tool Calling Not Supported"
    `ProviderClaudeLocal` does not support `SetTools` in the current release. If your feature requires tool calling, use `ProviderClaude` or another API-backed provider. MCP-based tool integration is planned for a future release.

### OpenAI-Compatible Endpoints

`ProviderOpenAICompatible` targets any backend that exposes an OpenAI-compatible API. This unlocks a wide range of local and cloud backends without additional per-provider code.

**Supported backends include:** Ollama, Groq, Fireworks AI, Together AI, LM Studio, vLLM, and others.

**Requirements:**
- `BaseURL` must be set (the endpoint's base URL)
- `Model` must be set (no default — model names are backend-specific)

#### Ollama (Local)

```yaml
# config.yaml
ai:
  provider: openai-compatible
  base_url: "http://localhost:11434/v1"
  model: llama3.2
openai:
  api:
    key: "ollama"  # Ollama ignores the token; any non-empty value works
```

```bash
export AI_PROVIDER=openai-compatible
# BaseURL and model must be set via config or programmatically
```

#### Groq (Cloud)

```yaml
# config.yaml
ai:
  provider: openai-compatible
  base_url: "https://api.groq.com/openai/v1"
  model: llama-3.3-70b-versatile
openai:
  api:
    key: "gsk_..."  # Your Groq API key
```

!!! tip "Unknown Model Names"
    For model names not recognised by the standard tokenizer (e.g. Ollama and Groq models), `pkg/chat` automatically falls back to `cl100k_base` encoding so chunking remains accurate without manual configuration.

---

## Programmatic Usage

Beyond the `docs ask` command, you can use the AI chat client directly in your code:

### Basic Chat

```go
import "github.com/phpboyscout/gtb/pkg/chat"

func analyzeError(ctx context.Context, p *props.Props, errorLog string) (string, error) {
    client, err := chat.New(ctx, p, chat.Config{
        Provider:     chat.ProviderClaude,
        Model:        "claude-sonnet-4-6",
        SystemPrompt: "You are an expert at analyzing error logs and suggesting fixes.",
    })
    if err != nil {
        return "", err
    }

    return client.Chat(ctx, fmt.Sprintf("Analyze this error:\n%s", errorLog))
}
```

### Structured Responses

Force the AI to return structured data:

```go
type ErrorAnalysis struct {
    Severity    string   `json:"severity" jsonschema:"enum=low,enum=medium,enum=high"`
    RootCause   string   `json:"root_cause"`
    Suggestions []string `json:"suggestions"`
}

func analyzeErrorStructured(ctx context.Context, p *props.Props, errorLog string) (*ErrorAnalysis, error) {
    client, err := chat.New(ctx, p, chat.Config{
        Provider:     chat.ProviderOpenAI,
        Model:        "gpt-4o",
        SystemPrompt: "Analyze errors and provide structured feedback.",
    })
    if err != nil {
        return nil, err
    }

    var result ErrorAnalysis
    err = client.Ask(fmt.Sprintf("Analyze: %s", errorLog), &result)
    return &result, err
}
```

---

## Troubleshooting

### Common Issues

| Issue | Cause | Solution |
| :--- | :--- | :--- |
| "No API key found" | Missing configuration | Run `mytool init ai` or set environment variable |
| "Invalid API key" | Incorrect or expired key | Verify key in provider dashboard, reconfigure |
| "Rate limit exceeded" | Too many requests | Wait or upgrade API plan |
| "Model not found" | Incorrect model name | Check provider docs for valid model names |
| "`claude` binary not found" | Claude Local — binary not installed or not in PATH | Install with `npm install -g @anthropic-ai/claude-code` and run `claude login` |
| "Model is required for ProviderOpenAICompatible" | Missing model in config | Set `model` in config — no default exists for compatible endpoints |

### Testing Configuration

Verify your AI configuration is working:

```bash
# Quick test with docs ask
mytool docs ask "Say hello"

# Check current provider (if using debug mode)
mytool docs ask --provider openai "What provider am I using?"
```

!!! tip "Provider Override"
    Use the `--provider` flag on `docs ask` to temporarily switch providers without changing your configuration.
