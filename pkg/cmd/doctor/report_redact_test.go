package doctor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsCredentialKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		want bool
	}{
		{"anthropic.api.key", true},
		{"openai.api.key", true},
		{"github.auth.value", true},
		{"bitbucket.app_password", true},
		{"some.nested.token", true},
		{"x.secret", true},
		{"y.password", true},
		{"authorization", true}, // via redact.IsSensitiveHeaderKey
		{"log.level", false},
		{"service.url", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isCredentialKey(tt.key))
		})
	}
}

func TestRedactValue(t *testing.T) {
	t.Parallel()

	t.Run("credential leaf becomes sentinel", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, redactedSentinel, redactValue("x.token", "anything"))
	})

	t.Run("secret-shaped free-form value is scrubbed", func(t *testing.T) {
		t.Parallel()
		// A non-credential key, but the value carries URL userinfo — redact.String
		// must still strip it (defense-in-depth).
		got := redactValue("notes", "see https://admin:hunter2@example.com")
		assert.NotContains(t, got, "admin:hunter2")
	})

	t.Run("plain scalar is stringified and preserved", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "true", redactValue("feature.enabled", true))
		assert.Equal(t, "info", redactValue("log.level", "info"))
	})

	t.Run("maps recurse and keep keys", func(t *testing.T) {
		t.Parallel()
		in := map[string]any{
			"github": map[string]any{"auth": map[string]any{"value": "ghp_secret"}},
			"log":    map[string]any{"level": "debug"},
		}
		out := redactValue("", in).(map[string]any)
		gh := out["github"].(map[string]any)["auth"].(map[string]any)["value"]
		assert.Equal(t, redactedSentinel, gh)
		assert.Equal(t, "debug", out["log"].(map[string]any)["level"])
	})

	t.Run("slices recurse", func(t *testing.T) {
		t.Parallel()
		out := redactValue("hosts", []any{"a", "b"}).([]any)
		assert.Equal(t, []any{"a", "b"}, out)
	})
}
