---
name: diataxis-docs
description: Structure documentation with the Diátaxis framework (tutorials / how-to / reference / explanation), accommodate a hybrid docsite-plus-marketing site, split oversized docs into subsections, defer language API references to the package registry, and verify every claim against the source. Use when writing, restructuring, or auditing a project's docs.
---

# Documentation with Diátaxis

Use this whenever you write, restructure, or audit a project's documentation.
Adhere to the **[Diátaxis](https://diataxis.fr/)** framework, with two deliberate
adaptations for the common case where the docsite *also* serves as the project's
marketing/product website.

## The four quadrants

Every document belongs to **exactly one** quadrant — don't mix their purposes.

1. **Tutorials** (learning-oriented) — let a newcomer learn by doing.
   Step-by-step, low-context, *guaranteed to work*. Focus on the user's journey,
   not the product's features.
2. **How-to guides** (task-oriented) — show how to solve one specific problem.
   Goal-oriented, practical, sequential steps, for someone who already knows the
   basics.
3. **Explanation / concepts** (understanding-oriented) — the *why* and *how it
   works*. Discursive, architectural; design philosophy, alternatives, internal
   mechanics. Architectural component overviews live here.
4. **Reference** (information-oriented) — accurate, comprehensive facts. Austere
   and structured: CLI flags, config schemas, environment variables, REST APIs.

## The hybrid docsite/website compromise

When the same docs power both the technical docsite *and* the public website,
two pragmatic departures from pure Diátaxis are allowed:

- **Landing/about pages alongside explanation.** Product pitches, "why use X?",
  and landing content may live in an `about/` section or beside technical
  explanation. Pure Diátaxis says marketing isn't documentation; the hybrid
  pattern needs these pages to serve both audiences.
- **Tutorials may live off-site and be referenced, not duplicated.** When the
  same team's blog or marketing site already hosts the learning content,
  re-authoring it into the docsite is derivative and splits traffic away from the
  canonical posts. In that case the `tutorials/` quadrant is a *curated index that
  links out* to the external posts — not a set of local tutorial pages, and not a
  gap. This is a deliberate concession: don't flag an externally-referenced
  Tutorials section as "empty" or recommend duplicating the content locally.
  (Keep the index's links pointed at real published posts, not placeholders.)
- **Component-driven concepts.** Rather than isolating all conceptual material in
  a generic `concepts/` directory, fold architectural/conceptual overviews into
  the relevant `components/` docs. This avoids fragmentation and duplication.
- **Marketing voice in task docs is allowed — fix structure, not tone.** Some
  how-to pages carry a deliberate persuasive voice because they double as product
  pages. When such a page has drifted into a *feature tour* (no task spine,
  rationale where steps belong), give it a **light** fix: add a clear goal and
  numbered steps, move deep internals to explanation, but **keep the voice**. Do
  not sanitise marketing tone into austere prose unless asked.
- **TUI / embedded docs.** Docs may be rendered inside a CLI (a TUI browser, an
  AI assistant). Keep markdown clean — standard GitHub-Flavored Markdown, no
  complex HTML/JS macros that won't render in a terminal or static-site generator.

## Split large docs into subsections

A single page that has grown past ~400–500 lines or carries many distinct H2
topics is hard to scan and hard to navigate. Split it into a **subsection**: a
directory with an `index.md` landing page plus one focused page per topic. Most
static-site generators auto-build the nav from the directory tree, so the split
becomes an expandable nav group for free.

- Group the page's H2 sections into coherent topic pages; keep the overview,
  rationale, and "why" on the `index.md`, and add a short **"In this section"**
  list linking to the topic pages.
- **Preserve all content** — splitting is a move, not a rewrite. Verify by
  line-count / section coverage that nothing was dropped, and de-duplicate any
  repeated headings the split exposes (e.g. two "Best Practices" sections).
- Mirror the convention already in the codebase (e.g. if `setup/` and `vcs/` are
  already subsections, make `config/`, `chat/`, etc. match).
- Splitting moves files, so it **breaks inbound links** — see the link sweep in
  the workflow below.

## Defer the language API reference — and know what NOT to move

Documentation is narrative, usage, and architecture — **not** a dump of
auto-generated package definitions. Don't paste massive interface/struct
definitions into markdown.

- **Defer code APIs to the language's registry.** Go → `pkg.go.dev/...`; Rust →
  `docs.rs`; etc. Instead of `type MyInterface interface { … }`, give a one-line
  purpose and a link. Keep the **package-level doc comments** accurate too — that
  registry page *is* your code reference.
- **Reserve `reference/` for non-code surfaces** — CLI commands and flags, config
  file schemas (YAML/JSON), environment variables, REST API definitions.
- **Don't extract a table just because it's tabular.** A capability matrix, an
  options enumeration, or a sentinel-error list that *supports the surrounding
  explanation* has **no home in `reference/`** when component APIs live in the
  registry — and moving it strips the explanation of what makes it useful. Leave
  supporting tables where they explain; only move a table that is a standalone
  lookup surface (CLI flags, config keys, env vars).

## Generate reference from the source, not from memory

Reference pages must be exhaustive and exact, so build them *from* the source of
truth:

- **CLI reference** ← the command framework's command tree (e.g. Cobra `Use` /
  `Short` / `Long` / flag registrations). Document subcommands, args, and flags as
  they are declared; end each with a pointer to `--help` for the authoritative set.
- **Config / env reference** ← the config schema and the shipped default config
  files; state types, defaults, precedence, and the env-var mapping.
- **Separate audiences.** If a project has both a runtime CLI (shipped in the
  built tool) and a developer/build CLI (the framework's own commands), give them
  separate reference sections — they serve different readers.

## Verify claims — about the code *and* its dependencies

A doc that asserts something must be checked against reality, not training data.

- **Reference is a promise of accuracy** — every documented flag, command,
  signature, default, and path must match the implementation. Verify against the
  source.
- **Audit claims about upstream dependencies.** Docs love to justify a wrapper by
  asserting a limitation of the thing it wraps ("slog can't change levels at
  runtime", "viper is hard to mock", "this provider only supports X"). These go
  stale or were never true. **Verify each against the actual pinned dependency
  source** (the module cache / vendored code / the library's own docs at the
  pinned version) — not memory. When a justification turns out false, find the
  *true* reason the wrapper exists and state that instead; don't just delete it.
- **Refresh moving targets.** Model names, default versions, provider lists, and
  pricing-style facts drift fastest — grep for them and reconcile to one source.

## Workflow

When writing or auditing:

1. **Identify the quadrant** — "is this teaching, guiding, explaining, or
   referencing?" Put it in the right place. Common misplacements to fix:
   - an *explanation* page that is really a cookbook of task steps → move to
     **how-to** (and delete the duplicate if a how-to already covers it);
   - a *how-to* that is a feature tour / rationale essay → add a task spine, push
     the "why" to **explanation**;
   - a *reference* page carrying a `main.go` walkthrough or enable/disable recipe
     → replace with a facts table + a link to the how-to.
2. **Strip duplication** — if a reference doc drifts into architecture, move that
   to explanation; if an explanation lists every flag, move the flags to
   reference. Consolidate facts repeated across pages to one canonical home and
   link to it.
3. **Link, don't copy** — strip large API code blocks; link to `pkg.go.dev` /
   `docs.rs` instead. But keep *supporting* tables in place (see above).
4. **After a restructure, verify the links.** Moving or splitting files silently
   breaks every inbound relative link and any nav that names a path. Sweep the
   *whole* docset for links to the old paths and fix them — recompute relative
   depth (a page moved one level deeper needs an extra `../`). Aim for **zero
   broken links** before committing; a moved-but-still-linked page is the most
   common restructure regression. Beware path-rewriting tools matching substrings
   (e.g. `controls.md` inside `server-controls.md`) — match whole paths.
5. **Keep reference true to the code** and **verify dependency claims** (see the
   section above).
6. **Mind secret scanners.** Realistic-looking example tokens in docs (an
   `--api-token=sk-...` shown being redacted, a sample `Authorization` header)
   trip secret scanners — and scanners read git *history*, so the finding follows
   the example across every path it has lived at. Allowlist the **example value**
   (which a real secret would never match), not just the current file path.
