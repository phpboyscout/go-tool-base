---
description: Guide for contributing to the GTB library
---
0. **Spec Check**:
   - Check `docs/development/specs/` for an existing spec matching the feature.
   - Only proceed with implementation if the spec status is `APPROVED` or `IN PROGRESS`.
   - For non-trivial features with no spec, run `/gtb-spec` to draft one and pause for review before continuing.
   - Update the spec status to `IN PROGRESS` before writing any code.
1. **Library-First Planning**:
   - Identify the reusable core logic and plan its implementation in `pkg/`.
   - Read the **Contributor Guide** (`docs/development/index.md`) and relevant **Concepts** docs.
2. **Implementation (TDD)**:
   // turbo
   - **Write failing tests first** — derive test cases from the spec's public API contracts, data model, error cases, and edge cases.
   - **Implement the minimum code** to make the tests pass. Follow the spec's interface definitions exactly.
   - **Refactor** — improve internal structure while keeping all tests green.
   - Maintain strict backward compatibility for public APIs.
   - Use `github.com/cockroachdb/errors` for all error creation and wrapping.
3. **Integration Tests** (when applicable):
   - If the feature interacts with external services or requires multi-component coordination, add integration tests in a dedicated `*_integration_test.go` file.
   - Gate each test with `testutil.SkipIfNotIntegration(t, "tag")` using an appropriate tag for the package group.
   - Document the test and its dependencies in `docs/development/integration-testing.md`.
4. **Verification**:
   - Run the `/gtb-verify` workflow.
   - If the library change affects generation output, verify by running:
     ```bash
     go run ./ generate <command> -p tmp
     ```
   - Clean up `tmp/` after verification.
   - Ensure all public-facing methods are documented in the code.
5. **Documentation Maintenance**:
   - Update `docs/components/` with the new or modified API details.
   - Update `docs/concepts/` if architectural patterns have evolved.
6. **Generator Check**:
   - If this library change affects how downstream tools are generated, update the templates in `internal/generator/`.
7. **PR Readiness**:
   - Run `/simplify` on changed files before raising a PR.
   - Verify that the change is isolated, well-tested, and fully documented.
   - Update the spec status to `IMPLEMENTED`.
