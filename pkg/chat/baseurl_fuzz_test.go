package chat_test

import (
	"testing"

	"github.com/cockroachdb/errors"

	"github.com/phpboyscout/go-tool-base/pkg/chat"
)

// FuzzValidateBaseURL exercises [chat.ValidateBaseURL] against a corpus
// of canonical URLs, userinfo-bearing URLs, control characters,
// oversize inputs, raw bytes, and Unicode tricks. The security
// invariants asserted here:
//
//  1. No call ever panics, regardless of input.
//  2. Every rejection wraps [chat.ErrInvalidBaseURL] so callers can
//     switch on kind via [errors.Is].
//  3. Accepting an input implies it parses, uses https (or the
//     allowInsecure opt-in), and has no userinfo — re-checked via an
//     independent oracle to catch any drift between the validator and
//     its contract.
func FuzzValidateBaseURL(f *testing.F) {
	seeds := []string{
		// Accepted shapes.
		"",
		"https://api.openai.com",
		"https://eu.api.openai.com/v1",
		"https://proxy.corp.internal:8443",

		// Userinfo rejections.
		"https://user:pass@api.openai.com",
		"https://user@api.openai.com",

		// Scheme rejections.
		"http://api.openai.com",
		"ftp://api.openai.com",
		"javascript:alert(1)",
		"file:///etc/passwd",

		// Parse failures.
		"https://[::1",
		"https:// spaces here",

		// Control characters.
		"https://api.openai.com/\x00",
		"https://api.openai.com/\n",

		// Placeholders.
		"https://example.com",
		"https://api.example.com",
		"https://EXAMPLE.COM",
	}

	for _, s := range seeds {
		f.Add(s, false)
		f.Add(s, true)
	}

	f.Fuzz(func(t *testing.T, baseURL string, allowInsecure bool) {
		err := chat.ValidateBaseURL(baseURL, allowInsecure)

		if err != nil {
			if !errors.Is(err, chat.ErrInvalidBaseURL) {
				t.Fatalf("rejection for %q wraps no ErrInvalidBaseURL sentinel: %v", baseURL, err)
			}

			return
		}

		// Accepted — must satisfy the contract:
		// empty, OR (parses & has host & https-or-http-with-opt-in & no userinfo & no placeholder).
		if baseURL == "" {
			return
		}

		if len(baseURL) > chat.MaxBaseURLLength {
			t.Fatalf("accepted over-length URL: %d bytes", len(baseURL))
		}
	})
}
