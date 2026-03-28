---
description: Library verification suite for GTB
---
// turbo-all
1. Run the complete library test suite:
   ```bash
   just test
   ```
2. Verify concurrency safety with the race detector:
   ```bash
   just test-race
   ```
3. Run the linter and enforce strict quality rules:
   ```bash
   just lint-fix
   ```
   If any issues remain after `--fix`, resolve them following the `/gtb-lint` workflow before continuing.
4. **Confirm tests still pass after any linting or refactoring changes.** Structural fixes (nestif, cyclop) can silently alter behaviour — always re-run the test suite as the final step after lint work, not just before it.
5. Regenerate mocks if any interfaces were modified:
   ```bash
   just mocks
   ```
6. Verify that no `//nolint` decorators were added unnecessarily.
7. Ensure test coverage for new library features in `pkg/` is at least 90%.
8. If integration tests exist for the affected packages, verify they compile correctly:
   ```bash
   go vet ./...
   ```
   Integration tests use `testutil.SkipIfNotIntegration(t, "tag")` and will be skipped in normal test runs. They must still compile cleanly.
9. If the change affects CLI commands, service lifecycle, or user-facing workflows, run the E2E BDD test suite:
   ```bash
   just test-e2e
   ```
   If only CLI scenarios are relevant, use `just test-e2e-smoke` for a faster feedback loop. New CLI commands or flag changes should have corresponding Gherkin feature files in `features/cli/`.
