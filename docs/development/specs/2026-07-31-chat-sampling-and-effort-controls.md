---
title: "chat: expose sampling and reasoning-effort controls, and say honestly when a model will not take them"
description: "chat.Config offers no way to influence how a model generates: no temperature, no top-p, and no reasoning effort — while Usage.ReasoningTokens already reports what reasoning cost. Adds Temperature, TopP and a provider-neutral Effort ladder. The hard part is not the fields but the support model: unlike Config.Stateless, support is per-model rather than per-provider, so a construction-time capability marker cannot express it. Distinguishes structural gaps (knowable at construction) from model-dependent rejection (only knowable at request time) and gives each its own failure mode."
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
:   DRAFT — pending review

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
sampling control at all:

| Provider | Mechanism | Shape |
|---|---|---|
| `openai` | `ReasoningEffort` | enum: `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max` |
| `gemini` | `ThinkingConfig.ThinkingLevel` | enum: `MINIMAL`, `LOW`, `MEDIUM`, `HIGH`; plus `ThinkingBudget` (`*int32`) |
| `claude` | `Thinking` (`ThinkingConfigParamUnion`) | token budget: `BudgetTokens int64`, or explicitly disabled |
| `claude-local` | `--effort` | enum: `low`, `medium`, `high`, `xhigh`, `max` |

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

### 1.4 The wrinkle: support is per *model*, not per provider

This is the part that shapes the whole design, and the originating report does
not anticipate it.

**`claude-local` cannot ever take sampling parameters.** The claude CLI exposes
no temperature, top-p or top-k flag — checked across the whole of `--help`. This
is not a version lag that an upgrade fixes; there is no such control to plumb.

**The default OpenAI model rejects temperature.** `DefaultModelOpenAI =
"gpt-5.4"` (`constants.go`). The GPT-5 reasoning family rejects any temperature
other than the default, with *"Unsupported parameter: 'temperature' is not
supported with this model"* or *"Only the default (1) value is supported"*. So
`Config{Provider: ProviderOpenAI, Temperature: ptr(0.0)}` — the most obvious
thing a caller would write — fails at **request** time.

**The default Anthropic model constrains it.** Extended thinking is incompatible
with `temperature` and `top_k`; `top_p` is restricted to 0.95–1.
`DefaultModelClaude = "claude-opus-4-8"`.

**Gemini takes it cleanly**, and validates server-side. Measured against the live
API on 2026-07-31:

```
temperature=0     -> 200        temperature=3.0  -> 400  "temperature must be in the range [0.0, 2.0]"
temperature=2.0   -> 200        temperature=-1   -> 400  "temperature must be in the range [0.0, 2.0]"
topP=0.1          -> 200        topP=1.5         -> 400  "top_p must be in the range [0.0, 1.0]"
```

So the same field is: structurally impossible on one provider, rejected by the
default model on another, conditionally invalid on a third, and fully supported
on the fourth. **A per-provider capability marker cannot express that.**

### 1.5 Ranges and types differ

| Provider | Temperature | TopP |
|---|---|---|
| `claude` | 0.0–1.0 | 0.0–1.0 (0.95–1 with thinking) |
| `openai` | 0–2 | 0–1 |
| `gemini` | 0.0–2.0 *(measured)* | 0.0–1.0 *(measured)* |

`genai` uses `float32` where the others use `float64`, so a `*float64` field
needs narrowing for Gemini. `TopK` is not uniform at all — `int64` on Anthropic,
`float32` on Gemini, absent on OpenAI — which is why the report was right to ask
only for temperature and top-p.

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
Temperature *float64

// TopP, when non-nil, overrides the provider's default nucleus-sampling
// threshold. Nil samples at the provider's default.
//
// Providers recommend adjusting temperature or top-p, not both.
TopP *float64
```

Pass-through over a normalised scale, following the report's reasoning: a
normalisation that quietly rescales is less honest than a documented range.

### D2 — `Config.Effort`, a provider-neutral ordinal ladder

```go
// Effort is a provider-neutral reasoning-effort level.
type Effort string

