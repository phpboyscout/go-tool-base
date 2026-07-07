# Config Refactor Blog Notes

_Started: 2026-07-07_

Working notes for a future blog backlog item/post about the GTB config
decoupling work. These notes are intentionally factual and source-oriented; the
blog framing should be written later from the completed implementation, commit
history, and the author's motivation.

## Working Thesis

The interesting story is not "we added another config helper". It is the moment
where a framework author realises a useful framework abstraction can become a
dependency trap for the modules he wants to set free.

The design turn:

- GTB keeps its rich runtime config stack.
- Extracted packages own typed config structs.
- GTB unmarshals resolved config sections into those structs at the boundary.
- Packages receive data, not the framework.

## Why This Exists

- Package extraction exposed that `pkg/config` was becoming the next coupling
  point after `pkg/logger`.
- `chat` is the proving ground because it is a high-value extraction candidate
  and currently reads GTB config directly in several places.
- The constraint is strict: do not change user-facing config structure or
  loading semantics unless there is no sane alternative.
- The aim is inversion without making every module inherit Viper, afero,
  fsnotify, pflag binding, hot reload, and GTB env-prefix semantics.

## Source Trail

- `docs/development/reports/2026-07-07-package-extraction-report.md`
- `docs/development/specs/2026-07-07-slog-first-extraction-seams.md`
- `docs/development/specs/2026-07-07-config-section-adapters-for-extraction.md`
- `docs/development/specs/2026-07-05-chat-module-extraction.md`

## Implementation Notes

Add dated notes here as the work progresses:

- 2026-07-07: Established the pattern: typed package config structs plus
  GTB-side section unmarshalling. Decided not to make extracted modules depend
  on `pkg/config` by default.
- 2026-07-07: Commit and MR text for this work must follow the repository
  release constraints: no assistant attribution in forge-facing metadata, and no
  conventional-commit breaking syntax while GTB remains on the v0 release line.
- 2026-07-07: Applied the pattern first to `pkg/chat`: `chat.New` became the
  GTB adapter that loads runtime, fallback, and credential sections into
  package-owned structs. Provider constructors now receive typed credential data
  rather than reading `config.Containable` directly.

## Questions For Matt Before Drafting

- What was the moment that made `pkg/config` feel like a coupling problem rather
  than just a useful framework abstraction?
- Is the post a design-decision piece about module boundaries, or a war story
  about discovering that the next extraction blocker was self-inflicted?
- What is the analogy that should carry the piece: config as a translator at the
  border, customs paperwork, a shipping container, or something else?
