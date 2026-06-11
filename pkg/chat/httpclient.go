package chat

import (
	"net/http"
	"time"

	gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
)

// DefaultChatRequestTimeout bounds a single AI request. LLM generations —
// especially a large single-shot code conversion on a slower flagship model
// like Opus — run well past the shared 30s HTTP default, so chat clients use a
// more generous cap. It is deliberately bounded (not unlimited): a model stuck
// in a loop or never returning must still fail rather than hang forever.
// Override per-environment with the ai.request_timeout config key (e.g. "8m")
// or programmatically via Config.RequestTimeout.
const DefaultChatRequestTimeout = 5 * time.Minute

// resolveChatTimeout picks the effective per-request timeout: an explicit
// Config.RequestTimeout (set from the ai.request_timeout config key by the
// caller) wins, else the sane default. (A future refinement could vary the
// default per model.)
func resolveChatTimeout(cfg Config) time.Duration {
	if cfg.RequestTimeout > 0 {
		return cfg.RequestTimeout
	}

	return DefaultChatRequestTimeout
}

// newChatHTTPClient returns an HTTP client tuned for AI calls: the hardened gtb
// transport, but with the overall and response-header timeouts raised to the
// given (bounded) value so a slow model's single-shot generation isn't cut off
// at 30s — which otherwise surfaces as "Client.Timeout exceeded while awaiting
// headers" and a silent fall back to placeholder code. Cancellation is still
// driven by the request context.
func newChatHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultChatRequestTimeout
	}

	transport := gtbhttp.NewTransport(nil)
	transport.ResponseHeaderTimeout = timeout

	return gtbhttp.NewClient(
		gtbhttp.WithTransport(transport),
		gtbhttp.WithTimeout(timeout),
	)
}
