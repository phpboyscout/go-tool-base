package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
)

func TestIsProjectLocalConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		toolName string
		want     bool
	}{
		{"project-local dotfile", "/repo/.mytool.yaml", "mytool", true},
		{"project-local in subdir", "/a/b/.mytool.yaml", "mytool", true},
		{"global config file", "/home/u/.mytool/config.yaml", "mytool", false},
		{"differently named file", "/repo/secrets.yaml", "mytool", false},
		{"empty path", "", "mytool", false},
		{"empty tool", "/repo/.mytool.yaml", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, isProjectLocalConfig(tt.path, tt.toolName))
		})
	}
}

func TestIsSensitiveWrite(t *testing.T) {
	t.Parallel()

	// Constructed so the literal doesn't itself trip gosec G101.
	realToken := "ghp_" + strings.Repeat("a", 36)

	tests := []struct {
		name  string
		key   string
		value string
		want  bool
	}{
		{"known literal-credential key, weak value", "github.auth.value", "abc", true},
		{"known AI key", chat.ConfigKeyClaudeKey, "x", true},
		{"bitbucket app password", "bitbucket.app_password", "x", true},
		{"token-shaped value under any key", "some.random.field", realToken, true},
		{"env reference is not a secret", "github.auth.env", "GITHUB_TOKEN", false},
		{"keychain reference is not a secret", "github.auth.keychain", "tool/github.auth", false},
		{"ordinary key and value", "log.level", "debug", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, isSensitiveWrite(tt.key, tt.value))
		})
	}
}
