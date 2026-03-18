---
title: AI Collaboration
description: How to effectively work with AI assistants using project-specific skills, workflows, and spec-driven development.
tags: [ai, collaboration, library, gtb, workflows, specifications]
---

# AI Collaboration

GTB is a foundational library. Maintaining its stability and quality is critical. We use project-specific AI Skills, Antigravity Workflows, and a spec-driven development process to guide all contributions.

## AI Skills

The **[gtb-dev](file://.agent/skills/gtb-dev/SKILL.md)** skill defines the standard for library and CLI contributions. It mandates:

- **Library-First Design**: New features must be implemented in `pkg/` as reusable components first.
- **Generator Maintenance**: Ensuring that internal generator templates (`internal/generator/`) stay up to date.
- **Strict Quality**: 90% test coverage and no `//nolint` decorators.

## Antigravity Workflows

Trigger these workflows to automate the heavy lifting:

- **`/gtb-spec`**: Drafts a new feature specification and saves it to `docs/development/specs/`. Always start here for non-trivial features.
- **`/gtb-verify`**: The standard verification suite. Includes suite tests, race detection, and strict linting.
- **`/gtb-library-contribution`**: A guide for adding new reusable logic to the core library.
- **`/gtb-command-generation`**: Streamlines the process of defining and generating new CLI commands.
- **`/gtb-docs`**: Documentation-only updates — cross-references docs against source and verifies accuracy.
- **`/gtb-release`**: Release preparation — validates commits, goreleaser config, and snapshot builds.

## Spec-Driven Development

Feature specifications are the primary mechanism for communicating requirements to both human developers and AI agents. All non-trivial features begin with a spec that is drafted collaboratively with an AI assistant and stored in the repository.

### Why Specs Matter for AI Agents

- **Persistent context**: Specs live in `docs/development/specs/` and are always available for an agent to read, eliminating the need to re-explain requirements across sessions.
- **Feature history**: The dated spec files (`YYYY-MM-DD-<feature-name>.md`) provide a chronological record of proposals, approvals, and design decisions that an agent can consult for precedent.
- **Structured requirements**: Specs follow a consistent format (public API, data models, testing strategy, implementation phases) that maps directly to implementation tasks.

### Working with Specs as an AI Agent

When asked to implement a feature:

1. **Read the spec first** -- check `docs/development/specs/` for an approved spec matching the feature.
2. **Read the required documentation** -- before writing any code, read the documents listed in the [Required Reading for Implementation](./specs/index.md#required-reading-for-implementation) section. This builds the context needed to follow project conventions.
3. **Use Test-Driven Development** -- for each implementation phase, write failing tests first (derived from the spec's public API, data model, and error cases), then implement the minimum code to make them pass. See [Implementing a Specification](./specs/index.md#implementing-a-specification) for the full TDD workflow.
4. **Follow the implementation phases** -- specs define a phased approach; implement in order.
5. **Cross-reference the public API** -- interfaces, types, and constructors are defined in the spec.
6. **Check the status** -- only implement specs with status `APPROVED` or `IN PROGRESS`.
7. **Update the status** -- mark the spec as `IN PROGRESS` when starting and `IMPLEMENTED` when complete.

### Drafting a New Spec

When asked to design a new feature, follow the **[Feature Specifications](./specs/index.md)** guide. It includes a prompt template, the list of context files to reference, and the required document format.

### Implementing an Approved Spec

When asked to implement a spec, use the **[Implementation Prompt](./specs/index.md#implementation-prompt)** template from the Feature Specifications guide. It provides step-by-step TDD instructions and references the documentation the agent must read before writing code.

## Testing the Generator

When contributing to the library or the command generator itself, always verify the output:

1.  Use `go run ./ generate <command> -p tmp` to generate a test project in a temporary folder.
2.  Verify that the generated code compiles and functions as intended.
3.  Delete the `tmp/` folder once verification is complete.

## Documentation Maintenance

Accuracy is paramount. The AI is instructed to cross-reference every documentation update with the implementation code to ensure they are perfectly aligned. Documentation should always be **knowledgeable, informative, and friendly**.

## Best Practices for AI-Driven Development

1.  **Spec First**: For non-trivial features, always check for or draft a [Feature Specification](./specs/index.md) before writing code.
2.  **Tests Before Code**: Use [TDD](./specs/index.md#implementing-a-specification) when implementing specs -- write failing tests derived from the spec, then implement.
3.  **Reference the Docs**: Always consult the relevant `docs/` before suggesting changes.
4.  **Verify Often**: Use `/gtb-verify` early and often to catch style or logic issues.
5.  **Documentation Maintenance**: Any functional change MUST include a doc update. Cross-reference code for accuracy.
6.  **No `//nolint`**: Avoid suppressing linter errors. Address the underlying cause instead.

!!! tip
    If you find the AI drifting from project standards, remind it to re-read its **gtb-dev** skill and the relevant feature spec.

!!! important
    Always run `/gtb-verify` before submitting a PR. It is the definitive quality gate for this library.
