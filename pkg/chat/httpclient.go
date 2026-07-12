package chat

import (
	"net/http"
	"time"
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

// chatHTTPClient returns the HTTP client a provider should use: the
// host-injected Config.HTTPClient verbatim when set (go-tool-base injects its
// hardened transport), otherwise the module's own plain bounded default. This
// is the seam that keeps the chat core free of any specific HTTP stack.
func chatHTTPClient(cfg Config) *http.Client {
	if cfg.HTTPClient != nil {
		return cfg.HTTPClient
	}

	return newChatHTTPClient(resolveChatTimeout(cfg))
}

// newChatHTTPClient returns the module-default HTTP client for AI calls: a plain
// stdlib client whose overall and response-header timeouts are raised to the
// given (bounded) value so a slow model's single-shot generation isn't cut off
// at the stdlib default — which otherwise surfaces as "Client.Timeout exceeded
// while awaiting headers". Cancellation is still driven by the request context.
// Hosts that want a hardened transport inject one via Config.HTTPClient.
func newChatHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultChatRequestTimeout
	}

	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{Timeout: timeout}
	}

	t := transport.Clone()
	t.ResponseHeaderTimeout = timeout

	return &http.Client{
		Transport: t,
		Timeout:   timeout,
	}
}
