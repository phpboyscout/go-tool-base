---
title: AI Agents & MCP
description: Expose your CLI as an autonomous agent using the Model Context Protocol (MCP).
date: 2026-02-16
tags: [concepts, ai, mcp, agents]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# AI Agents & MCP

GTB enables your CLI applications to act as powerful autonomous agents through native support for the **Model Context Protocol (MCP)**. Instead of just being a manual tool, your application can become a "brain extension" for an AI assistant.

## The Model Context Protocol (MCP)

MCP is an open standard that allows AI models (like Claude or Gemini) to safely discover and interact with local tools. By implementing this protocol, GTB removes the need for custom integrations or wrapper scripts for every different AI service.

### How it Works

When you build a CLI with GTB, the framework automatically maps your **Cobra command tree** to a set of **MCP Tool Definitions**:

- **Command Name** -> **Tool Name**
- **Short Description** -> **Tool Description**
- **Flags & Arguments** -> **JSON Schema Parameters**

## Exposing your Tool

Every GTB application includes a built-in `mcp` command. This command starts a JSON-RPC server over standard I/O:

```bash
mytool mcp
```

### Integration with AI Assistants

To use your tool as an agent, you simply configure your preferred AI client (like Claude Desktop) to run your tool in MCP mode. The assistant will:

1.  **Call `mytool mcp`** on startup.
2.  **Discover** all available commands as tools.
3.  **Contextually call** your commands when a user's prompt requires it.

## Why use MCP?

- **Universal Compatibility**: Write once, and your tool works across any AI assistant that supports the protocol.
- **Zero Effort**: No extra code required. If it's a command in your CLI, it's a tool for the agent.
- **Safety & Control**: The AI is restricted to the specific commands and parameters you've defined in your tool's manifest.

---

!!! tip
    To see how to configure specific AI clients (like Cursor or Claude Desktop) to use your tool as an agent, refer to the [MCP CLI Guide](../cli/mcp.md).
