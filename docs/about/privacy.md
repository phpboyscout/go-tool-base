---
title: Privacy Policy
description: Privacy policy and data handling practices for Go Tool Base.
hide:
  - navigation
---

# Privacy Policy

**Effective Date:** June 25, 2026

Go Tool Base (GTB) is an open-source lifecycle framework for Go CLI tools. As an open-source tool, GTB itself does not collect, store, or transmit your personal data, source code, or credentials to any central servers.

## Telemetry & Diagnostics

If you opt-in to telemetry (e.g., using `gtb telemetry enable`), the CLI will collect anonymised usage metrics to help us improve the tool. You can review the exact data collected and configure your telemetry preferences at any time.

*   **No PII:** We do not collect Personally Identifiable Information (PII).
*   **Opt-In:** Telemetry is strictly opt-in.
*   **Redaction:** All credentials, paths, and environment variables are heavily redacted by our robust internal filtering before any payload is constructed.

## AI Chat Providers

GTB integrates with third-party LLM providers (e.g., OpenAI, Anthropic, Google Gemini) to power its autonomous ReAct loops. 

*   When you use AI features (e.g., `gtb docs --chat`), prompt data and relevant contextual snippets from your workspace are transmitted directly to your configured provider.
*   We do not proxy these requests. They are transmitted directly from your machine to the provider's API.
*   Please refer to your respective LLM provider's privacy policy regarding how they handle your API transmissions.

## Credentials Vault

Any credentials (such as API keys for LLMs or remote Git hosts) are stored securely and locally using your operating system's native keychain (macOS Keychain, Linux Secret Service/KWallet, or Windows Credential Manager). They never leave your device unless explicitly transmitted to the service they authenticate with.
