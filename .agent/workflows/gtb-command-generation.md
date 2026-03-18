---
description: Generator-driven workflow for CLI commands in GTB
---
0. **Spec Check**:
   - Check `docs/development/specs/` for an existing spec matching the command.
   - Only proceed if the spec status is `APPROVED` or `IN PROGRESS`.
   - For non-trivial commands with no spec, run `/gtb-spec` to draft one and pause for review.
   - Update the spec status to `IN PROGRESS` before writing any code.
1. **Define**:
   - Update `.gtb/manifest.yaml` with the new command or flag definition.
2. **Generate**:
   // turbo
   - Run the project regenerator:
     ```bash
     just build
     ```
3. **Implement**:
   - Locate the generated files in `cmd/` or `pkg/cmd/`.
   - Implement the business logic, preferably delegating to a `pkg/` component.
4. **Template Review**:
   - If the generated code requires repetitive manual fixes, update the templates in `internal/generator/` instead.
5. **Verify Generation Output**:
   - Test the generator by running it against a temporary folder:
     ```bash
     go run ./ generate <command> -p tmp
     ```
   - Verify that the output in `tmp/` is as expected.
   - Clean up the `tmp/` directory once verified.
6. **Verify**:
   - Run the `gtb-verify` workflow.
7. **Document**:
   - Ensure the new command's purpose and usage are reflected in `docs/`.