const (
    EffortNone    Effort = "none"    // do not reason at all
    EffortMinimal Effort = "minimal"
    EffortLow     Effort = "low"
    EffortMedium  Effort = "medium"
    EffortHigh    Effort = "high"
    EffortXHigh   Effort = "xhigh"
    EffortMax     Effort = "max"
)
```

An ordinal ladder is genuinely provider-neutral in a way a float range is not:
`low` means the same thing everywhere, whereas `0.5` means different things on a
0–1 scale and a 0–2 one. The ladder mirrors OpenAI's because it is the superset;
see OQ-1 on whether to carry the full seven or the common core.

The mapping each provider applies:

| `Effort` | `openai` | `gemini` | `claude` (budget tokens) | `claude-local` |
|---|---|---|---|---|
| `None` | `none` | `thinkingBudget: 0` | thinking disabled | **unsupported — no way to disable** |
| `Minimal` | `minimal` | `MINIMAL` | 1024 | `low` (clamped up) |
| `Low` | `low` | `LOW` | 4096 | `low` |
| `Medium` | `medium` | `MEDIUM` | 8192 | `medium` |
| `High` | `high` | `HIGH` | 16384 | `high` |
| `XHigh` | `xhigh` | `HIGH` (clamped) | 32768 | `xhigh` |
| `Max` | `max` | `HIGH` (clamped) | 65536 | `max` |

Gemini's levels and the disable path are measured, not assumed —
`thinkingLevel: LOW` produced 158 thought tokens, `HIGH` 176, `thinkingBudget: 0`
exactly 0, and `MAX` is rejected as an invalid level. The Anthropic budget
figures are a proposal, not a measurement; see OQ-2.

Note that `EffortNone` is unsupported on `claude-local` specifically — **a
structural gap at the level of a single enum value, not the whole field.** That
falls out of D3 rather than needing its own rule.

### D3 — Two kinds of "unsupported", each with its own failure mode

The heart of this spec. `Config.Stateless` established a construction-time
capability marker, and reusing it wholesale here would be wrong, because it
models the wrong thing.

**Structural gaps — knowable at construction.** The provider has no such concept
at any version or model: `claude-local` and temperature/top-p; `claude-local` and
`EffortNone`. Nothing the caller does makes these work.

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
supports the parameter, and *this model* refuses it: `gpt-5.4` and temperature;
Anthropic with thinking enabled and temperature. Determining this at construction
would require the core to carry a per-model capability registry, which would be
permanently stale and is not a thing this module should own.

→ **Pass the parameter through and let the provider reject it**, but wrap the
rejection in a sentinel so the caller can tell it apart from every other 4xx:

```go
// ErrSamplingRejected wraps a provider's refusal of a sampling parameter for
// the selected model — as distinct from the provider not supporting sampling
// at all, which is a construction error. Inspect with errors.Is.
var ErrSamplingRejected = errors.New("provider rejected a sampling parameter for this model")
```

The distinction matters because the two demand different fixes: a construction
error means *change provider*, a rejection means *change model or drop the
parameter*. Collapsing them into one error would tell the caller neither.

**What must never happen: silently dropping the parameter.** A request that goes
out without the temperature the caller asked for, and succeeds, reproduces
exactly the failure that made the `scoutdm` measurement worthless — the absence
of a control being indistinguishable from the control having no effect (§1.3).

### D4 — Conflicts are a construction error

Some combinations are invalid per provider, and *that* is knowable up front:
Anthropic cannot take `Temperature` together with any `Effort` above `None`,
because enabling thinking disallows temperature.

`New` rejects the combination, naming both fields. This is neither a structural
gap nor a model-dependent rejection but a third case — a per-provider interaction
rule — and it is cheap to catch before any credential reaches the wire.

### D5 — Validation before the wire

Each provider validates the values it was given against its own documented range
(§1.5) and returns an error before making a request.

Gemini already validates server-side with good messages, so this is not about
correctness — it is about failing in one round-trip fewer, with a consistent
error shape across providers, and without spending a call to learn the caller
typed `2.5` for Claude.

### D6 — `TopK` is out of scope

Not uniform: `int64` on Anthropic, `float32` on Gemini, absent on OpenAI. A
neutral field would be a lie on at least one provider. Revisit if a caller asks.

### D7 — Documentation

- Godoc on all three fields, stating the per-provider ranges and that
  `claude-local` takes no sampling parameters.
- New `docs/how-to/control-sampling.md`: what temperature and effort do, what
  pinning buys and what it does not (§1.6), the effort mapping table, and the two
  failure modes from D3.
- The `Config` reference table in `docs/how-to/configure-a-provider.md`.
- `docs/explanation/providers.md` gains a capability column, since this is now
  the second axis on which providers differ.

## 3. Scope & release plan

| Repo | Change | Release |
|---|---|---|
| `go/chat` | `Temperature`, `TopP`, `Effort` + the `Effort` constants, `SamplingCapable`, `EffortCapable`, `ErrSamplingRejected`, `New` guards (D3/D4), `claude-local` `--effort`, docs | `feat:` minor |
| `go/chat-anthropic` | temperature/top-p, `Thinking` budget mapping, both markers, conflict rule | `feat:` minor |
| `go/chat-openai` | temperature/top-p, `ReasoningEffort`, both markers, rejection wrapping | `feat:` minor |
| `go/chat-gemini` | temperature/top-p (narrowed to `float32`), `ThinkingLevel`, both markers | `feat:` minor |

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
   backend.

**The two failure modes stay distinct**

4. `chat.New` with `Temperature` set and `Provider: ProviderClaudeLocal` returns
   a construction error naming the provider — it can never carry it.
5. `chat.New` with `Effort: EffortNone` and `ProviderClaudeLocal` returns a
   construction error; every other `Effort` value constructs and passes `--effort`.
6. A provider rejecting a parameter at request time returns an error satisfying
   `errors.Is(err, ErrSamplingRejected)`, and the underlying provider error is
   still reachable by unwrapping.
7. **No silent drops.** For every provider, a request built from a `Config` with
   a sampling parameter set either carries it or fails; a test asserts no
   request body omits a set parameter while returning success.

**Conflicts and validation**

8. `chat.New` with `Temperature` and `Effort > None` on `ProviderClaude` returns
   a construction error naming both fields.
9. Out-of-range values fail before any HTTP request: Claude `Temperature: 1.5`,
   OpenAI `Temperature: 2.5`, any provider `TopP: 1.5`. Asserted with a backend
   that fails the test if it is called at all.

**Everything else**

10. Existing suites pass untouched across all four modules; a `Config` with none
    of the new fields set produces identical requests.
11. Gemini narrowing to `float32` round-trips the values a caller can express
    without surprising precision loss.
12. Docs from D7 land with the core release; CI green (tests, race, lint) and
    coverage does not regress.

## 5. Open questions

**OQ-1 — Full seven-rung ladder, or the common core?** D2 mirrors OpenAI's
`none…max` because it is the superset, but that makes the neutral API look like
one vendor's, and two rungs (`xhigh`, `max`) clamp to `HIGH` on Gemini so the
distinction silently vanishes there. The alternative is `none/low/medium/high` —
honest everywhere, at the cost of a caller on OpenAI or claude-local being unable
to reach `xhigh`/`max` through this module at all. Recommendation: ship the seven
and document the clamping, because losing reach on two providers is worse than a
ladder that is uneven at the top; but this is a genuine API-shape decision.

**OQ-2 — What budget-token values map to each effort rung on Anthropic?** The
figures in D2 are proposed, not measured. Anthropic takes a token budget rather
than a level, so the mapping is ours to choose and will look arbitrary unless it
is calibrated. Worth one measured pass — run a fixed prompt at each budget and
record actual thinking-token usage — so the table has the same standing as the
Gemini row, which was measured. Until then the numbers should be treated as
placeholders.

**OQ-3 — Is `Seed` already dead on the default OpenAI model?** If `gpt-5.4`
rejects `temperature`, it may reject `seed` too, which would make the existing
field not merely partial (as its godoc says) but inert on the default model —
worse than absent, because it reads as working. Needs one call with a real key.
If it is dead, this spec should say so in the `Seed` godoc rather than leaving a
field that quietly does nothing.

**OQ-4 — Does `gpt-5.4` specifically reject non-default temperature?** §1.4 rests
on documented behaviour for the GPT-5 reasoning family; `gpt-5.4` was not tested
directly, as no OpenAI key was available. The design does not depend on the
answer — D3 handles rejection generically — but the problem statement should not
assert an untested specific.

**OQ-5 — Should `claude-local` sampling be a construction error, or a documented
no-op?** D3 says error, on the `Stateless` principle that silence is the failure
mode being removed. The counter-argument is ergonomic: a caller writing
provider-agnostic code with `Temperature` set would find `claude-local` newly
unusable, where today it merely ignores what it cannot do. Recommendation: keep
the error, since §1.3 is a direct account of what silent no-ops cost — but note
that this makes `claude-local` unusable as a fallback entry for any config that
sets temperature, which is a real ergonomic cost worth accepting deliberately
rather than discovering.

## 6. Resolved

*(Nothing yet — this spec is DRAFT. Resolutions from review land here, dated.)*
