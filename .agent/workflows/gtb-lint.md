---
description: Workflow for resolving golangci-lint issues in GTB
---
Run `golangci-lint run` first and collect the full issue list before making any changes. Address issues **in the order below — simplest to most complex** — so that straightforward fixes do not get entangled with structural refactors.

## Issue Resolution Order

### 1. `errcheck` — unchecked error returns
Assign ignored return values explicitly: `_ = f.Close()`. If the error is genuinely actionable, handle it properly instead.

### 2. `gocritic` — code style issues
Fix the flagged pattern directly (e.g. `appendAssign`, `dupBranchBody`). These are usually one-line changes. Removing duplicate branches may leave unused imports — clean those up immediately.

### 3. `staticcheck` — deprecated APIs and misuse
Replace deprecated symbols or packages with their recommended successors. For dependency migrations (e.g. a renamed module), update the import path, run `go mod tidy`, verify the package name, and fix any API differences before moving on.

### 4. `exhaustive` — missing switch cases
Add an explicit `case` for every missing enum value. If a case legitimately has no action, add it with a short comment explaining why (do not leave it empty and silent).

### 5. `nestif` — deeply nested conditional blocks
Extract the nested block into a named helper method. The extracted function should be self-contained and testable. Preserve the original logic exactly — do not simplify behaviour while restructuring.

### 6. `cyclop` — high cyclomatic complexity
Extract sub-logic into named functions or methods. Prefer named functions over closures: closures count toward the outer function's complexity score, named functions do not. Aim to bring each function to a complexity of ≤ 10.

### Any other linter
For issue types not listed above, apply the same principle: **resolve in order of complexity, simplest first**. A useful heuristic is:
- Issues that require only a local, single-line fix (e.g. a missing argument, a wrong type, a redundant call) come before
- Issues that require restructuring a single function, which come before
- Issues that require splitting or reorganising multiple functions or files.

When in doubt, fix the issue that touches the fewest lines first.

---

## Rules

- **Never use `//nolint` comments** to bypass a lint issue. Always address the root cause.
- **Run tests after every fix**, not just at the end. Structural changes (nestif, cyclop) can silently alter behaviour.
- **One linter category per commit** where practical. Mix only when changes are inseparable (e.g. a dependency migration that also fixes errcheck in the same file).
- After all issues are resolved, run `golangci-lint run` once more to confirm a clean output before committing.

## Verification

After resolving all issues, run the full `/gtb-verify` workflow to confirm that lint is clean, all tests pass, and no regressions have been introduced.
