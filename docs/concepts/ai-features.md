---
title: AI-Powered Features
description: Build intelligent features by consuming AI services for analysis and code generation.
date: 2026-03-18
tags: [concepts, ai, llm, features]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# AI-Powered Features

While MCP allows *external* agents to use your tool, the `pkg/chat` component allows your tool to **consume AI services** to provide intelligent features directly to your users—such as automated analysis, code generation, or natural language interfaces.

## The Unified Chat Client

GTB provides a robust abstraction layer over multiple AI providers. A single `ChatClient` interface covers all supported backends, so your feature code never needs to change when you swap providers.

### Supported Providers

| Provider | Value | Notes |
| :--- | :--- | :--- |
| **OpenAI** | `chat.ProviderOpenAI` | Default provider. Industry standard. |
| **Claude (Anthropic)** | `chat.ProviderClaude` | Excellent for coding and long-context analysis. |
| **Gemini (Google)** | `chat.ProviderGemini` | Strong performance and massive context windows. |
| **Claude Local** | `chat.ProviderClaudeLocal` | Uses the locally installed `claude` binary. No API key required. Ideal for secure environments where direct API access is restricted. |
| **OpenAI-Compatible** | `chat.ProviderOpenAICompatible` | Any OpenAI-compatible endpoint — Ollama, Groq, Fireworks, LM Studio, and others. Requires `BaseURL` in config. |

### Extending with Custom Providers

The provider registry is open for extension. Any package can register a new provider without modifying `pkg/chat`:

```go
func init() {
    chat.RegisterProvider("my-provider", func(ctx context.Context, p *props.Props, cfg chat.Config) (chat.ChatClient, error) {
        return &MyProviderClient{token: cfg.Token}, nil
    })
}
```

Once registered, the provider is available to all `chat.New()` callers by name.

## Core Capabilities

### 1. Autonomous Orchestration (Tool-Calling)
The most powerful feature of `pkg/chat` is its ability to handle **Autonomous Execution Loops**. When you provide a list of local functions (Tools) to the client:

- The AI decides which tool to call based on the user's prompt.
- The framework **automatically executes** the local Go function.
- The results are fed back to the AI for final reasoning.
- This continues in a loop (up to 20 steps) until the task is complete.

!!! note "ProviderClaudeLocal and Tools"
    `ProviderClaudeLocal` does not support tool calling in the current release. Tool integration via MCP server is planned for a future release. Use `ProviderClaude` or another API-backed provider when your feature requires `SetTools`.

### 2. Structured Data Extraction (`Ask`)
Beyond simple chat, you often need the AI to return data in a specific format (e.g., a list of bug fixes or a JSON configuration). The `Ask` method uses **JSON Schema** to enforce a strict response format:
- Define a Go `struct`.
- The framework generates the schema and instructs the AI to adhere to it.
- The result is automatically unmarshaled back into your Go object.

### 3. Context & Token Management
`pkg/chat` handles the complexities of building a reliable agent:

- **History Tracking**: Manages multi-turn conversations automatically.
- **Token Chunking**: Automatically splits large inputs to fit within the model's context window. Falls back to `cl100k_base` encoding for models not recognised by the tokenizer (e.g. Ollama models).
- **System Prompts**: Easily set the "personality" and behavioral constraints of your internal agent.

## Agentic vs. Legacy Workflows

When building AI-powered features, it is helpful to distinguish between "Legacy" single-action patterns and the "Agentic" patterns enabled by GTB.

### Legacy: The One-Way Prompt

In a legacy workflow, the interaction is linear and deterministic:

1. **Request**: The user sends a prompt.
2. **Execution**: The model processes the input in a single pass.
3. **Response**: The model returns a static response.

This approach is brittle for complex tasks. If the AI needs a piece of information it doesn't have, it must either "hallucinate" a guess or fail. The developer is forced to front-load as much context as possible into the prompt (context-stuffing), which is expensive and often leads to lower-quality reasoning.

### Agentic: The Iterative Loop

GTB shifts the focus toward **Agentic Workflows**. Instead of trying to solve the entire problem in one shot, the AI is given a set of "senses"—your CLI commands and library functions.

1. **Reasoning**: The AI analyzes the request and decides on a *first step*.
2. **Action**: It calls a local tool (e.g., `ReadDir` or `GetConfig`).
3. **Observation**: It receives the *actual results* from your system.
4. **Correction**: Based on the observation, it updates its plan.
5. **Finality**: It repeats this until it has sufficient information to provide a verified answer.

!!! note "The Philosophy of Verification"
    In an agentic workflow, the AI doesn't just *say* it fixed a bug; it uses a `Test` tool to *verify* the fix before reporting success. This transforms the AI from a creative writer into a reliable collaborator.

## Example: Agentic Tooling

An agentic pattern involves giving the AI the ability to interact with your system. In this example, we provide a tool that allows the AI to "inspect a directory" before it answers a user request.

```go
// 1. Define the tool handler
inspectDir := func(ctx context.Context, args json.RawMessage) (interface{}, error) {
    var params struct { Path string `json:"path"` }
    json.Unmarshal(args, &params)
    return props.Assets.ReadDir(params.Path)
}

// 2. Package it as a Tool
tool := chat.Tool{
    Name:        "list_files",
    Description: "List files in a given directory path",
    Parameters:  chat.GenerateSchema[struct{ Path string `json:"path"` }]().(*jsonschema.Schema),
    Handler:     inspectDir,
}

// 3. Register and Run
props.Chat.SetTools([]chat.Tool{tool})

// The AI will call 'list_files' autonomously if the prompt requires it
response, err := props.Chat.Chat(ctx, "What templates are available in the assets folder?")
```

In this case, the AI doesn't guess what files exist; it calls `list_files`, sees the actual contents of the filesystem, and then provides a verified answer to the user.

## Example: Structured Extraction (`Ask`)
```go
// Define a structured response
type Analysis struct {
    Severity string `json:"severity"`
    Fix      string `json:"fix"`
}

// Ask the AI to perform the analysis
var result Analysis
err := props.Chat.Ask("Analyze this log file", &result)
```

By leveraging `pkg/chat`, you can transform simple CLI utilities into "smart" tools that reason about the data they process.
