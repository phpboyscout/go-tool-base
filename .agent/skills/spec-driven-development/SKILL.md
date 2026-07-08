---
name: spec-driven-development
description: Drive any AI-involved change through a written, approved spec first — no implementation without a spec it implements. Use when starting a feature, a component, or any non-trivial change; the spec is the decision record the work cites.
---

# Spec-driven development

Use this whenever you're about to build something non-trivial with an AI in the
loop — a feature, a component, a migration, a refactor. The rule is simple and
strict: **no implementation change without a spec it implements.** The spec is
the authoritative decision record; the code is downstream of it.

Why this matters with an AI: the expensive part isn't typing the code, it's
deciding *what* to build and *why*. Writing that down first turns a vague prompt
into a reviewable artifact, stops the AI inventing scope, gives every later MR a
thing to cite, and leaves a durable record of the decisions (and the options
rejected) long after the chat is gone.

## The spec file

- **Location + naming:** keep specs together under a `docs/.../specs/` directory,
  one file per decision, named `<YYYY-MM-DD>-<slug>.md`. The date orders them; the
  slug says what it is.
- **Frontmatter with a lifecycle status.** Every spec carries
  `status: draft | approved | rejected | implemented` (plus title, date, author).
  The status is the single source of truth for where the decision stands:
  - **draft** — under discussion; do not implement against it yet.
  - **approved** — agreed; safe to implement.
  - **rejected** — considered and decided against, or superseded by a later
    spec. **Keep it, don't delete it** — the value is the durable "we thought
    about X and chose not to, for these reasons" record, so the question
    doesn't get re-litigated from scratch later.
  - **implemented** — shipped (flip it when the change merges / tags).
- **Structure the decisions, number them.** Write the decisions as a numbered
  list (D1, D2, …) so reviews, MRs, and later specs can reference "D3" precisely.
  Record a short **Resolved** section for questions settled during review, dated,
  so the *why* survives — and an **Open questions** section for what's still live.

## The discipline

1. **Draft the spec before touching the implementation.** If you find yourself
   editing code without a spec that covers it, stop and write (or extend) the
   spec first.
2. **Get it to `approved` before implementing — with the human, not for them.**
   Walk the open questions one at a time and let the human decide each
   decision-bearing one; don't silently pick the convenient option to keep
   moving. Record each resolution in the spec, dated, so the *why* survives long
   after the conversation is gone.
3. **Cite the spec in the work.** Every MR/PR that implements a decision names
   the spec (and the decision number) it implements. Reviewers check the code
   against the spec, not just against itself.
4. **Flip `status: implemented` when it ships** (at merge or tag). A spec amended
   after the fact (a decision proved wrong in practice) gets a dated revision
   note rather than a silent edit — the record should show the change of mind.

## When a spec is overkill

Trivial, mechanical, reversible changes (a typo, a version-pin bump, a rename)
don't need a spec — forcing one is just ceremony. The bar is *non-trivial or
decision-bearing*: if a reasonable person would ask "why was it done this way?",
it needed a spec.
