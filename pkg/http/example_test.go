package http_test

import (
	"fmt"
	"time"

	gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
)

func ExampleNewClient() {
	// Create a hardened HTTP client with security defaults.
	client := gtbhttp.NewClient(
		gtbhttp.WithTimeout(10 * time.Second),
		gtbhttp.WithMaxRedirects(5),
	)

	_ = client // Use like a standard *http.Client
}

func ExampleNewClient_withRetry() {
	// Create a client with automatic retry for transient failures.
	client := gtbhttp.NewClient(
		gtbhttp.WithTimeout(30 * time.Second),
		gtbhttp.WithRetry(gtbhttp.RetryConfig{
			MaxRetries:     3,
			InitialBackoff: 500 * time.Millisecond,
			MaxBackoff:     30 * time.Second,
		}),
	)

	_ = client
}

func ExampleDefaultTLSConfig() {
	// DefaultTLSConfig returns the shared hardened TLS configuration
	// used by both HTTP and gRPC servers/clients.
	cfg := gtbhttp.DefaultTLSConfig()

	fmt.Println("Min TLS version:", cfg.MinVersion)
	fmt.Println("Cipher suites:", len(cfg.CipherSuites))
	// Output:
	// Min TLS version: 771
	// Cipher suites: 6
}
