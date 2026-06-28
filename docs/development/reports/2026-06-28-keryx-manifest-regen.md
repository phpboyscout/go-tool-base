# Bug report — `generate command` / `regenerate manifest` manifest handling

Filed from a downstream consumer session (**keryx**, `gitlab.com/phpboyscout/keryx`)
against go-tool-base **v0.27.0** (gtb binary `go install`ed from the v0.27.0 tag, Linux
x86_64). Reproduced repeatedly while editing existing command metadata.

---

## Bug 1 (primary) — re-running `generate command` on an existing subcommand drops the parent's untouched siblings from the manifest

### What happens
Re-registering one subcommand of a parent rebuilds the parent's `commands:` list in
`.gtb/manifest.yaml` from **only the command(s) touched in that invocation**, silently
**dropping the parent's other existing subcommands** from the manifest. The generated
parent `cmd.go` still registers all of them (so the build isn't immediately broken), so
the manifest now **drifts** from the code — and the next `gtb regenerate project`
(manifest → code) **rewrites the parent `cmd.go` to register only the manifest's
subcommands, deleting the dropped ones from the source.**

### Reproduction (the real keryx case)
1. Project has a parent command `theme` with five subcommands all registered in the
   manifest: `add`, `edit`, `list`, `show`, `rm`.
2. To change the `--set` flag type on `theme add` and `theme edit`, each was
   re-registered:
   ```
   gtb --ci generate command --name add  --parent theme --short "Register a new theme" --agentless
   gtb --ci generate command --name edit --parent theme --short "Edit an existing theme" --agentless
   ```
3. After this, `.gtb/manifest.yaml`'s `theme` entry listed **only `add` and `edit`** —
   `list`, `show`, `rm` were gone from the manifest.
4. `pkg/cmd/theme/cmd.go` still registered all five (binary worked), but
   `gtb --ci regenerate project --dry-run` then proposed **removing** the
   `list.NewCmdList` / `show.NewCmdShow` / `rm.NewCmdRm` registrations from
   `theme/cmd.go` — i.e. it would have deleted three working subcommands.

(Note: regenerating a **top-level** command did *not* drop the root's other top-level
commands in the same way — only the parent-of-the-regenerated-subcommand's child list
was rebuilt. So the bug is specific to the parent→child list merge.)

### Expected
Re-registering a subcommand should **merge** into the parent's existing `commands:` list
(preserve untouched siblings), not replace it with only the touched command(s).

### Impact
Silent manifest corruption that becomes **source deletion** on the next
`regenerate project`. Easy to miss because the build/tests stay green until someone
regenerates.

### Workaround used
`gtb regenerate manifest` (rescan source → rebuild manifest) restored the dropped
entries. But see Bug 2.

---

## Bug 2 — `regenerate manifest` cannot resolve code-constant flag defaults

`gtb regenerate manifest` warns and **leaves the default unset** for any flag whose
default is a Go constant it can't evaluate:

```
WARN Could not resolve default value for flag 'n' in pkg/cmd/cover/cmd.go (value: defaultN); leaving default unset
WARN Could not resolve default value for flag 'port' in pkg/cmd/studio/cmd.go (value: defaultPort); leaving default unset
```

In keryx these constants were all `int(int64(0))` = 0, so "unset" happened to be
harmless. But for a **non-zero** code-constant default, this would silently change the
default to the zero value on the next `regenerate project`. The scanner should resolve
simple constant initialisers (at least integer/string literals and `int(int64(N))`), or
preserve the original default token rather than dropping it.

---

## Bug 3 — `regenerate manifest --dry-run` is not honoured

`gtb regenerate manifest --dry-run` **writes `manifest.yaml` anyway** (logs
`Writing updated manifest.yaml...` and the file changes on disk). By contrast,
`gtb regenerate project --dry-run` **is** honoured (previews without writing). The
`--dry-run` flag should be respected (or rejected) consistently across both
`regenerate` subcommands.

---

> **Note:** the missing linux/amd64 release asset (`gtb update` → "unable to find asset
> for Linux x86_64") is a **known issue already being resolved** — not included here.
> Until it lands, install the gtb binary from source:
> `go install gitlab.com/phpboyscout/go-tool-base/cmd/gtb@vX.Y.Z`.

---

## Suggested investigation order
1. **Bug 1** is the dangerous one (silent source deletion). Look at where
   `generate command` updates the parent entry's `commands:` slice in the manifest —
   it appears to overwrite rather than merge.
2. **Bug 3** (dry-run ignored) is a quick correctness fix and makes Bug 1/2 safer to
   diagnose (you can preview `regenerate manifest` without mutating).
3. **Bug 2** (constant defaults) — resolve simple constant initialisers, or round-trip
   the original default expression.

A clean round-trip property worth asserting in tests:
`regenerate manifest` → `regenerate project --dry-run` should be a **no-op** for an
unchanged project (currently it isn't: the manifest's tool block re-indents 4-space ↔
2-space, and flag defaults flip between an inline literal and a `defaultX` const).

---

## Resolution (go-tool-base)

Each item below has a test under `internal/generator/keryx_bug_validation_test.go`
or `pipeline_integration_test.go`.

- **Bug 1 — could not reproduce.** `generate command`'s manifest writer
  (`updateOrAppendCommand`) already merges in place; re-registering a subcommand
  preserves untouched siblings, and `regenerate project` preserves every
  subcommand registration (no source deletion). A regression test guards this.
  Likely an older binary or a symptom misattributed from a separate operation.
- **Bug 2 — fixed.** Nested-conversion constant defaults such as `int(int64(0))`
  now resolve (the resolvers recurse through single-arg conversions) instead of
  being warned-and-unset.
- **Bug 3 — fixed.** `regenerate manifest --dry-run` is honoured: it prints a
  unified diff of `.gtb/manifest.yaml` and writes nothing.
- **Round-trip — fixed.** A single 2-space serialiser is now used by every
  manifest write site, so `regenerate manifest` is byte-idempotent. Two related
  cosmetic divergences between the incremental and canonical command renderers
  were also closed: child imports now carry the same alias on both paths, and a
  boilerplate leaf is reshaped to canonical parent form on its first child.
  - *Known residual:* a root-level `NewCmdRoot(...)` call's argument
    line-wrapping can still differ between the two paths. It drops no
    registrations and is gofmt-stable; full convergence is deferred to a
    manifest-driven re-render of the root on `generate command`.
