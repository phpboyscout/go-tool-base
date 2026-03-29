package config_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/phpboyscout/go-tool-base/pkg/cmd/config"
)

func TestNewMasker_DefaultsNotMutatedByOptions(t *testing.T) {
	t.Parallel()

	m1 := config.NewMasker()
	m2 := config.NewMasker(config.WithKeyPattern("custom"))

	// m1 should not have the custom pattern
	assert.False(t, m1.IsSensitive("service.custom", "anything"))
	assert.True(t, m2.IsSensitive("service.custom", "anything"))
}

func TestMasker_IsSensitive_KeyPatterns(t *testing.T) {
	t.Parallel()

	m := config.NewMasker()

	tests := []struct {
		key   string
		value string
		want  bool
	}{
		// leaf matches built-in patterns
		{"ai.claude.key", "somevalue", true},
		{"github.auth.token", "somevalue", true},
		{"db.password", "somevalue", true},
		{"api.secret", "somevalue", true},
		{"service.apikey", "somevalue", true},
		{"github.auth", "somevalue", true},
		// non-sensitive keys
		{"log.level", "info", false},
		{"github.url.api", "https://api.github.com", false},
		{"tool.name", "myapp", false},
		// github.auth.value — leaf is "value", NOT matched by key pattern
		// but may be matched by value pattern if value looks like a token
		{"github.auth.value", "not-a-token", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, m.IsSensitive(tt.key, tt.value))
		})
	}
}

func TestMasker_IsSensitive_KeyPatternsCaseInsensitive(t *testing.T) {
	t.Parallel()

	m := config.NewMasker()

	assert.True(t, m.IsSensitive("api.TOKEN", "val"))
	assert.True(t, m.IsSensitive("api.Password", "val"))
	assert.True(t, m.IsSensitive("api.SECRET", "val"))
}

func TestMasker_IsSensitive_ValuePatterns(t *testing.T) {
	t.Parallel()

	m := config.NewMasker()

	tests := []struct {
		name  string
		key   string
		value string
		want  bool
	}{
		{
			name:  "github classic PAT detected by value",
			key:   "github.auth.value",
			value: "ghp_" + strings.Repeat("A", 36),
			want:  true,
		},
		{
			name:  "github fine-grained PAT detected by value",
			key:   "github.auth.value",
			value: "github_pat_" + strings.Repeat("A", 82),
			want:  true,
		},
		{
			name:  "plain string not detected",
			key:   "github.auth.value",
			value: "plain-config-value",
			want:  false,
		},
		{
			name:  "partial github token prefix not matched",
			key:   "github.auth.value",
			value: "ghp_short",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, m.IsSensitive(tt.key, tt.value))
		})
	}
}

func TestMasker_IsSensitive_CustomKeyPattern(t *testing.T) {
	t.Parallel()

	m := config.NewMasker(config.WithKeyPattern("credential"))

	assert.True(t, m.IsSensitive("vault.mycredential", "val"))
	assert.False(t, m.IsSensitive("vault.endpoint", "val"))
}

func TestMasker_IsSensitive_CustomValuePattern(t *testing.T) {
	t.Parallel()

	re := regexp.MustCompile(`^sk-[A-Za-z0-9]{32}$`)
	m := config.NewMasker(config.WithValuePattern(re))

	assert.True(t, m.IsSensitive("ai.provider.apitoken", "sk-"+strings.Repeat("A", 32)))
	assert.False(t, m.IsSensitive("some.endpoint", "not-a-key"))
}

func TestMasker_Mask(t *testing.T) {
	t.Parallel()

	m := config.NewMasker()

	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"a", "*"},
		{"ab", "**"},
		{"abc", "***"},
		{"abcd", "****"},
		{"abcde", "*bcde"},
		{"hello world", "*******orld"},
		{"ghp_" + strings.Repeat("A", 36), strings.Repeat("*", 36) + "AAAA"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, m.Mask(tt.input))
		})
	}
}

func TestMasker_MaskIfSensitive(t *testing.T) {
	t.Parallel()

	m := config.NewMasker()

	// sensitive key — masked
	result := m.MaskIfSensitive("api.token", "supersecret")
	assert.NotEqual(t, "supersecret", result)
	assert.True(t, strings.HasSuffix(result, "cret"))

	// non-sensitive key with non-token value — not masked
	result = m.MaskIfSensitive("log.level", "info")
	assert.Equal(t, "info", result)

	// non-sensitive key but value looks like github PAT — masked
	result = m.MaskIfSensitive("github.auth.value", "ghp_"+strings.Repeat("B", 36))
	assert.NotEqual(t, "ghp_"+strings.Repeat("B", 36), result)
}
