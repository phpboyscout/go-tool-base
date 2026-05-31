package tls_test

import (
	"fmt"

	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

func ExampleDefaultConfig() {
	// DefaultConfig returns the shared hardened TLS configuration used by the
	// HTTP, gRPC and gateway transports.
	cfg := gtbtls.DefaultConfig()

	fmt.Println("Min TLS version:", cfg.MinVersion)
	fmt.Println("Cipher suites:", len(cfg.CipherSuites))
	// Output:
	// Min TLS version: 771
	// Cipher suites: 6
}
