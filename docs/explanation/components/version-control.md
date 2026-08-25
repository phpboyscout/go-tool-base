---
title: Version Control
description: Redirect: VCS documentation has moved to the vcs/ subsection.
date: 2026-03-25
tags: [components, vcs]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Version Control

The VCS documentation has been reorganised into per-package pages.

- **[VCS overview](vcs/index.md)**: what stays in GTB (the config glue) and how the extracted forge/repo modules are wired
- **[Release Provider](https://forge.go.phpboyscout.uk/reference/providers/)**: backend-agnostic `Provider`, `Release`, `ReleaseAsset` interfaces
- **[Repo](vcs/repo.md)**: git repository operations (local and in-memory)
- **[GitHub](https://gitlab.com/phpboyscout/go/forge-github)**: GitHub API client and release provider (external `go/forge-github` module)
- **[GitLab](https://forge.go.phpboyscout.uk/reference/providers/#gitlab)**: GitLab release provider
