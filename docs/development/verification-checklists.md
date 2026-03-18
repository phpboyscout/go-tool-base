---
title: Verification Checklists
description: Standards for verifying changes to the GTB library and generator.
tags: [verification, testing, checklist]
---

# Verification Checklists

Since GTB is used by many other projects, verification must be extremely thorough.

## Pre-Submission Checklist

### 0. Specification Check
- [ ] A [Feature Specification](./specs/index.md) exists for non-trivial features.
- [ ] The spec status is `APPROVED` or `IN PROGRESS`.
- [ ] The spec status has been updated to reflect the current state of the implementation.

### 1. Library Correctness
- [ ] `task test` passes with 100% success.
- [ ] `go test -race ./...` passes without identifying concurrency issues.
- [ ] `mockery` has been run to ensure all mocks are up to date.
- [ ] Coverage for new features in `pkg/` is at least 90%.
- [ ] `golangci-lint run --fix` passes with no warnings.

### 2. Generator Verification
If you modified `internal/generator`:
- [ ] Run `task build` to create a fresh generator binary.
- [ ] Test the generator against a dummy project to ensure it correctly scaffolds commands and manifests.
- [ ] Verify that `regenerate project` correctly handles existing files without data loss.

### 3. Documentation
- [ ] New components or public functions are documented in `docs/`.
- [ ] All code examples in documentation are functional.

## Automated Verification

We rely heavily on CI for final verification, including:
- **Matrix Testing**: Running tests against multiple Go versions.
- **Race Detection**: Running `go test -race ./...` to catch concurrency issues.
- **Linting Suite**: Enforcing project-wide code style.
