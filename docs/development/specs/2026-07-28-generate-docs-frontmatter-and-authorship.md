---
title: "generate docs: frontmatter-first output and non-model authorship"
description: "gtb generate docs writes the model's conversational preamble above the YAML frontmatter (breaking it) and fills the frontmatter authors field with the AI model identity. Strip any preamble ahead of the first frontmatter fence and assert a frontmatter-first byte-0 invariant on write; populate authors from project configuration (never the model identity), optionally recording provenance in a separate field."
date: 2026-07-28
status: DRAFT
tags:
  - specification
  - generator
  - docs
  - frontmatter
  - authorship
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# `generate docs`: frontmatter-first output and non-model authorship

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   28 July 2026

Status
:   DRAFT — pending review

Related
:   [Issue #7 — generate docs: model preamble leaks above YAML frontmatter, and authors: is filled with the model identity](https://gitlab.com/phpboyscout/go-tool-base/-/work_items/7);
    the independent code review that backs this spec is recorded inline in
    §2 (root cause) and §3 (validation), each cited to `file:line` on
    `origin/main` at `e7578e20`.

## Summary

`gtb generate docs`, in its AI-assisted path, writes two defects into every
command- and package-reference page it produces:

1. The model's conversational preamble is written **above** the YAML
   frontmatter, so the frontmatter is no longer the first bytes of the file and
   stops being parsed as frontmatter by any static-site generator.
2. The frontmatter `authors:` field is populated with the **AI model identity**
   (e.g. `Claude (claude-opus-4-8)`) rather than the project's author.

Both are reproduced by red tests in
`internal/generator/docs_issue7_test.go` (see §5). This spec records the
independent root-cause analysis and the proposed fix direction; it does **not**
implement the fix.

## 1. Reported problem

Reported against `gtb v0.32.0` (issue #7), observed on two separate invocations
over the same command, so systematic rather than a sampling artefact.

**Defect 1 — preamble above frontmatter.** The generated file begins with the
model's narration, e.g.:

```markdown
I'll analyze the code and inspect the referenced packages to ensure accurate documentation.I now have enough context to generate accurate documentation.

---
title: scoutdm ingest
...
---
```

The reporter notes the missing sentence separator (`documentation.I now have`),
suggesting the leak is the model's non-content turn written straight through
rather than a formatting slip. **Suggested fix (reporter):** discard anything
before the first `---`, or require the response to begin with frontmatter and
retry otherwise; a post-write assertion that byte 0 is `---\n` would catch the
class.

**Defect 2 — model identity in `authors:`.** The frontmatter carries
`authors: [Claude (claude-opus-4-8)]` where neighbouring hand-authored pages
carry the human maintainer. The reporter flags this as more than cosmetic: it
asserts AI authorship in a committed, machine-readable field (contrary to
common no-AI-attribution policy) and churns the diff whenever the configured
provider/model changes. **Suggested fix (reporter):** populate `authors:` from
project configuration (manifest, git config, or an explicit `--author` flag) and
never from the model identity; record generation provenance, if wanted, in a
separate `generated_by:` field.

## 2. Independent root-cause analysis

The reporter's diagnosis is **correct on both defects**. Evidence from the
current code (`internal/generator/docs.go` on `origin/main` @ `e7578e20`):

### Defect 1 — preamble leak: **AGREE**

The AI response is captured in `writeAIDocs` (`docs.go:233-277`). It calls
either `StreamChat` or `Chat` on the chat client (`docs.go:243-253`) — a client
that has tools registered (`createAIDocsClient` → `SetTools`, `docs.go:829`), so
the returned string is the aggregation of the model's turns across the
tool-calling (ReAct) loop, including any pre-tool-call narration. The **only**
post-processing before the file is written is `sanitizeAIOutput`
(`docs.go:259`, defined `docs.go:763-772`):

```go
func (g *Generator) sanitizeAIOutput(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		if idx := strings.Index(content, "\n"); idx != -1 {
			content = content[idx+1:]
		}
	}
	return strings.TrimSpace(content)
}
```

It trims whitespace and strips a single leading code fence — **nothing** removes
content ahead of the frontmatter, and there is no frontmatter-first assertion
anywhere on the write path (`writeAIDocs` → `afero.WriteFile`, `docs.go:268`).
So when the model emits narration before the `---` block, it is written
verbatim as bytes 0..n of the file. The concatenation-without-separator the
reporter saw (`documentation.I now have`) is consistent with two assistant text
turns from the tool loop being glued together upstream and returned as one
string. **Confirmed.**

### Defect 2 — model identity in `authors:`: **AGREE — and it is deliberate in the prompt**

This is not the model volunteering its name; the generator **instructs** it to.
In `getPromptAndOutput` (`docs.go:506-521`):

```go
aiAuthor := fmt.Sprintf("%s (%s)", g.capitalize(provider), model)  // docs.go:512
```

With the default provider `claude` (`gochat.ProviderClaude = "claude"`) and a
resolved model `claude-opus-4-8`, `capitalize` yields exactly
`Claude (claude-opus-4-8)` — a byte-for-byte match for the reported
`authors: [Claude (claude-opus-4-8)]`. That string is interpolated into both
system prompts:

- Command prompt (`docs.go:81`):
  `- authors: A list of authors. You MUST append the current AI model ("%s") to any existing authors.`
  reinforced at `docs.go:116` (`You MUST merge existing authors with the current AI model.`)
  and by the worked example at `docs.go:90`
  (`authors: [human-maintainer, gemini-2.0-flash-exp]`).
- Package prompt (`docs.go:40`): `- authors: A list of authors. Append "%s" to existing.`
  reinforced at `docs.go:53`.

There is **no** code path that reads a project author (git config, manifest, or
flag) for `authors:` — the field is sourced solely from the model identity by
prompt construction. **Confirmed.**

## 3. Validation on current main

**Verdict: BOTH DEFECTS PRESENT on `origin/main` @ `e7578e20`** (well past the
reported `v0.32.0`; the recent heavy generator/Diátaxis work did not touch this
path). Reproduced by the red tests in `internal/generator/docs_issue7_test.go`.

Red evidence (`go test ./internal/generator/ -run TestGenerateDocs_Issue7`):

- `TestGenerateDocs_Issue7_PreambleStrippedAboveFrontmatter` — FAIL. The written
  file's leading bytes are
  `"I'll analyze the code and inspect the referenced packages…"` instead of
  `---\n`; the preamble is present in the committed doc.
- `TestGenerateDocs_Issue7_AuthorsNotModelIdentity` — FAIL. The constructed
  command system prompt contains
  `authors: A list of authors. You MUST append the current AI model ("Claude (claude-opus-4-8)")…`.

## 4. Proposed solution direction

Not implemented here; captured for review.

**Defect 1 — enforce frontmatter-first.** Extend `sanitizeAIOutput` (or add a
dedicated step in `writeAIDocs` before write) to discard everything ahead of the
first line that is exactly `---`. Then assert the invariant: the bytes handed to
`afero.WriteFile` must begin with `---\n`. If, after stripping, no frontmatter
fence is found, treat it as a generation failure (fall back to the deterministic
boilerplate via `handleNoAIDocs`, or retry once) rather than writing a broken
page. Apply on **both** the command and package AI write paths. A focused helper
(e.g. `stripToFrontmatter(string) (string, bool)`) keeps this testable in
isolation.

**Defect 2 — authorship from project, not model.** Remove the AI-identity
injection from both prompts (`docs.go:40,53,81,90,116`) and stop computing
`aiAuthor` for the authors field (`docs.go:512`). Populate `authors:`
deterministically from project configuration, with a clear precedence — e.g.
explicit `--author` flag → manifest author/maintainer field → git
`user.name`/`user.email` — and **preserve existing authors** already present in
the file's frontmatter (the "merge/preserve manual edits" intent stays, minus
the AI append). If generation provenance is worth recording, add a **separate**
`generated_by:` frontmatter field that projects can opt out of; it must never
occupy `authors:`. Since authors then no longer depend on the model, the
generator can set/merge it deterministically post-response rather than trusting
the model to echo it.

Open design points are deferred to §6.

## 5. Acceptance criteria

The following tests (added red in this spec's branch,
`internal/generator/docs_issue7_test.go`) must go **green** once the fix lands,
and no existing generator test may regress:

1. `TestGenerateDocs_Issue7_PreambleStrippedAboveFrontmatter` — given a mock
   chat client whose response carries conversational preamble ahead of the
   frontmatter, the written command doc begins with `---\n` and contains none of
   the preamble text.
2. `TestGenerateDocs_Issue7_AuthorsNotModelIdentity` — the constructed
   documentation prompt does not inject the AI model identity into an authors
   instruction, and does not instruct the model to append the AI model to
   authors.

Additional coverage to add alongside the implementation (not yet written):

3. A package-doc equivalent of (1) (the package AI write path shares
   `sanitizeAIOutput`).
4. A positive test that `authors:` is populated from project configuration and
   that a pre-existing human author in the file's frontmatter is preserved
   through regeneration.
5. A test that a response with **no** frontmatter at all does not produce a
   frontmatter-less committed file (fallback/retry path).

## 6. Open questions

1. **Authorship source of truth.** Which wins, and in what order — a
   `--author` flag, a manifest author/maintainer field (does one exist, or must
   it be added?), or git `user.name`/`user.email`? What is the fallback when
   none is configured (empty `authors:`, omit the field, or a tool-name
   placeholder)?
2. **`generated_by:` provenance.** Do we want it at all? If so, is it
   default-on or default-off, and does it carry provider+model or just "AI"?
   (No-AI-attribution projects will want it absent entirely.)
3. **No-frontmatter response handling.** On a response that yields no `---`
   block after stripping, do we fall back to deterministic boilerplate, retry
   the model once, or hard-fail the command? Retry has cost/latency
   implications for the tool-calling loop.
4. **Scope of the strip.** Only strip a leading preamble, or also detect and
   drop a trailing model sign-off after the content? Issue #7 only evidences a
   leading leak; a trailing strip risks eating legitimate content.
5. **Existing generated pages.** Should `regenerate` proactively repair pages
   already carrying a model identity in `authors:` / a leaked preamble, or is
   that left to the next per-page regeneration?
