package features

import "context"

// EvalContext is who is asking: a targeting key and attributes, the shape
// every flag system and the OpenFeature specification use. Empty for a
// static answer.
type EvalContext struct {
	TargetingKey string
	Attributes   map[string]any
}

// Reason says how a Decision was reached.
type Reason string

const (
	// ReasonStatic is a static-only feature, or a Set, answering from the
	// resolved state.
	ReasonStatic Reason = "static"
	// ReasonDefault is a backend answering with its default rule.
	ReasonDefault Reason = "default"
	// ReasonTargeting is a backend matching the EvalContext.
	ReasonTargeting Reason = "targeting"
	// ReasonDisabled is a backend that has the flag disabled: the fallback
	// (the Set state) applies, and that is a clean answer.
	ReasonDisabled Reason = "disabled"
	// ReasonFallback is a backend error or a backend not ready: the Set
	// answered, and the error travels alongside.
	ReasonFallback Reason = "fallback"
	// ReasonError is no answer possible (an ID nobody declared).
	ReasonError Reason = "error"
)

// Decision is an Evaluator's answer.
type Decision struct {
	Enabled bool
	Reason  Reason
	// Variant is empty for a plain toggle; a multivariate backend fills it.
	Variant string
}

// Evaluator answers whether a feature is on for this request. A Set is one;
// a Dynamic evaluator over a Backend is the other (spec 0199 D10).
type Evaluator interface {
	Evaluate(ctx context.Context, id ID, ec EvalContext) (Decision, error)
}
