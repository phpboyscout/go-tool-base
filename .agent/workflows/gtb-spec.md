---
description: Workflow for drafting a new GTB feature specification
---
1. **Check for an existing spec**:
   - Search `docs/development/specs/` for a spec matching the feature.
   - If one exists, read it and report its current status. Do not draft a duplicate.
2. **Gather context**:
   - Read the **Feature Specifications guide** (`docs/development/specs/index.md`) for the required spec format and prompt template.
   - Read the **Contributor Guide** (`docs/development/index.md`) and any relevant `docs/concepts/` and `docs/components/` files for the area being specified.
   - Identify what existing code, interfaces, or packages the feature will extend or replace.
3. **Draft the spec**:
   - Use today's date and a descriptive slug: `docs/development/specs/YYYY-MM-DD-<feature-name>.md`.
   - Follow the spec frontmatter format exactly (title, description, date, status: DRAFT, tags, author).
   - Include all required sections: Problem Statement, Goals & Non-Goals, Public API, Data Models, Error Cases, Testing Strategy, Implementation Phases, and Open Questions.
   - Cross-reference any existing types, interfaces, or patterns from the codebase that the spec builds on.
4. **Save and pause for review**:
   - Save the spec file with `status: DRAFT`.
   - Do not begin implementation. Inform the user that the spec is ready for review and must be marked `APPROVED` before work starts.
