---
title: Add Tool Calling to an AI Command
description: How to expose Go functions as tools the AI can call, using chat.SetTools and the built-in ReAct loop.
date: 2026-03-25
tags: [how-to, ai, tools, tool-calling, react, agents]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Add Tool Calling to an AI Command

Tool calling lets the AI decide, during a conversation, which of your Go functions to invoke. GTB's `chat` package handles the ReAct (Reason → Act → Observe) loop automatically — you define the tools, and the framework manages the back-and-forth until the AI produces a final text answer.

---

## How the ReAct Loop Works

When you call `client.Chat(ctx, prompt)`:

1. The AI reasons about the prompt and decides whether to call a tool.
2. If it calls a tool, GTB invokes your `Handler` function and feeds the result back.
3. Steps 1–2 repeat until the AI produces a final response (no more tool calls) or `MaxSteps` is reached (default 20).

`Chat` returns the final text response. Tool errors are returned to the AI as error strings — the loop continues rather than aborting.

---

## Step 1: Define Your Tools

Each `chat.Tool` has a name, description, JSON Schema for parameters, and a handler function:

```go
import (
    "context"
    "encoding/json"

    "github.com/invopop/jsonschema"
    "gitlab.com/phpboyscout/go-tool-base/pkg/chat"
)

// Parameter structs must be exported and tagged for schema generation.
type ReadFileParams struct {
    Path string `json:"path" jsonschema_description:"Relative path to the file"`
}

type SearchParams struct {
    Query     string `json:"query" jsonschema_description:"The search term"`
    Directory string `json:"directory,omitempty" jsonschema_description:"Directory to search in (default: current)"`
}

var readFileTool = chat.Tool{
    Name:        "read_file",
    Description: "Read the contents of a file at the given path",
    Parameters:  jsonschema.Reflect(&ReadFileParams{}).Definitions["ReadFileParams"],
    Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
        var params ReadFileParams
        if err := json.Unmarshal(args, &params); err != nil {
            return nil, err
        }
        content, err := os.ReadFile(params.Path)
        if err != nil {
            return nil, err
        }
        return string(content), nil
    },
}

var searchTool = chat.Tool{
    Name:        "search_files",
    Description: "Search for a pattern in files under a directory",
    Parameters:  jsonschema.Reflect(&SearchParams{}).Definitions["SearchParams"],
    Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
        var params SearchParams
        if err := json.Unmarshal(args, &params); err != nil {
            return nil, err
        }
        dir := params.Directory
        if dir == "" {
            dir = "."
        }
        // ... run grep/walk and return results
        return results, nil
    },
}
```

---

## Step 2: Create the Client and Register Tools

```go
func NewCmdAnalyse(p *props.Props) *setup.Command {
    return setup.Wrap("analyse", &cobra.Command{
        Use:   "analyse",
        Short: "Use AI to analyse the codebase",
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := cmd.Context()

            client, err := chat.New(ctx, p, chat.Config{
                Provider:     chat.ProviderClaude,
                SystemPrompt: "You are a code analysis assistant. Use the provided tools to explore the codebase before answering.",
                MaxSteps:     15,  // limit ReAct iterations
            })
            if err != nil {
                return err
            }

            if err := client.SetTools([]chat.Tool{readFileTool, searchTool}); err != nil {
                return err
            }

            response, err := client.Chat(ctx,
                "Find all files that import the 'database/sql' package and summarise what each one does.")
            if err != nil {
                return err
            }

            p.Logger.Print(response)
            return nil
        },
    })
}
```

---

## Step 3: Observe Tool Execution

GTB logs each tool call at `INFO` level and the parameters at `DEBUG` level automatically:

```
INFO  Tool Call  tool=read_file
DEBUG Tool Parameters  tool=read_file args={"path":"pkg/db/client.go"}
INFO  Tool executed successfully  tool=read_file
```

This requires no extra wiring — the `executeTool` function in `pkg/chat/tools.go` handles it.

---

## Using Props in Tool Handlers

Handlers are plain closures — capture `props` (or any dependency) from the outer scope:

```go
func makeConfigTool(p *props.Props) chat.Tool {
    return chat.Tool{
        Name:        "get_config",
        Description: "Read a configuration value by key",
        Parameters:  jsonschema.Reflect(&GetConfigParams{}).Definitions["GetConfigParams"],
        Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
            var params GetConfigParams
            if err := json.Unmarshal(args, &params); err != nil {
                return nil, err
            }

            if !p.Config.Has(params.Key) {
                return nil, fmt.Errorf("config key %q not found", params.Key)
            }

            return p.Config.GetString(params.Key), nil
        },
    }
}
```

---

## Combining Tools with Structured Output

Tools work alongside `Ask` if you want a structured final response after exploration:

```go
// First, let the AI explore using Chat (tools enabled)
exploration, err := client.Chat(ctx, "Examine the database package and list what you find.")

// Then ask for a structured summary (same conversation history is preserved)
var report DatabaseReport
err = client.Ask(ctx, "Based on your exploration, produce a structured summary.", &report)
```

Because `Add`, `Chat`, and `Ask` all share the same conversation history on a given client instance, you can interleave them freely.

---

## Controlling Loop Depth

`MaxSteps` limits how many tool-call iterations the loop will execute:

```go
chat.Config{
    MaxSteps: 5,   // AI gets at most 5 tool calls before being forced to answer
}
```

The default is 20. Set it lower for interactive commands where latency matters, higher for deep analysis tasks.

---

## Testing

Mock `ChatClient` and verify `SetTools` is called with the expected tool names:

```go
mockClient := mock_chat.NewMockChatClient(t)

mockClient.EXPECT().
    SetTools(mock.MatchedBy(func(tools []chat.Tool) bool {
        names := make([]string, len(tools))
        for i, t := range tools { names[i] = t.Name }
        return slices.Contains(names, "read_file") &&
               slices.Contains(names, "search_files")
    })).
    Return(nil)

mockClient.EXPECT().
    Chat(mock.Anything, mock.Anything).
    Return("Found 3 files using database/sql", nil)
```

---

## Related Documentation

- **[Structured AI Responses](structured-ai-responses.md)** — using `Ask` for typed output without tool calling
- **[AI Provider Setup](ai-integration.md)** — token and provider configuration
- **[Chat component](../explanation/components/chat/index.md)** — full `ChatClient` and `Tool` API reference
