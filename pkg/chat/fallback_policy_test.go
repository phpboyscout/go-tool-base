package chat

import (
	"context"
	"net"
	"os/exec"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go/v3"
	"google.golang.org/genai"
)

func TestProviderHTTPStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantOK     bool
	}{
		{"anthropic 429", &anthropic.Error{StatusCode: 429}, 429, true},
		{"openai 401", &openai.Error{StatusCode: 401}, 401, true},
		{"gemini 503 (Code field)", &genai.APIError{Code: 503}, 503, true},
		{"wrapped anthropic still unwraps", errors.Wrap(&anthropic.Error{StatusCode: 500}, "call failed"), 500, true},
		{"non-provider error", errors.New("boom"), 0, false},
		{"nil", nil, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			status, ok := providerHTTPStatus(tt.err)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantStatus, status)
		})
	}
}

func TestDefaultPolicy_Classify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want FailoverDecision
	}{
		// Retryable — advance.
		{"429 rate limit", &anthropic.Error{StatusCode: 429}, FailoverNext},
		{"500 server error", &openai.Error{StatusCode: 500}, FailoverNext},
		{"502 bad gateway", &genai.APIError{Code: 502}, FailoverNext},
		{"503 unavailable", &anthropic.Error{StatusCode: 503}, FailoverNext},
		{"504 gateway timeout", &openai.Error{StatusCode: 504}, FailoverNext},
		{"408 request timeout", &anthropic.Error{StatusCode: 408}, FailoverNext},
		{"wrapped 503 advances", errors.Wrap(&genai.APIError{Code: 503}, "gemini send"), FailoverNext},
		{"per-request deadline", context.DeadlineExceeded, FailoverNext},
		{"net.OpError (connection refused)", &net.OpError{Op: "dial", Err: errors.New("refused")}, FailoverNext},
		{"net.DNSError", &net.DNSError{Err: "no such host", Name: "api.example.com"}, FailoverNext},

		// Fatal — do not advance.
		{"400 bad request", &openai.Error{StatusCode: 400}, FailoverFatal},
		{"401 auth", &anthropic.Error{StatusCode: 401}, FailoverFatal},
		{"403 forbidden", &openai.Error{StatusCode: 403}, FailoverFatal},
		{"404 unknown model", &genai.APIError{Code: 404}, FailoverFatal},
		{"422 unprocessable", &openai.Error{StatusCode: 422}, FailoverFatal},
		{"caller cancelled", context.Canceled, FailoverFatal},
		{"claude-local exit (OQ-1)", &exec.ExitError{}, FailoverFatal},
		{"unrecognised error", errors.New("mystery"), FailoverFatal},
		{"nil", nil, FailoverFatal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, DefaultFailoverPolicy.Classify(tt.err))
		})
	}
}
