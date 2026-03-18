---
name: gtb-dev
description: Defines the development lifecycle for the gtb project and cli. Use whenever you are developing or maintaining the gtb project
---
# GTB Development Assistant Skill

This skill provides guidelines and workflows for AI assistants working on the GTB library. It focuses on library-first principles, spec-driven development, generator-driven workflows, and strict quality controls.

## Core Documentation References

Before performing research or suggesting changes, always consult the following:

- **[Contributor Guide](file://docs/development/index.md)**: Library-first principles, project structure, and quality protocols.
- **[AI Collaboration Guide](file://docs/development/ai-collaboration.md)**: How to work with specs, workflows, and the AI toolchain.
- **[Feature Specifications](file://docs/development/specs/index.md)**: Spec format, lifecycle, TDD workflow, and prompts.
- **[Verification Checklists](file://docs/development/verification-checklists.md)**: Pre-submission quality gates.
- **Concepts**: Core framework design patterns in `docs/concepts/`.
- **Components**: API documentation for library packages in `docs/components/`.

---

## Step 0: Spec Check (Before Any Implementation)

This is the most important step. **Do not write implementation code until it is complete.**

1. Check `docs/development/specs/` for an existing spec matching the feature or change.
2. **If a spec exists**: verify its `status` is `APPROVED` or `IN PROGRESS` before proceeding. Do not implement a `DRAFT` or `IN REVIEW` spec.
3. **If no spec exists**:
   - For **non-trivial features** (new packages, public API changes, generator modifications, architectural changes): draft a spec first using the **[Suggested Prompt](file://docs/development/specs/index.md#suggested-prompt)** and save it to `docs/development/specs/YYYY-MM-DD-<feature-name>.md` with status `DRAFT`. Pause and wait for human review and `APPROVED` status before implementing.
   - For **quick fixes and minor changes** (bug fixes, small enhancements, refactors that don't alter the public API): proceed directly to implementation without a spec.
4. **Update the spec status** to `IN PROGRESS` when you begin implementation, and to `IMPLEMENTED` when complete (update both the frontmatter and the document header).

---

## Workflows

Use the appropriate workflow for the task at hand. These are the primary execution mechanisms — prefer them over ad-hoc steps.

| Task | Workflow |
|------|----------|
| Drafting a new feature specification | `/gtb-spec` |
| Adding or modifying a reusable library component in `pkg/` | `/gtb-library-contribution` |
| Defining or generating a new CLI command | `/gtb-command-generation` |
| Verifying correctness before committing or raising a PR | `/gtb-verify` |
| Updating documentation without touching code | `/gtb-docs` |
| Preparing or validating a release | `/gtb-release` |

---

## Development Lifecycle

Every non-trivial task MUST follow this lifecycle in order:

### 1. Library-First Design

Any new feature must be implemented in `pkg/` as a reusable component before being exposed via the CLI. Maintain strict backward compatibility and clean public APIs. Define interfaces at the point of use.

### 2. Internal Generator Maintenance

When modifying CLI commands or library APIs that affect scaffolded output, ensure that internal templates in `internal/generator/` are updated to reflect the best-practice implementation. The generator is the source of truth for downstream project consistency.

### 3. Implementation (TDD)

Follow a **Test-Driven Development** approach for all spec-driven work:

1. **Write failing tests first** — derive test cases from the spec's public API contracts, data model, error cases, and edge cases.
2. **Implement the minimum code** to make the tests pass. Follow the spec's interface definitions and type contracts exactly.
3. **Refactor** — improve internal structure while keeping all tests green.
4. **Verify** — run `go test -race ./...` and `golangci-lint run --fix` before moving to the next phase.

Additional implementation standards:

- Use `github.com/cockroachdb/errors` for all error creation and wrapping. `github.com/go-errors/errors` has been fully removed from the codebase.
- Follow the `props`-based dependency injection pattern for all library components.
- Use a `.env` file for local secrets and configuration. See the **[Environment Variables & .env](file://docs/development/environment-variables.md)** guide.

### 4. Verification

Run the `/gtb-verify` workflow. At minimum this covers:

- `just test` — complete library test suite.
- `go test -race ./...` — concurrency safety (mandatory).
- `golangci-lint run --fix` — strict linting with no outstanding issues.
- `mockery` — regenerate mocks if any interfaces were added or modified.
- Test coverage for new `pkg/` features must be at least **90%**.
- No `//nolint` decorators added except in the most exceptional, documented cases.

If the change affects generator output, also run:

```bash
just build
go run ./cmd/gtb generate <command> -p tmp
```

Verify the output in `tmp/` is correct, then delete the `tmp/` directory.

### 5. Documentation Maintenance

Any alteration to functionality **MUST** trigger a review of associated documentation in the same turn.

- **Accuracy & Cross-Reference**: All documentation MUST be cross-referenced with the code for absolute accuracy. Examples, function signatures, and described behaviours must match the implementation exactly.
- **Tone of Voice**: Use a **knowledgeable, informative, and friendly** tone. Helpful and professional — avoid overly dense or dry language.
- **Update**: If behaviour or API changes, update the corresponding markdown files in `docs/concepts/` and `docs/components/`. These remain the source of truth for library usage.

### 6. Branch & PR Readiness

- Work on a feature branch created from `main` (e.g. `feature/my-feature`).
- For spec-driven features: the spec and the implementation live on the **same branch and PR**. This co-locates the design rationale with the code in git history.
- For spec-only PRs (before implementation begins), prefix the PR title with `[SPEC]`.
- Before raising a PR, ensure all items in the **[Verification Checklists](file://docs/development/verification-checklists.md)** are satisfied and CI checks pass.

---

## Code Quality

Before raising a PR, run the `/simplify` skill on changed files to check for unnecessary complexity, redundant abstractions, or reuse opportunities within the codebase.

---

## Debugging

When tests fail, linting fails, or generator output is wrong, follow these steps before retrying:

1. **Test failures**: Read the full failure output — identify whether it is a logic error, a missing mock, or a stale interface. Run `go test -v -run TestName ./path/to/pkg` to isolate.
2. **Linter failures**: Run `golangci-lint run --fix` — most issues are auto-fixable. For remaining issues, address the root cause rather than adding `//nolint`.
3. **Mock issues**: If a mock is stale or missing, run `mockery` to regenerate from the current interface definition.
4. **Generator failures**: Run `just build` to recompile the generator before testing output. Check `internal/generator/` templates if scaffolded code is incorrect.
5. **Race conditions**: Run `go test -race -count=5 ./path/to/pkg` to reproduce intermittent race failures reliably.

Do not retry the same failing command repeatedly — diagnose the root cause first.

---

## Security

- **Ignored Secrets**: Verify that `.env*` files are ignored in `.gitignore`.
- **Sensitive Data**: Ensure no secrets are hardcoded in library assets or default configurations.
- Consult **[Security](file://docs/development/security.md)** when the change handles credentials, tokens, or authentication.
