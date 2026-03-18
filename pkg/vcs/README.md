# VCS

Abstractions for Version Control System operations, supporting multiple backends like GitHub and GitLab.

## Package Structure

- **[github](./github)**: GitHub-specific client and release implementation.
- **[gitlab](./gitlab)**: GitLab release provider implementation.
- **[repo](./repo)**: Generic Git repository management (Local and In-memory).
- **[release](./release)**: Domain interfaces for releases, assets, and providers.

## Pluggable Release Providers

The VCS package provides a `release.Provider` interface that abstracts release operations across different platforms:

- Programmatic API interaction (PRs, releases, etc.)
- Unified authentication layer for SSH and tokens
- Backend-agnostic asset downloads

For detailed documentation, testing strategies, and configuration options, see the **[Version Control Component Documentation](../../docs/components/version-control.md)**.
