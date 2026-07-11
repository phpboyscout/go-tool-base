---
title: Build a Command with Structured AI Responses
description: How to use chat.Ask to receive typed, JSON-schema-validated responses from an AI provider.
date: 2026-03-25
tags: [how-to, ai, chat, structured-output, json-schema]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Build a Command with Structured AI Responses

`chat.Ask` lets you send a question to an AI and receive the answer unmarshalled directly into a Go struct. This is the right approach when you need deterministic, parseable output — code analysis, classification, extraction, or any workflow where you can't rely on free-form text.

---

## Prerequisites

An AI provider must be configured. See **[AI Provider Setup](ai-integration.md)** for token configuration. The examples below use Claude, but all providers support `Ask`.

---

## Step 1: Define Your Response Schema

Design a struct that represents the data you want back. Tags control JSON field names:

```go
type CodeReview struct {
    Summary  string   `json:"summary"`
    Issues   []Issue  `json:"issues"`
    Score    int      `json:"score"`   // 0-100
    Approved bool     `json:"approved"`
}

type Issue struct {
    Severity    string `json:"severity"`    // "error", "warning", "info"
    File        string `json:"file"`
    Line        int    `json:"line"`
    Description string `json:"description"`
    Suggestion  string `json:"suggestion"`
}
```

---

## Step 2: Create the Client with a Schema

Pass `ResponseSchema` to enforce the output structure. The framework generates the JSON Schema from your struct automatically:

```go
import (
    "context"

    "gitlab.com/phpboyscout/go-tool-base/pkg/chat"
    "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func analyseCode(ctx context.Context, p *props.Props, code string) (*CodeReview, error) {
    client, err := chat.NewFromProps(ctx, p, chat.Config{
        Provider:          chat.ProviderClaude,
        SystemPrompt:      "You are a senior Go code reviewer. Be concise and actionable.",
        ResponseSchema:    CodeReview{},
        SchemaName:        "code_review",
        SchemaDescription: "Structured code review with issues and score",
    })
    if err != nil {
        return nil, err
    }

    var result CodeReview
    question := "Review this Go code and identify any issues:\n\n```go\n" + code + "\n```"

    if err := client.Ask(ctx, question, &result); err != nil {
        return nil, err
    }

    return &result, nil
}
```

---

## Step 3: Use the Result in a Command

```go
func NewCmdReview(p *props.Props) *setup.Command {
    return setup.Wrap("review", &cobra.Command{
        Use:   "review [file]",
        Short: "AI-powered code review",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            code, err := os.ReadFile(args[0])
            if err != nil {
                return err
            }

            review, err := analyseCode(cmd.Context(), p, string(code))
            if err != nil {
                return err
            }

            p.Logger.Info("Review complete",
                "score", review.Score,
                "issues", len(review.Issues),
                "approved", review.Approved,
            )

            for _, issue := range review.Issues {
                p.Logger.Warn(issue.Description,
                    "severity", issue.Severity,
                    "file", issue.File,
                    "line", issue.Line,
                    "suggestion", issue.Suggestion,
                )
            }

            return nil
        },
    })
}
```

---

## Multi-Turn Context with `Add`

Use `Add` to build up conversation context before calling `Ask`. This is useful when you want to provide reference material without it being part of the question itself:

```go
client, err := chat.NewFromProps(ctx, p, chat.Config{
    Provider:       chat.ProviderClaude,
    ResponseSchema: CodeReview{},
    SchemaName:     "code_review",
})

// Add context without triggering a completion
_ = client.Add(ctx, "Here is the project's style guide:\n\n" + styleGuide)
_ = client.Add(ctx, "Here is the existing test file:\n\n" + testFile)

// Now ask the question — the context is included
var result CodeReview
_ = client.Ask(ctx, "Review the following implementation for style guide compliance:\n\n"+code, &result)
```

Message history accumulates across calls on the same client instance. Create a new client via `chat.New` to start a fresh conversation.

---

## Asking Without a Schema (Plain Text Response)

If you omit `ResponseSchema`, `Ask` returns the raw text content. Pass a `*string` as target:

```go
client, _ := chat.NewFromProps(ctx, p, chat.Config{
    Provider:     chat.ProviderClaude,
    SystemPrompt: "You are a helpful assistant.",
})

var answer string
_ = client.Ask(ctx, "Summarise this in one sentence: "+longText, &answer)
fmt.Println(answer)
```

---

## Choosing a Model

Override the default model for the provider:

```go
chat.Config{
    Provider: chat.ProviderClaude,
    Model:    "claude-opus-4-8",   // default: claude-opus-4-8
}

chat.Config{
    Provider: chat.ProviderOpenAI,
    Model:    "gpt-5.4",
}

chat.Config{
    Provider: chat.ProviderGemini,
    Model:    "gemini-2.5-pro",
}
```

---

## Using an OpenAI-Compatible Local Model

For Ollama or other local inference servers:

```go
chat.Config{
    Provider: chat.ProviderOpenAICompatible,
    BaseURL:  "http://localhost:11434/v1",
    Model:    "llama3.2",
    Token:    "ollama",  // Ollama accepts any non-empty token
}
```

---

## Testing

In tests, avoid live API calls by mocking `ChatClient`:

```go
import mock_chat "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/chat"

func TestAnalyseCode(t *testing.T) {
    mockClient := mock_chat.NewMockChatClient(t)

    expected := CodeReview{Score: 85, Approved: true}
    mockClient.EXPECT().
        Ask(mock.Anything, mock.MatchedBy(func(q string) bool {
            return strings.Contains(q, "Review this Go code")
        }), mock.AnythingOfType("*main.CodeReview")).
        RunAndReturn(func(_ context.Context, _ string, target any) error {
            *(target.(*CodeReview)) = expected
            return nil
        })

    // Inject mockClient into your function or command
}
```

---

## Related Documentation

- **[AI Tool Calling](ai-tool-calling.md)** — letting the AI call functions in a ReAct loop
- **[AI Provider Setup](ai-integration.md)** — provider configuration and API keys
- **[Chat component](../explanation/components/chat/index.md)** — full `ChatClient` interface reference
