---
title: "chat: expose sampling and reasoning-effort controls, and say honestly when a model will not take them"
description: "chat.Config offers no way to influence how a model generates: no temperature, no top-p, and no reasoning effort — while Usage.ReasoningTokens already reports what reasoning cost. Adds Temperature, TopP and a provider-neutral five-rung Effort ladder. The hard part is not the fields but the support model: measurement shows support is per-model, not per-provider — two models from the same vendor take opposite controls — so the construction-time capability marker that Config.Stateless established cannot express it. Structural gaps become construction errors; model-dependent rejection is passed through and wrapped in a sentinel."
date: 2026-07-31
status: DRAFT
tags:
  - specification
  - chat
  - sampling
  - reasoning
  - conformance
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 5
    role: AI drafting assistant
---

# chat: expose sampling and reasoning-effort controls, and say honestly when a model will not take them

Authors
:   Matt Cockayne, Claude Opus 5 *(AI drafting assistant)*

Date
:   2026-07-31

Status
:   DRAFT — pending review. Open questions resolved by the maintainer on 2026-07-31; see §6. Revised after a measurement pass against all three live APIs, which corrected one claim in the first draft and withdrew one decision entirely.

Related
:   [chat/#3](https://gitlab.com/phpboyscout/go/chat/-/work_items/3) (the originating report),
    [chat stateless one-shot calls](2026-07-28-chat-stateless-one-shot-calls.md) (the capability-marker precedent this spec deliberately does *not* reuse wholesale),
    [chat family provider-conformance suite](2026-07-23-chat-provider-conformance.md) (where the cross-provider cases belong)

---

## 1. Problem

`chat.Config` gives a caller no way to influence how the model generates. Two
distinct gaps, one shared root.

### 1.1 No sampling control

Verified against `origin/main` (v0.2.0). The only sampling-adjacent field is:

```go
// Seed, when non-nil, requests deterministic sampling from providers
// that support a seed parameter (currently OpenAI / OpenAI-compatible).
Seed *int64
```

`client.go:180-186`. It is read by exactly one provider —
`chat-openai/openai.go:216-217` — and ignored by Gemini, Anthropic and
ClaudeLocal. No provider sets `Temperature`, `TopP` or `TopK` anywhere.

All three vendor SDKs expose the fields at the pinned versions, so nothing is
blocked by the dependencies:

| SDK | Temperature | TopP | TopK |
|---|---|---|---|
| `anthropic-sdk-go@v1.61.0` | `param.Opt[float64]` | `param.Opt[float64]` | `param.Opt[int64]` |
| `openai-go/v3@v3.46.0` | `param.Opt[float64]` | `param.Opt[float64]` | — |
| `genai@v1.65.0` | `*float32` | `*float32` | `*float32` |

### 1.2 No effort control, while the bill for it is already reported

`Usage.ReasoningTokens` exists and is populated (`usage.go:31-33`), and
`chat-openai` maps it from `CompletionTokensDetails.ReasoningTokens`
(`openai.go:156`). **The module reports what reasoning cost and offers no way to
influence it.** A caller can see the bill and cannot touch the dial.

Every provider supports an effort control, including the one that supports no
sampling control at all.

### 1.3 Why this surfaced

`scoutdm` measured run-to-run stability of a two-stage extraction pipeline over
eight real session transcripts, two runs per configuration. Three of eight
sessions produced a different result from byte-identical input.

Every one of those numbers was taken at whatever the provider's default sampling
is, because the module offers no way to pin it. The measurement cannot currently
distinguish *"this prompt is unstable"* from *"this is what sampling at a
chat-tuned default looks like"* — and that distinction is the blocking question
in a downstream design spec.

The report is explicit that it is **not** asking for determinism, and that is the
right framing (§1.6).

### 1.4 What the providers actually accept — measured

Every cell below was measured against the live API on 2026-07-31, not read from
documentation. This matters: the first draft of this spec asserted, on the
strength of community reports about the GPT-5 family, that `gpt-5.4` rejects
temperature. **It does not.** The correction is left visible because it is the
reason this section exists.

**Temperature and top-p**

| Provider / model | Temperature | Notes |
|---|---|---|
| `gemini-3.5-flash` | ✅ 0.0–2.0 | `400` outside range: *"temperature must be in the range [0.0, 2.0]"* |
| `gpt-5.4` | ✅ 0–2 | `400` outside range: *"Expected a value <= 2"*. `seed` and `top_p` also accepted |
| `claude-sonnet-4-5` | ✅ 0–1 | |
| `claude-opus-4-8` **(default)** | ❌ | *"`temperature` is deprecated for this model."* |
| `claude-local` | ❌ | No sampling flag exists in the CLI at all |

**Reasoning effort**

| Provider / model | Field | Levels accepted |
|---|---|---|
| `gpt-5.4` | `reasoning_effort` | `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max` |
| `claude-opus-4-8` **(default)** | `output_config.effort` | `low`, `medium`, `high`, `xhigh`, `max` — *"Input should be 'low', 'medium', 'high', 'xhigh' or 'max'"* |
| `claude-sonnet-4-5` | — | ❌ *"This model does not support the effort parameter."* |
| `gemini-3.5-flash` | `thinkingConfig.thinkingLevel` | `MINIMAL`, `LOW`, `MEDIUM`, `HIGH`; `MAX` rejected. `thinkingBudget: 0` disables |
| `claude-local` | `--effort` | `low`, `medium`, `high`, `xhigh`, `max` |

Two things fall out that neither the report nor the first draft anticipated.

**Anthropic has a native effort enum, not a token budget.** The current model
rejects `thinking.type: enabled` outright — *"Use `thinking.type.adaptive` and
`output_config.effort` to control thinking behavior"* — and `output_config.effort`
works standalone, with no `thinking` block required. So there is no budget-token
mapping to invent; the levels are exactly the five the claude CLI exposes, which
makes sense since the CLI wraps this API.

**Gemini's levels are real, not nominal.** Measured on a fixed arithmetic prompt:
`thinkingLevel: LOW` → 158 thought tokens, `HIGH` → 176, `thinkingBudget: 0` →
exactly 0.

### 1.5 The finding that shapes everything: support is per *model*

The clearest evidence is two models from the same vendor:

```
claude-sonnet-4-5:   temperature ✅    effort ❌ "This model does not support the effort parameter."
claude-opus-4-8:     temperature ❌    effort ✅ "temperature is deprecated for this model."
```

Perfectly complementary. The older generation takes temperature and not effort;
the newer takes effort and not temperature. This is not a conflict between two
settings — it is a generational handover, and **which control is available
depends entirely on the model string the caller supplied.**

`claude-local` adds a third case again: it can never take sampling parameters at
any model, because the CLI has no such flag.

So the same field is structurally impossible on one provider, deprecated on one
model of another, and fully supported on a sibling model of that same provider.
**A per-provider capability marker cannot express that**, which is why the
`Config.Stateless` precedent does not transfer wholesale (D3).

### 1.6 What pinning the sampler does and does not buy

Recorded because it bounds what the acceptance criteria can honestly claim.

Temperature rescales the logits before the softmax: `p(i) = exp(z_i/T) / Σ
exp(z_j/T)`. `T < 1` sharpens the distribution toward the already-likely tokens;
`T > 1` flattens it. `T → 0` approaches greedy selection.

**Greedy is not deterministic.** Floating-point addition is not associative, so
GPU kernels that batch a request alongside other traffic can reduce the same
logits in a different order and differ in the last bits. Where two candidates are
near-tied that is enough to flip the choice, and one flipped token early changes
everything after it. Mixture-of-experts routing can also shift with batch
composition. Run-to-run variation at `T=0` is therefore expected, not a defect.

So pinning narrows the distribution being sampled from; it does not collapse it
to a point. Concretely, this separates two causes of `scoutdm`'s instability that
are currently indistinguishable:

- **Sampling variance** — the model was genuinely uncertain and the sampler chose
  differently. Lowering temperature shrinks this.
- **Genuine ambiguity** — the prompt or document does not determine one answer.
  Lowering temperature does *not* fix this; it makes the same near-tie land more
  consistently on whichever side is marginally ahead, which resembles a fix while
  concealing the real problem.

The deliverable is the ability to tell those apart. It is not a stability
guarantee, and nothing in §4 asserts one.

## 2. Proposed change

### D1 — `Config.Temperature` and `Config.TopP`, nil-by-default pass-through

```go
// Temperature, when non-nil, overrides the provider's default sampling
// temperature. Nil samples at the provider's default.
//
// The value is passed through in the provider's own units and is NOT
// rescaled — ranges differ (Claude 0-1, OpenAI 0-2, Gemini 0-2) and silently
// rescaling what the caller asked for would be worse than making them read
// the range. It is validated against the selected provider's range.
//
// Support is narrowing as vendors move to reasoning models: it is deprecated
// on claude-opus-4-8, absent from claude-local entirely, and available on
// Gemini, OpenAI and older Claude models. See Effort, which is the
// forward-looking control and is supported everywhere.
Temperature *float64

// TopP, when non-nil, overrides the provider's default nucleus-sampling
// threshold. Nil samples at the provider's default.
//
// Providers recommend adjusting temperature or top-p, not both.
TopP *float64
```

Pass-through over a normalised scale, following the report's reasoning: a
normalisation that quietly rescales is less honest than a documented range.

Shipping temperature despite the narrowing support (resolved OQ-6): it works on
Gemini across the full 0–2 range, which is where the originating measurement
runs, so it unblocks that work today. The godoc says plainly that it is
shrinking and points at `Effort`, which steers new code without blocking
existing need.

### D2 — `Config.Effort`, a five-rung provider-neutral ladder

```go
// Effort is a provider-neutral reasoning-effort level.
type Effort string

const (
    EffortLow    Effort = "low"
    EffortMedium Effort = "medium"
    EffortHigh   Effort = "high"
    EffortXHigh  Effort = "xhigh"
    EffortMax    Effort = "max"
)
```

An ordinal ladder is genuinely provider-neutral in a way a float range is not:
`low` means the same thing everywhere, whereas `0.5` means different things on a
0–1 scale and a 0–2 one.

Five rungs rather than OpenAI's seven (resolved OQ-1): `low…max` is exactly what
Anthropic and `claude-local` accept natively, and a strict subset of OpenAI's.
Adding `none`/`minimal` would put two values in the ladder that fail outright on
half the providers — a neutral API whose bottom two rungs are unusable on
Anthropic and claude-local is not neutral.

| `Effort` | `openai` | `claude` | `claude-local` | `gemini` |
|---|---|---|---|---|
| `Low` | `low` | `low` | `low` | `LOW` |
| `Medium` | `medium` | `medium` | `medium` | `MEDIUM` |
| `High` | `high` | `high` | `high` | `HIGH` |
| `XHigh` | `xhigh` | `xhigh` | `xhigh` | `HIGH` (clamped) |
| `Max` | `max` | `max` | `max` | `HIGH` (clamped) |

Every cell except the two Gemini clamps is a native value measured as accepted.
The clamps are documented in the godoc, not silent.

### D3 — Two kinds of "unsupported", each with its own failure mode

The heart of this spec. `Config.Stateless` established a construction-time
capability marker, and reusing it wholesale here would be wrong, because it
models the wrong thing (§1.5).

**Structural gaps — knowable at construction.** The provider has no such concept
at any version or model: `claude-local` and temperature/top-p. Nothing the caller
does makes these work.

→ **Construction error**, following the `StatelessCapable` precedent. A marker
interface per control, asserted in `New`:

```go
// SamplingCapable is implemented by provider clients that can carry
// Config.Temperature and Config.TopP.
type SamplingCapable interface {
    ChatClient
    SupportsSampling()
}

// EffortCapable is implemented by provider clients that can carry Config.Effort.
type EffortCapable interface {
    ChatClient
    SupportsEffort()
}
```

**Model-dependent rejection — only knowable at request time.** The provider
supports the parameter and *this model* refuses it. Anthropic is the worked
example in both directions: `claude-opus-4-8` rejects temperature, and
`claude-sonnet-4-5` rejects effort. Determining this at construction would
require the core to carry a per-model capability registry, which would be
permanently stale and is not a thing this module should own.

→ **Pass the parameter through and let the provider reject it**, but wrap the
rejection in a sentinel so the caller can tell it apart from every other 4xx:

```go
// ErrModelRejectedParameter wraps a provider's refusal of a sampling or effort
// parameter for the selected model — as distinct from the provider not
// supporting the parameter at all, which is a construction error. Inspect with
// errors.Is.
var ErrModelRejectedParameter = errors.New("model rejected a generation parameter")
```

The distinction matters because the two demand different fixes: a construction
error means *change provider*, a rejection means *change model or drop the
parameter*. Collapsing them into one error would tell the caller neither.

**What must never happen: silently dropping the parameter.** A request that goes
out without the temperature the caller asked for, and succeeds, reproduces
exactly the failure that made the `scoutdm` measurement worthless — the absence
of a control being indistinguishable from the control having no effect (§1.3).

### D4 — *Withdrawn.* No conflict rule is needed

The first draft proposed rejecting `Temperature` together with `Effort` at
construction on Anthropic, on the documented basis that enabling thinking
disallows temperature.

Measurement withdrew it. On Anthropic the two controls **never coexist on the
same model** — `claude-sonnet-4-5` takes temperature and rejects effort;
`claude-opus-4-8` takes effort and rejects temperature (§1.5). There is no
combination to catch, because on any given model at most one of the two is
available, and which one is a property of the model string.

Kept as a numbered, withdrawn decision rather than deleted: "we proposed a
conflict rule and measurement showed there is nothing to conflict" is the useful
record, and it is the cleanest demonstration that this is D3's second category
rather than a knowable-at-construction interaction.

### D5 — Validation before the wire

Each provider validates the values it was given against its own documented range
(§1.4) and returns an error before making a request.

Gemini and OpenAI both already validate server-side with good messages, so this
is not about correctness — it is about failing in one round-trip fewer, with a
consistent error shape across providers, and without spending a call to learn the
caller typed `2.5` for Claude.

### D6 — `TopK` is out of scope

Not uniform: `int64` on Anthropic, `float32` on Gemini, absent on OpenAI. A
neutral field would be a lie on at least one provider. Revisit if a caller asks.

### D7 — `Seed` stays as it is

Measured working on `gpt-5.4` (resolved OQ-3), so the existing godoc — a seed for
providers that support one, currently OpenAI — remains accurate. No change beyond
leaving it alone.

### D8 — Documentation

- Godoc on all three new fields, stating per-provider ranges, the Gemini effort
  clamps, and that `claude-local` takes no sampling parameters.
- New `docs/how-to/control-sampling.md`: what temperature and effort do, what
  pinning buys and what it does not (§1.6), the effort mapping table, and the two
  failure modes from D3.
- The `Config` reference table in `docs/how-to/configure-a-provider.md`.
- `docs/explanation/providers.md` gains a capability column, and states plainly
  that sampling and effort support are per-model — with the two Anthropic models
  as the example, because a reader who assumes per-provider support will
  otherwise be surprised exactly once, expensively.

## 3. Scope & release plan

| Repo | Change | Release |
|---|---|---|
| `go/chat` | `Temperature`, `TopP`, `Effort` + constants, `SamplingCapable`, `EffortCapable`, `ErrModelRejectedParameter`, `New` guard (D3), `claude-local` `--effort`, docs | `feat:` minor |
| `go/chat-anthropic` | temperature/top-p, `output_config.effort` + `thinking.type: adaptive`, both markers, rejection wrapping | `feat:` minor |
| `go/chat-openai` | temperature/top-p, `ReasoningEffort`, both markers, rejection wrapping | `feat:` minor |
| `go/chat-gemini` | temperature/top-p (narrowed to `float32`), `ThinkingLevel` + clamping, both markers | `feat:` minor |

`claude-local` implements `EffortCapable` but **not** `SamplingCapable` — the
first provider to hold one marker and not the other, which is the case D3 exists
to represent.

Same release order as the stateless work: core minor first, then each provider's
`go.mod` bump and minor, keeping the family at a matching minor.

## 4. Acceptance criteria

**The parameters reach the wire**

1. For each provider that supports it, a `Config` with `Temperature` set produces
   a request body carrying that value, in that provider's units, unrescaled.
   Likewise `TopP`.
2. A nil `Temperature`/`TopP`/`Effort` produces a request with no such field —
   existing behaviour byte-identical.
3. `Effort` maps per the D2 table for each provider, asserted against a recording
   backend, including the two Gemini clamps.
4. Anthropic sends `thinking.type: adaptive` alongside `output_config.effort`
   only where required, and the request is accepted by the live API (gated test).

**The two failure modes stay distinct**

5. `chat.New` with `Temperature` or `TopP` set and `Provider: ProviderClaudeLocal`
   returns a construction error naming the provider — it can never carry them.
6. `chat.New` with `Effort` set and `ProviderClaudeLocal` constructs, and the
   argv carries `--effort <level>`.
7. A model rejecting a parameter at request time returns an error satisfying
   `errors.Is(err, ErrModelRejectedParameter)`, with the underlying provider
   error still reachable by unwrapping.
8. **No silent drops.** For every provider, a request built from a `Config` with
   a generation parameter set either carries it or fails; a test asserts no
   request body omits a set parameter while returning success.

**Validation**

9. Out-of-range values fail before any HTTP request: Claude `Temperature: 1.5`,
   OpenAI `Temperature: 2.5`, any provider `TopP: 1.5`. Asserted with a backend
   that fails the test if it is called at all.
10. An unrecognised `Effort` value is a construction error.

**Live conformance (gated)**

11. A gated integration test per provider asserts the measured matrix of §1.4
    still holds — temperature accepted where the table says so and rejected where
    it says so, effort likewise. This is the test that would have caught the
    first draft's incorrect claim about `gpt-5.4`, and it is the only kind that
    can, since every one of these facts is a property of a live model rather than
    of our code. Run at release, not in CI.

**Everything else**

12. Existing suites pass untouched across all four modules; a `Config` with none
    of the new fields set produces identical requests.
13. Gemini narrowing to `float32` round-trips the values a caller can express
    without surprising precision loss.
14. Docs from D8 land with the core release; CI green (tests, race, lint) and
    coverage does not regress.

## 5. Open questions

**OQ-7 — Should the module surface which controls the selected model accepts?**
D3 deliberately declines to carry a per-model capability registry, and §1.5 shows
why that judgement is right — the matrix changed under us between two Claude
generations. But it leaves a caller writing provider-agnostic code with no way to
ask *"will this model take a temperature?"* short of sending a request and
catching `ErrModelRejectedParameter`. A trial-and-fall-back helper in the host,
rather than in this module, is probably the answer; worth recording that the
question was asked and where it belongs.

**OQ-8 — Does the effort ladder need a documented cost warning?** `Max` on a
reasoning model can multiply output tokens substantially, and `Usage.ReasoningTokens`
already exists to observe it. The how-to should probably pair the ladder with a
note that effort is the most direct cost dial in `Config` — but quantifying it
means a measured pass across providers, which is a bigger job than this spec
needs. Recommendation: ship the qualitative warning, and leave the numbers to
whoever wants them.

## 6. Resolved

**2026-07-31 — OQ-1: five rungs, `low…max`.** Superseded by measurement before it
was even answered as posed. Anthropic turned out to have a native effort enum
(`output_config.effort`) accepting exactly `low`, `medium`, `high`, `xhigh`,
`max` — identical to the claude CLI's `--effort`, which makes sense as the CLI
wraps that API. So the natural neutral ladder is those five: native on two
providers, a strict subset of OpenAI's seven, and clamped only at the top two
rungs on Gemini. `none`/`minimal` were dropped because they fail outright on
Anthropic and claude-local, and a ladder whose bottom rungs are unusable on half
the providers is not neutral. See D2.

**2026-07-31 — OQ-2: dissolved.** The question was what budget-token values map
to each effort rung on Anthropic. There is no mapping to invent — Anthropic takes
a level, not a budget, on the current model, and rejects the budget-based
`thinking.type: enabled` shape entirely. The proposed table in the first draft
was not merely uncalibrated, it was the wrong mechanism.

**2026-07-31 — OQ-3: `Seed` is not dead.** Measured accepted on `gpt-5.4`, along
with `top_p` and `reasoning_effort`. The existing godoc stays accurate; see D7.

**2026-07-31 — OQ-4: `gpt-5.4` accepts temperature.** The first draft asserted
otherwise, on the strength of community reports about earlier GPT-5 models.
Measured: `temperature` 0, 0.5 and 1 all accepted, with proper range validation
(`400` above 2 and below 0), so it is parsed and honoured rather than tolerated
and ignored. §1.4 was rewritten around measured results, and the correction left
visible.

**2026-07-31 — OQ-5: `claude-local` + temperature is a construction error.** On
the `Stateless` principle that a silently-ignored control is the failure mode
being removed. Accepted cost, deliberately: `claude-local` becomes unusable as a
fallback entry for any config that sets temperature. See D3.

**2026-07-31 — OQ-6: temperature ships, with the shrinkage documented.** It works
across the full 0–2 range on Gemini, which is where the originating measurement
runs, so it unblocks that work now. Its godoc states that support is narrowing
and points at `Effort` as the forward-looking control. See D1.

**2026-07-31 — D4 withdrawn.** The proposed construction-time conflict rule for
`Temperature` + `Effort` on Anthropic has nothing to catch: the two controls never
coexist on the same Anthropic model. See D4.
