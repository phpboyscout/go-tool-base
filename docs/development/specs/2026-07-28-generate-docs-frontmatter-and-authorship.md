---
title: "generate docs: frontmatter-first output and non-model authorship"
description: "gtb generate docs writes the model's conversational preamble above the YAML frontmatter (breaking it) and fills the frontmatter authors field with the AI model identity. Strip any preamble ahead of the first frontmatter fence and assert a frontmatter-first byte-0 invariant on write; populate authors from project configuration (never the model identity), optionally recording provenance in a separate field."
date: 2026-07-28
status: IMPLEMENTED
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
:   IMPLEMENTED — maintainer decisions resolved (see §4); fix landed on this branch

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

## 4. Solution (maintainer decisions resolved)

The maintainer reviewed the §1 reporter suggestions and the §2 analysis and
resolved the two defects as follows. This section is the decision record; the
fix implementing it landed on this branch.

**Defect 1 — enforce frontmatter-first.** Straightforward, as the reporter
suggested. `writeAIDocs` (`docs.go`), after `sanitizeAIOutput`, now routes the
model output through a focused helper `stripToFrontmatter(string) (string, bool)`
that discards everything ahead of the first line that is exactly `---` and
returns the content from that fence onward. The bytes handed to
`afero.WriteFile` are then asserted to begin with `---` (a cheap frontmatter-first
invariant). If, after stripping, **no** `---` fence is found at all, that is a
generation failure worth surfacing: the helper reports `ok=false`, `writeAIDocs`
returns the sentinel `ErrNoFrontmatter`, and `GenerateDocs` falls back to the
deterministic boilerplate via `handleNoAIDocs` rather than committing a
frontmatter-less page. The strip is applied on **both** the command and package
AI write paths (they share `writeAIDocs`). Only a **leading** preamble is
stripped; content within and after the frontmatter is untouched (§6 Q4).

**Defect 2 — additive AI co-authorship by default, opt-out flag.** The
maintainer **rejected** the "remove AI attribution entirely" direction. AI
attribution in generated *docs* is acceptable — docs are a common human
oversight and the AI genuinely contributes — but two things change:

- **(a) Make it additive.** The prompt still names the current AI model, but the
  instruction now tells the model to **preserve every existing (human) author**
  and *additionally append* the AI model as a **co-author**, never to replace or
  drop the human. This fixes the reporter's replacement + churn complaint at the
  instruction level (the authors content is produced by the model, so the fix is
  prompt-driven).
- **(b) Add a `--no-ai-attribution` flag** to `generate docs`. When set, it
  **flips the system prompt**: the `authors:` field is scoped to the project's
  human author(s) only, and the model is explicitly instructed to add **no**
  AI/model/assistant/tool identity. This is the opt-out for no-AI-attribution
  projects.

Implementation: the hard-coded "You MUST append the current AI model (…)"
instructions (`docs.go` command prompt frontmatter bullet / worked example /
INSTRUCTIONS merge line, and the package prompt equivalents) are replaced with
`%s` placeholders filled by a new `authorsDirectives(provider, model)` helper
that returns the frontmatter directive, the merge directive, and the example
authors line — additive-co-author wording by default, human-only wording when
`Config.NoAIAttribution` is set. The flag threads
`DocsOptions.NoAIAttribution → generator.Config.NoAIAttribution`.

A separate `generated_by:` provenance field (§6 Q2) was considered and **not**
adopted: co-authorship in `authors:` already records the contribution when
wanted, and `--no-ai-attribution` removes it entirely when not — a second field
would be redundant.

## 5. Acceptance criteria

The tests in `internal/generator/docs_issue7_test.go` are **green** with the fix,
and no existing generator test regressed:

1. `TestGenerateDocs_Issue7_PreambleStrippedAboveFrontmatter` — given a mock
   chat client whose response carries conversational preamble ahead of the
   frontmatter, the written command doc begins with `---\n`, contains none of the
   preamble text, and the human author in the model output is preserved.
2. `TestGenerateDocs_Issue7_NoFrontmatterFallsBackToBoilerplate` — a response
   with **no** `---` fence at all does not produce a frontmatter-less committed
   file; the generator falls back to the (frontmatter-first) deterministic
   boilerplate.
3. `TestGenerateDocs_Issue7_AuthorsAdditiveByDefault` — by default the prompt
   injects the AI model as a **co-author** and instructs the model to *preserve*
   existing (human) authors and *append* the AI model (additive, not replacing).
4. `TestGenerateDocs_Issue7_NoAIAttributionFlag` — with `--no-ai-attribution`
   the prompt does not inject the AI model identity, does not instruct appending
   the AI model, and scopes authorship to the project's human author(s).
5. `TestGenerateDocs_Issue7_NoAIAttributionPackagePrompt` — the flag applies to
   the package doc prompt too (both prompts share `authorsDirectives`).

## 6. Open questions (resolved / deferred)

1. **Authorship source of truth.** *Resolved:* authorship stays **model-driven
   via the prompt** — the model preserves existing frontmatter authors and, by
   default, appends the AI model as a co-author. A deterministic `--author` /
   manifest / git-config precedence chain was **not** adopted for this fix; the
   defect was replacement of the human author, which the additive-co-author
   instruction (plus the `--no-ai-attribution` opt-out) addresses without a new
   authorship pipeline.
2. **`generated_by:` provenance.** *Resolved (not adopted)* — see §4. Redundant
   with additive co-authorship + `--no-ai-attribution`.
3. **No-frontmatter response handling.** *Resolved:* fall back to the
   deterministic boilerplate (`handleNoAIDocs`) via the `ErrNoFrontmatter`
   sentinel. No retry loop — cheaper and deterministic.
4. **Scope of the strip.** *Resolved:* strip only a **leading** preamble (issue
   #7's evidenced case). A trailing sign-off strip is deliberately not done — it
   risks eating legitimate content.
5. **Existing generated pages.** *Deferred:* repair is left to the next
   per-page regeneration, which now emits frontmatter-first, additive-authors
   output. No bulk `regenerate` migration is shipped with this fix.
