---
name: diataxis-docs
description: Guidelines for generating and structuring documentation using the Diátaxis framework, accommodating hybrid website/docsite patterns, and deferring API references.
---

# Documentation Generation Guidelines (Diátaxis)

When generating, structuring, or auditing documentation for any project, you must strictly adhere to the **Diátaxis** framework, with specific adaptations for our hybrid website/docsite approach.

## The Diátaxis Framework

Diátaxis organizes documentation into four distinct quadrants based on user needs. Every document must clearly belong to *one* of these quadrants—do not mix their purposes.

1. **Tutorials** (Learning-oriented): 
   - **Goal:** Allow the newcomer to learn by doing.
   - **Style:** Step-by-step, instructional, low-context, guaranteed to work.
   - **Focus:** The user's journey, not the product's features.

2. **How-to Guides** (Task-oriented):
   - **Goal:** Show the user how to solve a specific problem.
   - **Style:** Goal-oriented, practical, sequential steps.
   - **Focus:** Achieving a specific outcome for a user who already understands the basics.

3. **Explanation / Concepts** (Understanding-oriented):
   - **Goal:** Explain the "why" and "how it works" at a higher level.
   - **Style:** Discursive, narrative, architectural.
   - **Focus:** Broadening understanding, design philosophies, alternatives, and internal mechanics.
   - *Note:* This includes architectural components and broader ecosystem concepts.

4. **Reference** (Information-oriented):
   - **Goal:** Provide accurate, comprehensive facts.
   - **Style:** Austere, structured, to-the-point.
   - **Focus:** API endpoints, CLI flags, configuration schemas, environment variables.

---

## The Hybrid Website/Docsite Compromise

In our projects, documentation often serves a dual purpose: it powers the technical docsite *and* the public-facing marketing/product website. To accommodate this, we make the following deliberate compromises to pure Diátaxis:

- **About / Landing Pages in Explanation:** High-level product pitches, "Why use X?", and landing page content may reside alongside technical explanations or in an `about/` section. While pure Diátaxis argues marketing doesn't belong in documentation, our hybrid pattern requires these docs to serve both audiences.
- **Component-driven Concepts:** Instead of isolating all conceptual material in a generic `concepts/` directory, architectural and conceptual overviews should be consolidated directly into the relevant `components/` documentation. This prevents fragmentation and duplication.
- **TUI & Integrated Docs:** Documentation might be embedded directly into CLI tools (e.g., via a Terminal UI or AI-assistant). Keep markdown clean, use standard GitHub Flavored Markdown, and avoid complex HTML/JS macros that won't render in a terminal or static site generator.

---

## Deferring API Reference (Go, Rust, etc.)

Documentation should focus on narrative, usage, and architecture. **Do not duplicate dry, auto-generated package definitions (like massive interfaces or structs).** 

- **Language-Agnostic Principle:** Defer language-level API references to the language's native ecosystem package registry.
- **Go (`pkg.go.dev`):** Instead of dumping `type MyInterface interface { ... }` into markdown, provide a brief summary of its purpose and link to the Go package documentation.
  - *Example:* `> [!NOTE]` \n `> See [pkg.go.dev/...](https://pkg.go.dev/...) for the full API definition.`
- **Rust (`docs.rs`):** Similarly, defer Rust trait, struct, and macro definitions to `docs.rs`.
- **Reference Section Focus:** The local `reference/` directory should be strictly reserved for non-code surfaces: CLI commands, flags, configuration file schemas (YAML/JSON), and REST API definitions.

## Workflow Execution

When asked to write or audit documentation:
1. **Identify the Quadrant:** Ask yourself: "Is this teaching, guiding, explaining, or referencing?"
2. **Strip Duplication:** If a Reference doc starts explaining architecture, move that architecture to Explanation. If an Explanation doc lists every CLI flag, move the flags to Reference.
3. **Link, Don't Copy:** Strip large API code blocks and insert external links to `pkg.go.dev` or `docs.rs`.
