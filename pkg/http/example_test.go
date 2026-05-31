package http_test

import (
	"os"
	"time"

	gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func ExampleNewClient() {
	// Create a hardened HTTP client with security defaults.
	client := gtbhttp.NewClient(
		gtbhttp.WithTimeout(10*time.Second),
		gtbhttp.WithMaxRedirects(5),
	)

	_ = client // Use like a standard *http.Client
}

func ExampleNewClient_withRetry() {
	// Create a client with automatic retry for transient failures.
	client := gtbhttp.NewClient(
		gtbhttp.WithTimeout(30*time.Second),
		gtbhttp.WithRetry(gtbhttp.RetryConfig{
			MaxRetries:     3,
			InitialBackoff: 500 * time.Millisecond,
			MaxBackoff:     30 * time.Second,
		}),
	)

	_ = client
}

func ExampleNewClientChain() {
	// Compose client middleware for auth, logging, and rate limiting.
	chain := gtbhttp.NewClientChain(
		gtbhttp.WithRequestLogging(logger.NewNoop()),
		gtbhttp.WithBearerToken(os.Getenv("API_TOKEN")),
		gtbhttp.WithRateLimit(10), // 10 requests per second
	)

	client := gtbhttp.NewClient(
		gtbhttp.WithTimeout(30*time.Second),
		gtbhttp.WithClientMiddleware(chain),
	)

	_ = client // Use like a standard *http.Client
}
