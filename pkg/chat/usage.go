package chat

import "sync"

// Usage reports the token consumption of one or more provider round-trips in a
// provider-neutral shape. Tools built on GTB use it to observe and cost their
// LLM calls without coupling to any one provider's SDK.
//
// Token counts are summed across every provider round-trip made within a single
// Chat, Ask, or StreamChat call — a ReAct tool-calling loop makes one round-trip
// per step, and the usage of every step is accumulated. The Usage() accessor on
// a client returns the cumulative total across the lifetime of that client
// instance; see ChatClient.Usage for details.
//
// Known reports whether the provider supplied token counts. Providers that do
// not expose usage (notably ProviderClaudeLocal, which wraps the claude CLI and
// returns no token data) report a zero-valued Usage with Known == false. Always
// check Known before treating the counts as authoritative.
type Usage struct {
	// InputTokens is the number of prompt/input tokens consumed.
	InputTokens int
	// OutputTokens is the number of completion/output tokens produced.
	OutputTokens int
	// TotalTokens is the sum of input and output tokens. When a provider
	// supplies its own total it is preserved; otherwise it is computed as
	// InputTokens + OutputTokens.
	TotalTokens int
	// CachedTokens is the number of input tokens served from a provider-side
	// prompt cache, when reported. Zero when the provider does not expose it.
	CachedTokens int
	// ReasoningTokens is the number of tokens spent on internal reasoning
	// ("thinking") output, when reported. Zero when not exposed.
	ReasoningTokens int
	// Known reports whether the provider supplied token counts for this call.
	// False indicates the counts are not authoritative (e.g. ProviderClaudeLocal).
	Known bool
}

// Add returns the element-wise sum of two Usage values. A result is Known if
// either operand is Known. TotalTokens is summed directly so a provider-supplied
// total (which may differ from input+output) is preserved across accumulation.
func (u Usage) Add(other Usage) Usage {
	return Usage{
		InputTokens:     u.InputTokens + other.InputTokens,
		OutputTokens:    u.OutputTokens + other.OutputTokens,
		TotalTokens:     u.TotalTokens + other.TotalTokens,
		CachedTokens:    u.CachedTokens + other.CachedTokens,
		ReasoningTokens: u.ReasoningTokens + other.ReasoningTokens,
		Known:           u.Known || other.Known,
	}
}

// newUsage builds a Usage from raw provider counts, computing TotalTokens when
// the provider did not supply one (total <= 0) and marking the result Known.
func newUsage(input, output, total, cached, reasoning int) Usage {
	if total <= 0 {
		total = input + output
	}

	return Usage{
		InputTokens:     input,
		OutputTokens:    output,
		TotalTokens:     total,
		CachedTokens:    cached,
		ReasoningTokens: reasoning,
		Known:           true,
	}
}

// usageTracker accumulates per-round-trip usage for a single client instance and
// fans each round-trip out to an optional observer. It is embedded by every
// provider implementation. The zero value is ready to use.
//
// It is safe for concurrent use, but note that ChatClient implementations are
// not themselves safe for concurrent use; the mutex guards only against an
// observer reading Usage() from another goroutine while a call is in flight.
type usageTracker struct {
	mu       sync.Mutex
	total    Usage
	observer func(Usage)
}

// recordUsage accumulates one round-trip's usage into the lifetime total and, if
// an observer is configured, invokes it with that round-trip's usage. Passing a
// usage with Known == false (e.g. a provider that reports nothing) still fires
// the observer so callers can distinguish "no usage" from "no call".
func (t *usageTracker) recordUsage(u Usage) {
	t.mu.Lock()
	t.total = t.total.Add(u)
	observer := t.observer
	t.mu.Unlock()

	if observer != nil {
		observer(u)
	}
}

// Usage returns the cumulative token usage across every provider round-trip made
// by this client instance since construction. It is promoted onto each provider
// type via embedding to satisfy ChatClient.Usage.
func (t *usageTracker) Usage() Usage {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.total
}
