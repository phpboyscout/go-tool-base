---
title: "Observability & telemetry follow-ups: endpoint parsing, fallback options, spill-cap contract"
description: "Batched findings from the architectural review on the OTLP export path and the GTB telemetry spill queue: trailing-slash endpoints produce //v1/<signal> paths that strict collectors 404, Headers/Insecure are silently dropped when the endpoint comes from OTEL_* env vars, and pruneSpillFiles silently violates the documented at-least-once delivery guarantee."
date: 2026-07-23
status: DRAFT
tags:
  - specification
  - observability
  - telemetry
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Observability & telemetry follow-ups: endpoint parsing, fallback options, spill-cap contract

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   DRAFT — pending review

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (cross-cutting leaf modules section)

---

## 1. Context

The 2026-07-23 architectural review flagged three follow-ups on the telemetry export path: two LOW configuration foot-guns in the extracted `go/observability` module where a natural config value silently breaks or downgrades OTLP export, and one MEDIUM contract violation in GTB's `pkg/telemetry` where the spill-file cap quietly discards events that the `DeliveryAtLeastOnce` mode documents as never lost. All three are small; the observability items are one-line-plus-tests fixes in the module, and the GTB item is a documentation-and-logging change (the cap itself is the right engineering call — it just isn't part of the stated contract yet). This spec batches them into one review cycle.

## 2. Items

### 2.1 go/observability

#### [LOW] Endpoint trailing slash produces `//v1/<signal>` export paths

`go/observability/otelcore/endpoint.go:59-64` keeps `u.Path` verbatim in `Endpoint.BasePath`, and every consumer concatenates blindly — `logs/logs.go:71`, `metrics/metrics.go`, `tracing/tracing.go`, and GTB `pkg/telemetry/backend_otel.go:143` (`ep.BasePath + "/v1/logs"`). So `endpoint: https://collector:4318/` — a very natural config value — yields the URL path `//v1/logs`, which strict collectors 404, and the failure only surfaces as a DEBUG-level SDK export error, i.e. telemetry silently vanishes.

**Direction:** `strings.TrimRight(u.Path, "/")` in `ParseEndpoint` so `BasePath` never ends in a slash; all four consumers are then correct without change.

**Acceptance:** `ParseEndpoint` on `https://collector:4318/`, `https://collector:4318/otel/`, and the slashless equivalents yields `BasePath` values of `""` and `/otel`, and a consumer-side test asserts the joined signal path contains no `//`.

#### [LOW] `Headers` and `Insecure` silently dropped on the env-var fallback path

`go/observability/logs/logs.go:56-63` (same shape in `metrics/metrics.go` and `tracing/tracing.go`): when `Settings.Endpoint == ""` the exporter is constructed with **no options** — deliberately deferring the endpoint to the SDK's `OTEL_EXPORTER_OTLP_*` env vars — but this also drops a configured `s.Headers` auth token and `s.Insecure`. An operator who sets `telemetry.headers` in config while supplying the endpoint via `OTEL_EXPORTER_OTLP_ENDPOINT` exports **unauthenticated with no warning**.

**Direction:** on the fallback path, still pass `WithHeaders(s.Headers)` when non-empty and `WithInsecure()` when set (the SDK merges explicit options over env vars), or — if mixing explicit options with env-var endpoints proves surprising — log the drop at WARN. Prefer passing the options: the operator stated an intent and it should be honoured. Apply symmetrically across logs, metrics, and tracing.

**Acceptance:** with `Endpoint == ""` and `Headers` set, the constructed exporter sends the configured headers (verified against an `httptest` collector via `OTEL_EXPORTER_OTLP_ENDPOINT`), for all three signals.

### 2.2 GTB pkg/telemetry

#### [MEDIUM] `pruneSpillFiles` silently violates the documented at-least-once guarantee

`pkg/telemetry/spill.go:200-216` — `pruneSpillFiles` deletes the oldest spill files without any send attempt whenever the on-disk count reaches `maxSpillFiles` (10, `spill.go:18`), while `telemetrytypes.DeliveryAtLeastOnce` is documented as "deletes spill files only after a successful send … no data loss". A long-offline machine (backend unreachable, 10 spill files accumulated) permanently discards the oldest events on every subsequent spill, and the deletion routes through `removeSpillFile` with no operator-visible signal. Additionally, the prune frees room for exactly **one** file (`files[:len(files)-maxSpillFiles+1]`) while a single `spillToDisk` call may write **several** chunk files per spill, so a multi-chunk spill can immediately re-breach the cap it just pruned for.

**Direction:** keep the cap (bounded disk usage is correct) but make it part of the contract: (a) reword the `DeliveryAtLeastOnce` documentation in `telemetrytypes` and `docs/components/telemetry.md` to state the guarantee is bounded by `maxSpillFiles` — oldest spill files are discarded beyond the cap; (b) log each prune at **WARN** with the number of files/events discarded; (c) either make the prune free enough room for the number of chunks about to be written, or explicitly document the one-file-per-prune behaviour alongside the multi-chunk spill note. This is a docs+logging change on the GTB side; no delivery semantics change.

**Acceptance:** a test that accumulates `maxSpillFiles` spill files and triggers one more spill observes a WARN log recording the prune, and the `DeliveryAtLeastOnce` doc comment names the cap as an explicit bound on the guarantee.

## 3. Scope & release plan

- **Modules:** `gitlab.com/phpboyscout/go/observability` (items 2.1 — `otelcore` parse fix + fallback-option fix across logs/metrics/tracing) and GTB `pkg/telemetry` (item 2.2 — documentation and WARN logging only).
- **Releases:** observability ships as `fix(observability):` commits → releaser-pleaser patch release. The GTB change is `fix(telemetry):` (the WARN log plus doc contract correction) in go-tool-base directly — no module release involved.
- **GTB pickup:** a routine Renovate bump picks up the observability patch; `backend_otel.go`'s concatenation needs no change once `BasePath` is normalised. Update `docs/components/telemetry.md` (delivery-mode table) and the observability module docs in the same MRs as the code they describe.

## 4. Open questions

1. **Fallback-path options — pass or warn?** Passing `WithHeaders`/`WithInsecure` alongside an env-var endpoint honours stated operator intent but creates a hybrid config (endpoint from env, auth from file) some may find surprising. Recommendation: pass the options — a silently unauthenticated export is strictly worse than a hybrid source split; document the merge behaviour in the module README.
2. **Prune headroom:** should `pruneSpillFiles` learn the incoming chunk count and free that much room, or is documenting the one-file-per-prune behaviour enough? Recommendation: document only for now — the cap self-corrects on the next spill cycle, and threading the chunk count through adds coupling for a corner case; revisit if WARN logs show repeated same-cycle re-breaches in practice.
