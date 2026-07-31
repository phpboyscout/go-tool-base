---
description: Workflow for drafting a new GTB feature specification
---
1. **Check for an existing spec**:
   - Search the [spec register](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/home) for a spec matching the feature.
   - If one exists, read it and report its current status. Do not draft a duplicate.
2. **Gather context**:
   - Read the **Feature Specifications guide** (https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/home) for the required spec format and prompt template.
   - Read the **Contributor Guide** (`docs/development/index.md`) and any relevant `docs/concepts/` and `docs/components/` files for the area being specified.
   - Identify what existing code, interfaces, or packages the feature will extend or replace.
3. **Draft the spec**:
   - Claim the next number from the [spec register](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/home) and publish to the wiki as `specs/NNNN-<feature-name>`.
   - Follow the spec frontmatter format exactly (title, description, date, status: DRAFT, tags, author).
   - Include all required sections: Problem Statement, Goals & Non-Goals, Public API, Data Models, Error Cases, Testing Strategy, Implementation Phases, and Open Questions.
   - Cross-reference any existing types, interfaces, or patterns from the codebase that the spec builds on.
   - In the **Testing Strategy** section, explicitly evaluate whether the feature warrants E2E BDD scenarios (Godog). TDD and BDD methodologies are a key requirement of any spec and must be put front and centre as part of the implementation process. Features involving CLI commands, multi-step user workflows, or service lifecycle coordination should include Gherkin feature files in `features/`. Reference the suitability assessment in [`0044-godog-bdd-strategy`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0044-godog-bdd-strategy) for guidance on when BDD adds value versus when standard Go tests suffice. 
   - Ensure documentation is a first class citizen and a hard requirement for the spec. All documentation follows the diataxis methodology with some notable exceptions for marketing (about section) and developer support (development section). The spec shoudl outline what sections require adding/updating as part of the implementation and should outline any new How-To guides. Tutorials are an edge case to diataxis, in so much that tutorials are not part of the core docs, and should be defined in the spec as a blog post for the phpboyscout.uk website and that once available will be linked to from the tutorials section. This is a ket deviation from pure diataxis to allow for appropriate traffic management & marketing strategy via the phpboyscout.uk website without creating duplicative/derivative tutorials in multiple places.  
4. **Save and pause for review**:
   - Save the spec file with `status: DRAFT`.
   - Do not begin implementation. Inform the user that the spec is ready for review and must be marked `APPROVED` before work starts.
