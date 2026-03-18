---
title: Agent Package
description: The Autonomous Repair Agent architecture for self-healing code generation.
date: 2026-02-16
tags: [components, internal, agent, ai, repair]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Agent Package

The `internal/agent` package defines the capabilities and environment for the **Autonomous Repair Agent**. This agent is responsible for the self-healing code generation feature, where AI-generated code is iteratively fixed until it compiles and passes tests.

## Overview

When the user runs `generate command --script` or `--prompt`, an AI loop is initiated. This loop allows the LLM (Large Language Model) to act as a "developer" exploring a sandbox environment. The `agent` package defines the **Tools** available to this developer.

## Available Tools

The agent does **not** have unrestricted shell access. It operates through a strictly defined set of tools defined in `tools.go` to ensure safety and determinism.

| Tool Name | Purpose | Implementation Details |
| :--- | :--- | :--- |
| `list_dir` | Explore the project structure | Lists files in the current sandbox. |
| `read_file` | Read code content | Returns file contents to the LLM context. |
| `write_file` | Create or update code | Writes code to the in-memory or on-disk filesystem. |
| `go_build` | Verify compilation | Runs `go build ./...` and captures `stderr`. |
| `go_test` | Verify functionality | Runs `go test ./...` and captures output. |
| `go_get` | Fix dependencies | Runs `go get <package>` to resolve missing imports. |
| `golangci_lint` | Enforce standards | Runs the project's linter configuration. |

## The Repair Loop

The repair process follows a **ReAct (Reason + Act)** pattern:

1.  **Draft**: The initial generation is written to the filesystem.
2.  **Verify**: The agent runs `go_build` and `go_test`.
3.  **Analyze**: If verification fails, the agent reads the error logs (compiler errors, test failures).
4.  **Reason**: The agent determines the root cause (e.g., "undefined variable", "import missing").
5.  **Act**: The agent calls `write_file` to patch the code or `go_get` to add a dependency.
6.  **Loop**: Steps 2-5 repeat until success or the maximum step limit (default: 15) is reached.

## Integration with Generator

The `internal/generator` package orchestrates this agent.

1.  `generator.go` initializes the project structure.
2.  It delegates to `ai.go` to create a `ChatClient` and an `Agent`.
3.  The `Agent` is initialized with the `props.FS` (Filesystem) and `props.Logger`.
4.  The loop executes, modifying the implementation files (`main.go`) directly.

### Security Strategy: Sandboxing

The agent's file operations are strictly confined to a `basePath` (typically the project root). This is enforced by the `ensurePathAllowed` helper, which prevents directory traversal attacks (`../`).

```go
func ensurePathAllowed(basePath, targetPath string) error {
    // changing /abs/base/../etc/passwd -> /etc/passwd
    absReq, _ := filepath.Abs(targetPath)

    // Check prefix
    if !strings.HasPrefix(absReq, absBase) {
        return ErrPathInvalid
    }
    return nil
}
```

## Tutorial: Building a New Agent Tool

This tutorial demonstrates how to add a `grep_search` tool to the agent, allowing the LLM to search for patterns in files.

### 1. Define the Tool Factory

Create a new file `internal/agent/search_tool.go` or add to `tools.go`.

```go
package agent

import (
    "context"
    "encoding/json"
    "fmt"
    "os/exec"

    "github.com/phpboyscout/gtb/pkg/chat"
    "github.com/invopop/jsonschema"
)

// GrepTool returns a tool for searching file contents
func GrepTool(basePath string) chat.Tool {
    return chat.Tool{
        Name:        "grep_search",
        Description: "Search for a string pattern in files recursively.",

        // 1. Define Parameters Schema using reflection
        Parameters: jsonschema.Reflect(struct {
            Pattern string `json:"pattern" jsonschema:"description=Regex pattern to search for"`
            Path    string `json:"path" jsonschema:"description=Directory to search in"`
        }{}),

        // 2. Implement the Handler
        Handler: func(ctx context.Context, args json.RawMessage) (interface{}, error) {
            // A. Parse Arguments
            var params struct {
                Pattern string `json:"pattern"`
                Path    string `json:"path"`
            }
            if err := json.Unmarshal(args, &params); err != nil {
                return nil, fmt.Errorf("invalid args: %w", err)
            }

            // B. Enforce Sandbox
            if err := ensurePathAllowed(basePath, params.Path); err != nil {
                return nil, err
            }

            // C. Execute Logic (ripgrep / grep)
            cmd := exec.CommandContext(ctx, "grep", "-r", params.Pattern, params.Path)
            output, err := cmd.CombinedOutput()
            if err != nil {
                // Return output even on failure (grep returns non-zero if not found)
                return fmt.Sprintf("Search completed (err=%v):\n%s", err, string(output)), nil
            }

            return string(output), nil
        },
    }
}
```

### 2. Register Your Tool

Update `internal/agent/ai.go` (or wherever the agent is initialized) to include your new tool in the definition list.

```go
// internal/agent/ai.go

func NewAgent(prop *props.Props) *chat.Agent {
    basePath := prop.Cwd

    tools := []chat.Tool{
        ListDirTool(basePath),
        ReadFileTool(basePath),
        GrepTool(basePath), // <--- Register here
    }

    return chat.NewAgent(client, tools)
}
```

## Architecture for Custom Agents

If you are building your own agent-based tool using this library as a foundation, follow these design patterns:

### 1. The Context-Tool-loop

The core loop is managed by the `chat.Client` (pkg/chat), but the *semantics* are defined here.

1.  **State**: Maintain a conversation history (`[]chat.Message`).
2.  **Iteration**:
    -   Send history to LLM.
    -   LLM responds with a `ToolCall`.
    -   Execute the tool locally.
    -   Append the `ToolResult` to history.
    -   Repeat.

### 2. Stateless vs Stateful Tools

-   **Stateless**: Most tools should be stateless functions (like `read_file`).
-   **Stateful**: If you need state (e.g., a "current selection"), pass a state object into the tool factory closure.

### 3. Error Handling as Feedback

Don't just log errors; return them to the LLM. The error message is often the most valuable feedback for the model to self-correct.

```go
if err != nil {
    // Return the error string so the LLM sees it
    return nil, fmt.Errorf("compilation failed: %w", err)
}
```

By treating errors as data, you enable the ReAct loop to actually work.

## Extending the Agent

To add new capabilities to the autonomous agent:

1.  **Define the Tool**: Add a new function definition in `internal/agent/tools.go`.
2.  ** Implement Logic**: Implement the handler that executes the tool (e.g., executing a new CLI command).
3.  **Register**: Add the tool definition to the tool registry passed to the LLM provider.

!!! note "Sandboxing"
    While the agent runs locally, it is constrained by the tools provided. It cannot arbitrarily delete files outside the project scope or execute dangerous system commands unless explicitly exposed as a tool.
