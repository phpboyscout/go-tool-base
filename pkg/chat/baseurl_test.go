package chat_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/chat"
)

func TestValidateBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		baseURL       string
		allowInsecure bool
		wantErr       bool
	}{
		// Accepted inputs.
		{name: "empty is accepted", baseURL: "", wantErr: false},
		{name: "https api host", baseURL: "https://api.openai.com/v1", wantErr: false},
		{name: "https with port", baseURL: "https://proxy.corp.internal:8443/ai", wantErr: false},
		{name: "https with subdomain", baseURL: "https://eu.api.openai.com", wantErr: false},
		{name: "https with path and query", baseURL: "https://api.example.co/v1?region=us", wantErr: false},
		{name: "localhost via allowInsecure", baseURL: "http://127.0.0.1:8080/v1", allowInsecure: true, wantErr: false},

		// Length rule.
		{name: "over max length", baseURL: "https://" + strings.Repeat("a", chat.MaxBaseURLLength) + ".example.co", wantErr: true},

		// Control characters.
		{name: "contains NUL", baseURL: "https://api.openai.com/\x00/v1", wantErr: true},
		{name: "contains LF", baseURL: "https://api.openai.com/v1\n", wantErr: true},
		{name: "contains DEL", baseURL: "https://api.openai.com/\x7f", wantErr: true},

		// Parse failure.
		{name: "parse failure from invalid bracket", baseURL: "https://[::1", wantErr: true},

		// URL.User rejection. Build the test input at runtime via
		// url.URL so the table does not embed a literal password
		// (which would trip the gosec G101 check).
		{name: "userinfo with password", baseURL: userinfoURL("user", "pass"), wantErr: true},
		{name: "userinfo without password", baseURL: userinfoURL("user", ""), wantErr: true},

		// Scheme rules.
		{name: "http rejected by default", baseURL: "http://api.openai.com/v1", wantErr: true},
		{name: "ftp rejected", baseURL: "ftp://api.openai.com/", wantErr: true},
		{name: "javascript scheme rejected", baseURL: "javascript:alert(1)", wantErr: true},
		{name: "file scheme rejected", baseURL: "file:///etc/passwd", wantErr: true},
		{name: "uppercase https accepted", baseURL: "HTTPS://api.openai.com", wantErr: false},

		// Missing host.
		{name: "scheme only", baseURL: "https://", wantErr: true},

		// Placeholder hosts — spec calls out scaffolding like
		// "https://api.example.com/v1" so subdomains of the reserved
		// placeholder roots must be rejected too.
		{name: "bare example.com rejected", baseURL: "https://example.com", wantErr: true},
		{name: "api.example.com rejected", baseURL: "https://api.example.com/v1", wantErr: true},
		{name: "example.net rejected", baseURL: "https://example.net/v1", wantErr: true},
		{name: "example.org rejected", baseURL: "https://example.org", wantErr: true},
		{name: "localhost.localdomain rejected", baseURL: "https://localhost.localdomain", wantErr: true},
		{name: "uppercase EXAMPLE.COM rejected", baseURL: "https://EXAMPLE.COM/", wantErr: true},
		// Legitimate hosts that happen to contain the substring
		// "example" must NOT be rejected — the check is on full
		// subdomain suffix, not substring.
		{name: "similar but distinct domain accepted", baseURL: "https://example-corp.io/v1", wantErr: false},
		{name: "hostname containing example accepted", baseURL: "https://fooexample.com/v1", wantErr: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := chat.ValidateBaseURL(tc.baseURL, tc.allowInsecure)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, chat.ErrInvalidBaseURL)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateBaseURL_CredentialsNotInErrorString(t *testing.T) {
	t.Parallel()

	// Marker we can grep for in the error string and hint.
	const marker = "SUPER-SECRET-TOKEN-12345"

	input := userinfoURL("user", marker)

	err := chat.ValidateBaseURL(input, false)
	require.Error(t, err)
	require.ErrorIs(t, err, chat.ErrInvalidBaseURL)

	assert.NotContains(t, err.Error(), marker,
		"error must not echo the URL (password is part of userinfo)")
	assert.NotContains(t, errors.FlattenHints(err), marker,
		"hint must not echo the URL (password is part of userinfo)")
}

// userinfoURL builds "https://user[:password]@api.openai.com" without
// a literal string containing credentials — the input that gosec G101
// would otherwise flag as a hardcoded credential.
func userinfoURL(user, password string) string {
	u := url.URL{Scheme: "https", Host: "api.openai.com"}
	if password != "" {
		u.User = url.UserPassword(user, password)
	} else {
		u.User = url.User(user)
	}

	return u.String()
}

func TestValidateBaseURL_HTTPDowngradeRequiresExplicitOptIn(t *testing.T) {
	t.Parallel()

	// Without allowInsecure, HTTP is refused.
	err := chat.ValidateBaseURL("http://localhost:8080", false)
	require.ErrorIs(t, err, chat.ErrInvalidBaseURL)

	// With allowInsecure (test-only), HTTP is accepted.
	err = chat.ValidateBaseURL("http://localhost:8080", true)
	require.NoError(t, err)
}

func TestValidateBaseURL_AllowInsecureDoesNotRelaxOtherRules(t *testing.T) {
	t.Parallel()

	// allowInsecure opens the http door; it must NOT let userinfo,
	// placeholders, or control characters through.
	cases := []string{
		"http://user:pass@localhost:8080",
		"http://example.com",
		"http://localhost/\x00",
		"ftp://localhost/",
	}

	for _, u := range cases {
		t.Run(u, func(t *testing.T) {
			t.Parallel()

			err := chat.ValidateBaseURL(u, true)
			require.Error(t, err)
			require.ErrorIs(t, err, chat.ErrInvalidBaseURL)
		})
	}
}
