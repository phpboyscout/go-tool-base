package chat

import (
	"context"
	"net"
	"net/http"

	"github.com/cockroachdb/errors"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go/v3"
	"google.golang.org/genai"
)

// FailoverDecision is the outcome of classifying a provider error.
type FailoverDecision int

const (
	// FailoverFatal — do not advance; return the error to the caller.
	FailoverFatal FailoverDecision = iota
	// FailoverNext — the active provider failed transiently or is
	// unavailable; advance to the next provider.
	FailoverNext
)

// FailoverPolicy classifies a provider error into a [FailoverDecision].
//
// Implementations MUST NOT log the error's message directly. A policy only
// decides whether to advance; the composite logs a single coarse WARN per
// transition (provider names + a "status"/"network" reason, never the raw
// error), and reduces any endpoint detail to the host only.
type FailoverPolicy interface {
	Classify(err error) FailoverDecision
}

// DefaultFailoverPolicy advances on transient/unavailable conditions (HTTP 408,
// 429, 5xx, and network errors) and treats everything else — auth, bad request,
// unknown model, caller cancellation, and local-CLI (claude-local) failures —
// as fatal so an operator-fixable problem surfaces instead of being masked.
//
// See the resolved open questions in
// docs/development/specs/2026-06-21-chat-provider-fallback.md (OQ-1: a
// claude-local non-zero exit is fatal).
var DefaultFailoverPolicy FailoverPolicy = defaultFailoverPolicy{}

type defaultFailoverPolicy struct{}

func (defaultFailoverPolicy) Classify(err error) FailoverDecision {
	if err == nil {
		return FailoverFatal
	}

	// The caller asked to stop — respect it, never advance.
	if errors.Is(err, context.Canceled) {
		return FailoverFatal
	}

	// Provider HTTP status drives the primary decision.
	if status, ok := providerHTTPStatus(err); ok {
		switch status {
		case http.StatusRequestTimeout, // 408
			http.StatusTooManyRequests,     // 429
			http.StatusInternalServerError, // 500
			http.StatusBadGateway,          // 502
			http.StatusServiceUnavailable,  // 503
			http.StatusGatewayTimeout:      // 504
			return FailoverNext
		default:
			// 400/401/403/404/422 etc. fail identically everywhere.
			return FailoverFatal
		}
	}

	// A per-request timeout (the call's own deadline, not the caller's — the
	// composite guards caller cancellation separately) is transient.
	if errors.Is(err, context.DeadlineExceeded) {
		return FailoverNext
	}

	// Network unreachable: DNS failure, connection refused/reset, TLS handshake.
	var netOpErr *net.OpError
	if errors.As(err, &netOpErr) {
		return FailoverNext
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return FailoverNext
	}

	// Everything else — including *exec.ExitError from claude-local (OQ-1) and
	// any unrecognised error — is fatal: do not mask an operator-fixable fault.
	return FailoverFatal
}

// providerHTTPStatus extracts an HTTP status code from a provider SDK error,
// unwrapping the cockroachdb/errors layers the providers add. It knows each
// SDK's status-bearing error type. The second return is false when no provider
// status could be found.
func providerHTTPStatus(err error) (int, bool) {
	var anthropicErr *anthropic.Error
	if errors.As(err, &anthropicErr) {
		return anthropicErr.StatusCode, true
	}

	var openaiErr *openai.Error
	if errors.As(err, &openaiErr) {
		return openaiErr.StatusCode, true
	}

	var geminiErr *genai.APIError
	if errors.As(err, &geminiErr) {
		return geminiErr.Code, true
	}

	return 0, false
}
