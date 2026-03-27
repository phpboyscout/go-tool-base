package http

import (
	"crypto/rand"
	"io"
	"math/big"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
)

const (
	defaultMaxRetries     = 3
	defaultInitialBackoff = 500 * time.Millisecond
	defaultMaxBackoff     = 30 * time.Second
	maxBackoffShift       = 62
)

// RetryConfig configures the retry behaviour of the HTTP client.
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts. Zero means no retries.
	MaxRetries int
	// InitialBackoff is the base delay before the first retry. Default: 500ms.
	InitialBackoff time.Duration
	// MaxBackoff caps the computed delay. Default: 30s.
	MaxBackoff time.Duration
	// RetryableStatusCodes defines which HTTP status codes trigger a retry.
	// Default: []int{429, 502, 503, 504}.
	RetryableStatusCodes []int
	// ShouldRetry is an optional custom predicate. When set, it replaces the
	// default status-code and network-error checks. The attempt count (0-based)
	// and either the response or the transport error are provided.
	ShouldRetry func(attempt int, resp *http.Response, err error) bool
}

// DefaultRetryConfig returns a RetryConfig suitable for most use cases.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:           defaultMaxRetries,
		InitialBackoff:       defaultInitialBackoff,
		MaxBackoff:           defaultMaxBackoff,
		RetryableStatusCodes: []int{http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout},
	}
}

// WithRetry enables automatic retry with exponential backoff for transient failures.
func WithRetry(cfg RetryConfig) ClientOption {
	return func(c *clientConfig) {
		c.retry = &cfg
	}
}

// retryTransport wraps an http.RoundTripper with retry logic using exponential
// backoff and full jitter. Request bodies are rewound via GetBody between attempts.
// Response bodies from failed attempts are drained and closed to allow connection reuse.
type retryTransport struct {
	next http.RoundTripper
	cfg  RetryConfig
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var (
		resp *http.Response
		err  error
	)

	for attempt := range t.cfg.MaxRetries + 1 {
		if attempt > 0 {
			delay := t.computeDelay(attempt, resp)

			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(delay):
			}

			// Reset request body for retry
			if req.GetBody != nil {
				req.Body, err = req.GetBody()
				if err != nil {
					return nil, errors.Wrap(err, "failed to reset request body for retry")
				}
			}
		}

		resp, err = t.next.RoundTrip(req)
		if !t.shouldRetry(attempt, resp, err) {
			break
		}

		// Drain and close response body before retry to reuse connection
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}

	return resp, err
}

// shouldRetry determines whether the request should be retried.
func (t *retryTransport) shouldRetry(attempt int, resp *http.Response, err error) bool {
	if attempt >= t.cfg.MaxRetries {
		return false
	}

	if t.cfg.ShouldRetry != nil {
		return t.cfg.ShouldRetry(attempt, resp, err)
	}

	return defaultShouldRetry(t.cfg.RetryableStatusCodes, resp, err)
}

// defaultShouldRetry checks for retryable status codes and transient network errors.
func defaultShouldRetry(codes []int, resp *http.Response, err error) bool {
	if err != nil {
		return isTransientNetworkError(err)
	}

	if resp != nil {
		return slices.Contains(codes, resp.StatusCode)
	}

	return false
}

// isTransientNetworkError returns true for network errors that are likely transient.
func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}

	// Check for timeout errors
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// Check for connection reset/refused
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}

	// Check for common transient error strings as a fallback
	msg := err.Error()

	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "EOF")
}

// computeDelay calculates the backoff delay for a retry attempt using
// exponential backoff with full jitter: uniform random in [0, min(cap, base * 2^attempt)].
// If the previous response included a Retry-After header, that value is used as the delay floor.
func (t *retryTransport) computeDelay(attempt int, resp *http.Response) time.Duration {
	// Check Retry-After header
	if resp != nil {
		if ra := parseRetryAfter(resp.Header.Get("Retry-After")); ra > 0 {
			return ra
		}
	}

	backoff := t.cfg.InitialBackoff
	if backoff == 0 {
		backoff = defaultInitialBackoff
	}

	maxBackoff := t.cfg.MaxBackoff
	if maxBackoff == 0 {
		maxBackoff = defaultMaxBackoff
	}

	// Exponential backoff: base * 2^(attempt-1)
	shift := min(uint(max(attempt, 1))-1, maxBackoffShift)
	backoff *= 1 << shift

	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	// Full jitter: uniform random in [0, backoff]
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(backoff)+1))

	return time.Duration(n.Int64())
}

// parseRetryAfter parses the Retry-After header value, supporting both
// delay-seconds and HTTP-date formats.
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}

	// Try as seconds first
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Duration(seconds) * time.Second
	}

	// Try as HTTP-date
	if t, err := http.ParseTime(value); err == nil {
		delay := time.Until(t)
		if delay > 0 {
			return delay
		}
	}

	return 0
}
