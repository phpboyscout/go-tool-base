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
4. Regenerate mocks if any interfaces were modified:
   ```bash
   just mocks
   ```
5. Verify that no `//nolint` decorators were added unnecessarily.
6. Ensure test coverage for new library features in `pkg/` is at least 90%.
