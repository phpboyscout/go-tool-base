---
title: Feature Specifications
description: Guide for creating feature specifications using AI-assisted drafting, including the process, prompt template, and required context.
tags: [development, specifications, ai, workflow, planning]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Feature Specifications

Feature specifications are the first step in developing any non-trivial feature for GTB. They capture requirements, design decisions, data models, public API surfaces, and implementation plans in a single, reviewable document before any code is written.

Because GTB is a foundational library consumed by many downstream projects, specifications are especially important -- they ensure that public API changes, new packages, and generator modifications are carefully designed before implementation begins.

Specifications are drafted collaboratively with an AI assistant (e.g. Cursor Agent, Claude) to ensure thorough coverage of edge cases, backwards compatibility, and alignment with existing project patterns.

## Specifications Directory

All specs live in `docs/development/specs/` and follow the naming convention:

```
YYYY-MM-DD-<feature-name>.md
```

The ISO 8601 date prefix ensures specs are listed in creation order. Examples:

- `2026-02-20-config-encryption.md`
- `2026-04-10-provider-plugin-system.md`
- `2026-06-01-tui-redesign.md`

## Process

### 1. Gather Context

Before starting, identify:

- **What problem does this feature solve?** Be specific about the current pain point.
- **What existing code or tooling is being replaced or extended?** Gather references from other repos if migrating functionality.
- **What are the constraints?** Backwards compatibility, supported platforms, security requirements.
- **Who are the consumers?** Since GTB is a library, consider the impact on downstream projects.

### 2. Start an AI Conversation

Open a new Cursor Agent conversation and provide:

- The prompt (see template below).
- All relevant context files attached using `@` references.
- Any external files from other repositories that inform the design.

### 3. Iterate on the Draft

The AI will produce a first draft. Review it and refine through follow-up messages:

- Answer any open questions the AI raises.
- Correct assumptions that don't match your understanding.
- Add or remove packages, interfaces, or options as needed.
- Challenge the AI on edge cases and error handling.
- Verify that public API changes are backwards compatible or properly versioned.

### 4. Finalise and Commit

Once the spec is complete:

