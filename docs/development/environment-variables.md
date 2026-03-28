---
title: Environment Variables & .env
description: How to use environment variables and localize setup with .env files.
tags: [config, environment, dotenv, setup]
---

# Environment Variables & .env

GTB supports configuration via environment variables, which is the preferred way to handle sensitive data like API keys and tokens during local development and in CI/CD.

## Automatic Environment Mapping

Any key in your `config.yaml` can be overridden by an environment variable. The mapping follows these rules:
1.  **Prefix**: None (direct mapping).
2.  **Separator**: Dots (`.`) in config keys are replaced with underscores (`_`).
3.  **Case**: Environment variables are case-insensitive but typically written in `UPPER_CASE`.

**Example:**
- Config: `ai.provider` -> Env: `AI_PROVIDER`
- Config: `auth.github.token` -> Env: `AUTH_GITHUB_TOKEN`

## Local Development with `.env`

To simplify local development, GTB automatically looks for a `.env` file in the current working directory. If found, it loads these variables into the process environment before initializing the configuration.

!!! tip "Standard Practice"
    Create a `.env` file in your project root for local overrides. **Never commit this file to Git.**

### Example `.env` file:

```bash
# AI Configuration
AI_PROVIDER=openai
OPENAI_API_KEY=sk-proj-xxxxxxxxxxxx

# Logging
LOG_LEVEL=debug

# Feature Flags
AUTH_SKIP_LOGIN=true
```

## Security Best Practices

!!! caution "Ignore .env Files"
    Always ensure `.env` is added to your project's `.gitignore` file to prevent accidental exposure of secrets.

```bash
# .gitignore
.env
.env.*
!.env.example
```

## Common Environment Variables

| Variable | Description |
| :--- | :--- |
| `LOG_LEVEL` | Sets the logging verbosity (`debug`, `info`, `warn`, `error`). |
| `AI_PROVIDER` | The AI service to use (`openai`, `claude`, `gemini`, `claude-local`, `openai-compatible`). |
| `OPENAI_API_KEY` | API key for the OpenAI provider. |
| `ANTHROPIC_API_KEY` | API key for the Claude (Anthropic) provider. |
| `GEMINI_API_KEY` | API key for the Gemini (Google) provider. |
| `AWS_PROFILE` | The AWS profile to use for cloud interactions. |

## Testing Environment Variables

These variables are used by integration tests. Integration tests are gated by the `//go:build integration` build tag, not by environment variables. See the [Integration Testing](integration-testing.md) guide for full details.

| Variable | Description |
| :--- | :--- |
| `GITHUB_TOKEN` | GitHub personal access token (`repo` scope) for VCS integration tests. |
| `GITHUB_KEY` | Path to an SSH private key for git-over-SSH tests (optional). |
