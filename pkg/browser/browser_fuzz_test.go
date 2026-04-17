package browser_test

import (
	"context"
	"errors"
	"testing"

	"github.com/phpboyscout/go-tool-base/pkg/browser"
)

// FuzzOpenURL feeds random bytes into [browser.OpenURL] and asserts that
// the function never panics. For every fuzz input, [OpenURL] must return
// one of:
//
//   - nil (accepted URL; opener invoked)
//   - a wrapped ErrInvalidURL (hygiene failure)
//   - a wrapped ErrDisallowedScheme (scheme-allowlist failure)
//   - context.Canceled / context.DeadlineExceeded (rare in this test)
//   - the fake opener's error (never in this test because the fake returns nil)
//
// Any other outcome — especially a panic — represents a validation
// defect that the table-driven tests did not cover.
func FuzzOpenURL(f *testing.F) {
	seeds := []string{
		"",
		"https://example.com",
		"http://localhost:8080",
		"mailto:user@example.com",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"data:text/html,hi",
		"://",
		"https:",
		"https:/",
		"https:// ",
		"h%74tps://example.com",
		"https://example.com/\x00",
		"https://example.com/\r\n",
		"HTTPS://EXAMPLE.COM",
		"MAILTO:user@example.com",
		"myapp://callback",
	}

	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, rawURL string) {
		opener := &fakeOpener{}
		err := browser.OpenURL(context.Background(), rawURL, browser.WithOpener(opener.Open))

		if err == nil {
			assertAcceptedOutcome(t, opener, rawURL)
			return
		}

		assertRejectedOutcome(t, err, opener, rawURL)
	})
}

func assertAcceptedOutcome(t *testing.T, opener *fakeOpener, rawURL string) {
	t.Helper()

	if !opener.Invoked() {
		t.Fatalf("OpenURL returned nil but opener was not invoked (input=%q)", rawURL)
	}
	if opener.URL() != rawURL {
		t.Fatalf("opener received %q but OpenURL was called with %q", opener.URL(), rawURL)
	}
}

func assertRejectedOutcome(t *testing.T, err error, opener *fakeOpener, rawURL string) {
	t.Helper()

	switch {
	case errors.Is(err, browser.ErrInvalidURL),
		errors.Is(err, browser.ErrDisallowedScheme):
		if opener.Invoked() {
			t.Fatalf("OpenURL returned a rejection error but opener was still invoked (input=%q, err=%v)", rawURL, err)
		}
	case errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):
		// Not expected with a fresh Background context, but not a bug if it occurs.
	default:
		t.Fatalf("OpenURL returned unexpected error: %v (input=%q)", err, rawURL)
	}
}