1. Add YAML frontmatter (`title`, `description`, `date`, `status`, `tags`, `author`) -- see [Frontmatter Template](#frontmatter-template).
2. Add the [document header](#document-header) with Authors, Date (long form), and Status below the title.
3. Set the `status` to `DRAFT`.
4. Save to `docs/development/specs/YYYY-MM-DD-<feature-name>.md`.
5. Commit and open a PR for team review before implementation begins.
6. Update `status` to `APPROVED` once the team has reviewed and accepted the spec.

## Suggested Prompt

Use the following as a starting point, adapting the description to your feature:

```markdown
I want you to draft a complete feature specification for the GTB project.

## Feature Summary

<Describe the feature in 2-5 sentences. What does it do? What problem does it solve?
What existing tooling or process is it replacing or extending?>

## Requirements

<List the key requirements, e.g.:>
- Must support X and Y
- Must be backwards compatible with existing public API
- Should follow existing GTB patterns for dependency injection and interface design
- Should include generated mocks for all new interfaces

## Context

I have attached the following files for reference:
- <list the @-referenced files you are attaching>

## Instructions

- Pay close attention to existing library patterns, Go types, and project conventions.
- Cross-reference any external codebases I have attached.
- Include: public API design (interfaces, types, constructors), internal implementation,
  project structure, testing strategy, and implementation phases.
- Document all design decisions and their rationale.
- Consider impact on downstream consumers and the generator.
- Suggest additional functionality that would make the feature more robust.
- Generate the specification as a markdown file with YAML frontmatter
  (title, description, date, status, tags, author) and a document header
  (Authors, Date in long form, Status) using Material for MkDocs definition
  list syntax. Refer to @docs/development/specs/index.md for the required
  format.
- Set the status to DRAFT.
- Save the specification to docs/development/specs/ using the naming convention
  YYYY-MM-DD-<feature-name>.md.
```

## Context Files to Attach

When starting a spec conversation, attach the relevant subset of these files to give the AI sufficient context about the project:

### Always Include

These files establish project conventions and should be attached for every spec:

| File / Folder | Purpose |
|---------------|---------|
| `@docs/development/index.md` | Development guide -- project structure, workflow, conventions. |
| `@docs/development/error-handling.md` | Error handling standards. |
| `@docs/development/specs/` | Existing specs as examples of the expected format and depth. |
| `@go.mod` | Module path and dependency versions. |

### Include When Relevant

Attach these based on the nature of the feature:

| File / Folder | When to Include |
|---------------|-----------------|
| `@docs/concepts/architecture.md` | Feature touches core architecture or cross-cutting concerns. |
| `@docs/development/ai-collaboration.md` | Feature involves AI integration or you want the agent to understand the AI workflow. |
| `@docs/development/ai-integration.md` | Feature modifies or extends the AI provider layer. |
| `@docs/development/security.md` | Feature handles secrets, credentials, or auth. |
| `@docs/development/dependency-management.md` | Feature introduces new external dependencies. |
| `@pkg/<package>/` | Feature extends an existing library package. |
| `@internal/generator/` | Feature modifies generator templates or scaffolding. |
| `@cmd/<command>/` | Feature adds or modifies a built-in command. |

### Include When Migrating from External Repos

If the feature migrates functionality from another codebase:

| Context | What to Attach |
|---------|----------------|
| Source files being migrated | Attach the specific files from the external repo. |
| Schemas or templates | Attach any JSON schemas, YAML templates, or config examples from the source. |
| Working examples | Attach real-world examples that the feature must handle. |

!!! tip
    Err on the side of providing too much context rather than too little. The AI can
    ignore irrelevant files, but it cannot infer information that was never provided.

## Implementing a Specification

Once a spec reaches `APPROVED` status, implementation follows a **Test-Driven Development (TDD)** approach. Writing tests first -- derived directly from the spec's public API, data model, error cases, and acceptance criteria -- ensures that every requirement is verifiable before production code exists.

### Why TDD for Spec Implementation

- **Specs define testable contracts**: The interfaces, types, constructors, and error cases in a spec translate directly into test cases. Writing these tests first validates the spec's completeness.
- **Confidence during phased implementation**: Specs break work into phases. A failing test suite for Phase 1 gives you a clear checklist; when all tests pass, the phase is done.
- **Safe refactoring**: With tests in place from the start, restructuring code to improve design never risks silently breaking a requirement.
- **Downstream safety**: As a library consumed by other projects, GTB demands that public API contracts are verified by tests before they are shipped. TDD guarantees this.
- **AI agent alignment**: When an AI agent writes tests first, it demonstrates its understanding of the spec before writing implementation code. Misunderstandings surface early, in test assertions, where they are cheap to correct.

### TDD Workflow

For each implementation phase defined in the spec:

1. **Write tests first** -- translate the phase's requirements into test cases (unit tests, integration tests, fixture-based tests). Tests should initially fail.
2. **Implement the minimum code** to make the tests pass. Follow the spec's interface definitions and type contracts exactly.
3. **Refactor** -- improve internal structure while keeping all tests green. Run `golangci-lint run --fix` to maintain code quality.
4. **Verify** -- run the full test suite (`go test -race ./...`) and linter before moving to the next phase.

Repeat for each phase until the spec is fully implemented.

### Implementation Prompt

Use the following prompt to ask an AI agent to implement an approved specification. Attach the spec file and the documentation listed in [Required Reading for Implementation](#required-reading-for-implementation).

```markdown
I want you to implement an approved feature specification for the GTB
project using a Test-Driven Development (TDD) approach.

## Specification

The approved spec is attached at:
- @docs/development/specs/<YYYY-MM-DD-feature-name>.md

## Instructions

### Before Writing Any Code

Read and understand the following project documentation (all attached):
- The feature spec itself -- pay close attention to public API (interfaces, types,
  constructors), internal implementation details, error handling, and implementation phases.
- @docs/development/index.md -- project structure, workflow, and conventions.
- @docs/development/error-handling.md -- error creation, wrapping, and reporting standards.
- @docs/development/verification-checklists.md -- the verification steps you must satisfy.
- @go.mod -- module path and current dependency versions.

### Implementation Approach

1. **Update the spec status** to `IN PROGRESS` (both frontmatter and document header).
2. **Follow the implementation phases** defined in the spec, in order.
3. **For each phase, use TDD**:
   a. Write failing tests first -- derive test cases from the spec's requirements,
      public API contracts, error handling, and edge cases for that phase.
   b. Implement the minimum code to make the tests pass.
   c. Refactor for clarity and consistency while keeping tests green.
   d. Run `golangci-lint run --fix` and `go test -race ./...` before moving on.
4. **Define interfaces at the point of use** following GTB conventions.
5. **Generate mocks** with `mockery` if you introduce or modify interfaces.
6. **Maintain >90% test coverage** for new code in `pkg/`.
7. **Cross-reference the spec** continuously -- do not deviate from the defined
   interfaces, type names, or package structure without noting why.

### Quality Gates

Before marking the spec as `IMPLEMENTED`:
- All tests pass, including race detection: `go test -race ./...`
- Linter passes: `golangci-lint run --fix`
- Mocks are up to date: `mockery`
- Coverage for new `pkg/` code is at least 90%.
- The verification checklist in @docs/development/verification-checklists.md is satisfied.
```

### Required Reading for Implementation

Before beginning implementation, the developer (or AI agent) should read these documents to build sufficient context. Attach them as `@` references when using the [Implementation Prompt](#implementation-prompt).

#### Always Read

These are mandatory for every spec implementation:

| Document | Why |
|----------|-----|
| The feature spec | The primary source of truth for what to build. |
| `@docs/development/index.md` | Project structure, workflow, feature lifecycle, architecture principles. |
| `@docs/development/error-handling.md` | Error creation with `cockroachdb/errors`, wrapping, hints, `ErrorHandler` usage. |
| `@docs/development/verification-checklists.md` | Pre-submission checks the implementation must satisfy. |
| `@go.mod` | Module path and dependency versions -- prevents version mismatches. |

#### Read When Relevant

Attach these based on what the spec touches:

| Document | When to Read |
|----------|--------------|
| `@docs/concepts/architecture.md` | Spec touches core architecture or cross-cutting concerns. |
| `@docs/development/ai-collaboration.md` | Implementation uses AI features or you want the agent to understand its own workflow. |
| `@docs/development/ai-integration.md` | Spec modifies or extends the AI provider layer (`pkg/chat/`). |
| `@docs/development/security.md` | Spec handles secrets, credentials, or authentication. |
| `@docs/development/dependency-management.md` | Implementation introduces new external dependencies. |
| `@pkg/<package>/` | Spec extends an existing library package -- read it as a pattern reference. |
| `@internal/generator/` | Spec modifies generator templates or scaffolding. |
| `@cmd/<command>/` | Spec adds or modifies a built-in command. |

!!! tip
    Attaching a similar, already-implemented package (e.g. `@pkg/config/` or `@pkg/chat/`) gives the agent a concrete pattern to follow for interface design, constructor signatures, and test structure.

## Spec Structure

A well-formed specification should cover these sections (adapt as needed):

1. **Overview** -- motivation, terminology, design decisions.
2. **Public API** -- interfaces, types, constructors, and their contracts.
3. **Internal Implementation** -- package-private logic, helpers, algorithms.
4. **Project Structure** -- new files, directory layout, package organisation.
5. **Generator Impact** -- changes to templates, manifest schema, or scaffolded output (if applicable).
6. **Error Handling** -- error types, wrapping strategy, user-facing messages.
7. **Testing Strategy** -- unit tests, integration tests, E2E BDD scenarios (evaluate Godog fit per the [suitability assessment](2026-03-28-godog-bdd-strategy.md#suitability-assessment)), fixtures, edge cases.
8. **Migration & Compatibility** -- backwards compatibility, deprecation path, versioning impact.
9. **Future Considerations** -- out-of-scope items noted for later.
10. **Implementation Phases** -- prioritised breakdown of work.

## Status Lifecycle

Every spec carries a `status` field in both the frontmatter and the document header. Use one of the following values:

| Status | Icon | Meaning |
|--------|------|---------|
| `DRAFT` | :material-pencil: | Initial authoring in progress. Not yet reviewed by the team. |
| `IN REVIEW` | :material-eye: | Submitted for team review (typically via PR). |
| `APPROVED` | :material-check-decagram: | Reviewed and accepted. Ready for implementation. |
| `IN PROGRESS` | :material-progress-wrench: | Implementation is actively underway. |
| `IMPLEMENTED` | :material-check-circle: | Feature has been fully implemented and merged. |
| `SUPERSEDED` | :material-swap-horizontal: | Replaced by a newer spec. Link to the replacement in the document. |
| `REJECTED` | :material-close-circle: | Reviewed and declined. Include rationale in the document. |
| `DEPRECATED` | :material-archive: | Functionality has been deprecated with no alternative. |

Specs should always begin as `DRAFT` and progress through the lifecycle as work advances. Update the status in **both** the frontmatter and the document header when it changes.

### Page Status Icons

Each status has a corresponding icon that appears in the navigation sidebar when the `status` frontmatter field is set. Both the uppercase document header value and the kebab-case form are accepted:

```yaml
---
status: IMPLEMENTED
---
```

```yaml
---
status: implemented
---
```

| Uppercase (document header) | Kebab-case | Display label | Icon |
|-----------------------------|------------|---------------|------|
| `DRAFT` | `draft` | Draft | :material-pencil: |
| `IN REVIEW` | `in-review` | In Review | :material-eye: |
| `APPROVED` | `approved` | Approved | :material-check-decagram: |
| `IN PROGRESS` | `in-progress` | In Progress | :material-progress-wrench: |
| `IMPLEMENTED` | `implemented` | Implemented | :material-check-circle: |
| `SUPERSEDED` | `superseded` | Superseded | :material-swap-horizontal: |
| `REJECTED` | `rejected` | Rejected | :material-close-circle: |
| `DEPRECATED` | `deprecated` | Deprecated | :material-archive: |

Icons and tooltips are configured in `zensical.toml` (or `mkdocs.yml` for legacy setups) under `extra.status` with styles in `docs/stylesheets/extra.css`.

### Handling Rejections

When a spec is rejected, keep the document in the repository -- it serves as a historical record and prevents the same proposal from being re-raised without new context. Add a `## Rejection Rationale` section at the top of the document (immediately after the definition list header) explaining **why** the spec was declined.

Example:

```markdown
Status
:   REJECTED

## Rejection Rationale

This spec proposed adding a plugin system for AI providers via shared libraries.
After review, the team concluded:

1. **Complexity vs value**: The current compile-time provider registration is simpler
   and sufficient for the known set of providers.
2. **Security concerns**: Loading shared libraries introduces attack surface that
   conflicts with our security posture.
3. **Go ecosystem norms**: Plugin-based extension is not idiomatic in Go libraries;
   interface-based composition is preferred.

The structured output improvements proposed in this spec remain valid and are
covered by a separate spec.
```

## Frontmatter Template

Every spec must include YAML frontmatter:

```yaml
---
title: "<Feature Name> Specification"
description: "<One-sentence summary of what the spec covers.>"
date: YYYY-MM-DD
status: DRAFT
tags:
  - specification
  - <feature-tag>
  - <additional-tags>
author:
  - name: <Your Name>
    email: <your.email@phpboyscout.com>
  - name: <AI Model Name>
    role: AI drafting assistant
---
```

## Document Header

Immediately after the `# Title` heading, add a definition list with the document metadata. Use a long-form date (e.g. "18 February 2026") for readability on the rendered site, while the frontmatter retains the ISO 8601 format for machine parsing.

```markdown
# <Feature Name> Specification

Authors
:   <Your Name>, <AI Model Name> *(AI drafting assistant)*

Date
:   18 February 2026

Status
:   DRAFT
```
