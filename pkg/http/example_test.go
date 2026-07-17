package http_test

import (
	"os"
	"time"

	"gitlab.com/phpboyscout/go/httpclient"
	transithttp "gitlab.com/phpboyscout/go/transit/http"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func ExampleNewClient() {
	// Create a hardened HTTP client with security defaults.
	client := httpclient.NewClient(
		httpclient.WithTimeout(10*time.Second),
		httpclient.WithMaxRedirects(5),
	)

	_ = client // Use like a standard *http.Client
}

func ExampleNewClient_withRetry() {
	// Create a client with automatic retry for transient failures.
	client := httpclient.NewClient(
		httpclient.WithTimeout(30*time.Second),
		httpclient.WithRetry(transithttp.RetryConfig{
			MaxRetries:     3,
			InitialBackoff: 500 * time.Millisecond,
			MaxBackoff:     30 * time.Second,
		}),
	)

	_ = client
}

func ExampleNewClientChain() {
	// Compose client middleware for auth, logging, and rate limiting.
	chain := transithttp.NewClientChain(
		transithttp.WithRequestLogging(logger.ToSlog(logger.NewNoop())),
		transithttp.WithBearerToken(os.Getenv("API_TOKEN")),
		transithttp.WithRateLimit(10), // 10 requests per second
	)

	client := httpclient.NewClient(
		httpclient.WithTimeout(30*time.Second),
		httpclient.WithClientMiddleware(chain),
	)

	_ = client // Use like a standard *http.Client
}
