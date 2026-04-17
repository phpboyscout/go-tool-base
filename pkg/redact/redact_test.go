package redact_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/redact"
)

func TestString_Golden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		// If contains is non-empty, the output must contain each entry.
		contains []string
		// notContains entries must NOT appear in the output.
		notContains []string
	}{
		// Pass-through cases.
		{
			name:        "empty",
			input:       "",
			notContains: []string{},
		},
		{
			name:        "no credentials",
			input:       "connection refused while dialing 10.0.0.5",
			contains:    []string{"connection refused"},
			notContains: []string{"<redacted"},
		},
		{
			name:        "short hex commit sha is left alone",
			input:       "rebased onto 9e4f359",
			contains:    []string{"9e4f359"},
			notContains: []string{"<redacted"},
		},
		{
			name:        "sha1 git commit left alone",
			input:       "at commit 7a5c45f3b8a9e7d2a1c6b4e3d9f8a7c5b2d4e6f1",
			contains:    []string{"7a5c45f3b8a9e7d2a1c6b4e3d9f8a7c5b2d4e6f1"},
			notContains: []string{"<redacted"},
		},
		{
			name:        "uuid left alone",
			input:       "id=11111111-1111-4111-8111-111111111111",
			contains:    []string{"11111111-1111-4111-8111-111111111111"},
			notContains: []string{"<redacted"},
		},

		// URL userinfo. Userinfo built via concatenation so gosec
		// does not flag literal password-in-URL inputs — these are
		// test data for the redactor, not embedded credentials.
		{
			name:        "https userinfo redacted",
			input:       "GET https://user:" + "sekret@api.example.co/v1/ failed",
			contains:    []string{"https://<redacted>@api.example.co/v1/"},
			notContains: []string{"user:" + "sekret", "sekret"},
		},
		{
			name:        "http userinfo redacted",
			input:       "dial http://admin:" + "hunter2@localhost:9000/ failed",
			contains:    []string{"http://<redacted>@localhost:9000/"},
			notContains: []string{"admin:" + "hunter2", "hunter2"},
		},

		// Query-string credentials.
		{
			name:        "apikey query param",
			input:       "POST /v1?apikey=sk-abc123def456ghi789jkl012mno345pqr678: 401",
			contains:    []string{"apikey=***"},
			notContains: []string{"sk-abc123def456ghi789jkl012mno345pqr678"},
		},
		{
			name:        "access_token query param",
			input:       "callback?access_token=eyJhbGciOiJIUzI1NiJ9.payload.sig",
			contains:    []string{"access_token=***"},
			notContains: []string{"eyJhbGciOiJIUzI1NiJ9.payload.sig"},
		},
		{
			name:        "password query param with ampersand boundary",
			input:       "login?user=bob&password=hunter2&redirect=/home",
			contains:    []string{"password=***", "redirect=/home"},
			notContains: []string{"hunter2"},
		},
		{
			name:     "query cred lowercase only match",
			input:    "APIKEY=foo", // outside word boundary, still caught (case-insensitive)
			contains: []string{"APIKEY=***"},
		},

		// Authorization header in free text.
		{
			name:        "authorization bearer",
			input:       `request rejected with Authorization: Bearer abc123def456ghi789`,
			contains:    []string{"Authorization: Bearer ***"},
			notContains: []string{"abc123def456ghi789"},
		},
		{
			name:        "authorization basic",
			input:       "failed to auth: Authorization: Basic dXNlcjpwYXNz",
			contains:    []string{"Authorization: Basic ***"},
			notContains: []string{"dXNlcjpwYXNz"},
		},

		// Well-known prefixes.
		{
			name:        "openai sk prefix",
			input:       "failed: sk-proj-abc123def456ghi789jkl012mno345",
			contains:    []string{"sk-***"},
			notContains: []string{"sk-proj-abc123def456ghi789jkl012mno345"},
		},
		{
			name:        "github classic pat ghp",
			input:       "token ghp_abcdefghijklmnopqrstuvwxyz012345 rejected",
			contains:    []string{"ghp_***"},
			notContains: []string{"ghp_abcdefghijklmnopqrstuvwxyz012345"},
		},
		{
			name:        "github fine-grained pat",
			input:       "token github_pat_11ABCDEF1234567890abcdef12345678 rejected",
			contains:    []string{"github_pat_***"},
			notContains: []string{"github_pat_11ABCDEF1234567890abcdef12345678"},
		},
		{
			name:        "slack xoxb",
			input:       "slack response 401 for xoxb-1234567890-abcdefghij",
			contains:    []string{"xoxb-***"},
			notContains: []string{"xoxb-1234567890-abcdefghij"},
		},
		{
			// Concatenation avoids gosec G101 flagging the literal.
			// Our redactor keeps the prefix up to the first "-" or
			// "_" for debug readability, so "AIzaSyA-" survives; the
			// critical invariant is that the body after the dash
			// and the body as a whole are both scrubbed.
			name:        "google aiza",
			input:       "403 for " + "AIza" + "SyA-abcDEFghijKLMNopqRSTuvWxYz0123456",
			contains:    []string{"AIza"},
			notContains: []string{"abcDEFghijKLMNopqRSTuvWxYz0123456"},
		},
		{
			// String concatenation avoids gosec G101 flagging the
			// literal — this is a test input for the redactor, not
			// an embedded credential.
			name:        "aws akia",
			input:       "AKIA" + "IOSFODNN7EXAMPLE attempted access",
			contains:    []string{"AKIA***"},
			notContains: []string{"AKIA" + "IOSFODNN7EXAMPLE"},
		},

		// Long opaque fallback — triggered when no earlier-precedence
		// rule already scrubbed the content.
		{
			name:        "41-char opaque in bare context",
			input:       "leaked value is " + strings.Repeat("a", 41),
			contains:    []string{"<redacted-token>"},
			notContains: []string{strings.Repeat("a", 41)},
		},
		{
			name:  "41-char opaque behind token= hits earlier rule",
			input: "token=" + strings.Repeat("a", 41),
			// queryCredPattern fires first — that's more aggressive,
			// not less. Either redaction is acceptable; what matters
			// is the secret is gone.
			contains:    []string{"token=***"},
			notContains: []string{strings.Repeat("a", 41)},
		},
		{
			name:        "40-char hex not redacted (git SHA-1 length)",
			input:       "at " + strings.Repeat("a", 40),
			contains:    []string{strings.Repeat("a", 40)},
			notContains: []string{"<redacted-token>"},
		},

		// Composite inputs — multiple credential shapes in one string.
		// Concatenated so gosec does not flag the literal userinfo URL.
		{
			name:        "composite error message",
			input:       "failed POST https://admin:" + "secret@api.example.co/v1?apikey=sk-abc123def456ghi789: 401",
			contains:    []string{"<redacted>@api.example.co", "apikey=***"},
			notContains: []string{"admin:" + "secret", "sk-abc123def456ghi789"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := redact.String(tc.input)
			for _, want := range tc.contains {
				assert.Contains(t, got, want)
			}

			for _, forbidden := range tc.notContains {
				assert.NotContains(t, got, forbidden)
			}
		})
	}
}

