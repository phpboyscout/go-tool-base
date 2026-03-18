---
description: Documentation-only update workflow for GTB
---
1. **Identify scope**:
   - Determine which component or concept is being documented.
   - Read the existing doc file(s) in `docs/components/` or `docs/concepts/` before making any changes.
2. **Cross-reference with code**:
   - Read the relevant source files in `pkg/` to verify that all function signatures, interface definitions, types, and behaviours described in the documentation are accurate.
   - Check any code examples compile and match the current public API.
3. **Update documentation**:
   - Apply corrections and additions. Use a **knowledgeable, informative, and friendly** tone.
   - Ensure internal links between docs are valid.
   - If a concept or component doc references another doc, verify that cross-reference is still accurate.
4. **Verify docs build**:
   - If MkDocs tooling is available locally, run `mkdocs serve` or the equivalent to confirm the site builds without errors.
5. **PR readiness**:
   - Documentation-only PRs do not require `/gtb-verify` unless Go source was also touched.
   - Confirm no code changes were accidentally included (`git diff`).