func TestString_Idempotent(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"",
		"no credentials here",
		"https://admin:secret@api.example.co/v1",
		"apikey=sk-proj-abc123def456ghi789jkl012mno345",
		"Authorization: Bearer abc123def456ghi789",
		"ghp_abcdefghijklmnopqrstuvwxyz012345",
		strings.Repeat("a", 50),
	}

	for _, in := range inputs {
		once := redact.String(in)
		twice := redact.String(once)
		assert.Equalf(t, once, twice, "String is not idempotent for %q", in)
	}
}

func TestError(t *testing.T) {
	t.Parallel()

	assert.Empty(t, redact.Error(nil), "nil error should yield empty string")

	err := errors.New("failed GET https://user:pass@api.example.co/v1?apikey=sk-abc123def456ghi789")
	got := redact.Error(err)
	assert.Contains(t, got, "<redacted>@")
	assert.Contains(t, got, "apikey=***")
	assert.NotContains(t, got, "user:pass")
	assert.NotContains(t, got, "sk-abc123def456ghi789")
}

func TestIsSensitiveHeaderKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "empty is not sensitive", key: "", want: false},
		{name: "authorization exact", key: "Authorization", want: true},
		{name: "authorization lowercase", key: "authorization", want: true},
		{name: "proxy-authorization", key: "Proxy-Authorization", want: true},
		{name: "cookie", key: "Cookie", want: true},
		{name: "set-cookie", key: "Set-Cookie", want: true},
		{name: "x-api-key", key: "X-API-Key", want: true},
		{name: "x-auth-token", key: "X-Auth-Token", want: true},

		// Fuzzy matches.
		{name: "custom auth header", key: "X-Custom-Auth", want: true},
		{name: "api-secret", key: "X-API-Secret", want: true},
		{name: "bearer prefix header", key: "X-Bearer-Only", want: true},
		{name: "password in name", key: "X-User-Password", want: true},

		// Should NOT match.
		{name: "content-type", key: "Content-Type", want: false},
		{name: "accept", key: "Accept", want: false},
		{name: "user-agent", key: "User-Agent", want: false},
		{name: "request-id", key: "X-Request-ID", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, redact.IsSensitiveHeaderKey(tc.key))
		})
	}
}

func TestSensitiveHeaderKeys_ContainsCoreSet(t *testing.T) {
	t.Parallel()

	// Guard against accidental deletions from the exported slice —
	// these entries are relied on by external consumers.
	want := []string{"Authorization", "Cookie", "Set-Cookie", "X-API-Key"}
	for _, k := range want {
		require.Contains(t, redact.SensitiveHeaderKeys, k)
	}
}

func BenchmarkString_MixedContent(b *testing.B) {
	// Roughly 4 KiB of mixed content: prose, URL userinfo, query creds,
	// an Authorization header, and two provider-prefixed tokens.
	input := strings.Repeat(
		"log: dialed https://admin:hunter2@api.example.co/v1?apikey=sk-abc123def456ghi789 with Authorization: Bearer ghp_abcdefghijklmnopqrstuvwxyz012345\n",
		30,
	)

	b.ResetTimer()

	for range b.N {
		_ = redact.String(input)
	}
}
