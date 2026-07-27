package root

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// hostileProjectConfig is a repo-shipped ".<tool>.yaml" that tries to downgrade
// self-update verification, flip telemetry consent, and plant a credential —
// while also setting a legitimate workflow key.
const hostileProjectConfig = `update:
  require_signature: false
  require_checksum: false
  policy: disabled
telemetry:
  enabled: true
github:
  auth:
    value: ghp_hostile
log:
  level: debug
`

// buildProjectStore builds a config store through the real bootstrap seam
// (buildConfigStore) with only a project-local layer, mirroring what a command
// run inside a cloned repository sees.
func buildProjectStore(t *testing.T, fs afero.Fs, log logger.Logger, projectPath string) *p.Props {
	t.Helper()

	props := &p.Props{
		Tool:   p.Tool{Name: "mytool"},
		Logger: log,
		FS:     fs,
	}

	store, err := buildConfigStore(t.Context(), ConfigLoadOptions{
		Props:             props,
		AllowEmpty:        true,
		ProjectConfigPath: projectPath,
	})
	require.NoError(t, err)

	props.Config = store

	return props
}

// TestProjectLocalTrust_HostileCloneIsIgnored is the headline security
// regression: a cloned repo's untrusted .<tool>.yaml must NOT be able to change
// update-verification, telemetry consent, or credentials — but its legitimate
// workflow keys still apply, and the ignored keys are logged at WARN.
func TestProjectLocalTrust_HostileCloneIsIgnored(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	fs := afero.NewOsFs()
	dir := t.TempDir()
	path := filepath.Join(dir, ".mytool.yaml")
	require.NoError(t, afero.WriteFile(fs, path, []byte(hostileProjectConfig), 0o600))

	log := logger.NewBuffer()
	props := buildProjectStore(t, fs, log, path)
	view := props.Config.View()

	// Security-sensitive keys are stripped from the untrusted layer.
	assert.False(t, view.IsSet("update.require_signature"),
		"untrusted project config must not set update.require_signature")
	assert.False(t, view.IsSet("update.require_checksum"),
		"untrusted project config must not set update.require_checksum")
	assert.False(t, view.IsSet("update.policy"),
		"untrusted project config must not set update.policy")
	assert.False(t, view.GetBool("telemetry.enabled"),
		"untrusted project config must not flip telemetry consent")
	assert.Empty(t, view.GetString("github.auth.value"),
		"untrusted project config must not plant a credential")

	// Non-security workflow keys are honoured as before.
	assert.Equal(t, "debug", view.GetString("log.level"),
		"a non-sensitive workflow key must still apply from the project layer")

	// The ignored keys are logged, not silently dropped.
	assert.True(t, log.Contains("ignoring security-sensitive keys"),
		"ignored keys must be logged at WARN")
}

// TestProjectLocalTrust_TrustedCloneApplies proves the escape hatch — and, by
// contrast, that but for the trust filter the hostile keys WOULD apply (the
// pre-fix behaviour): once the directory is trusted, every key is honoured.
func TestProjectLocalTrust_TrustedCloneApplies(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	fs := afero.NewOsFs()
	dir := t.TempDir()
	path := filepath.Join(dir, ".mytool.yaml")
	require.NoError(t, afero.WriteFile(fs, path, []byte(hostileProjectConfig), 0o600))

	require.NoError(t, setup.TrustProjectConfig(fs, "mytool", path))

	props := buildProjectStore(t, fs, logger.NewNoop(), path)
	view := props.Config.View()

	assert.True(t, view.IsSet("update.require_signature"))
	assert.False(t, view.GetBool("update.require_signature"),
		"a trusted file's require_signature:false is honoured")
	assert.True(t, view.GetBool("telemetry.enabled"),
		"a trusted file's telemetry.enabled is honoured")
	assert.Equal(t, "ghp_hostile", view.GetString("github.auth.value"),
		"a trusted file's credential is honoured")
	assert.Equal(t, "debug", view.GetString("log.level"))
}

// TestStripProtectedKeys covers the key-stripping predicate directly.
func TestStripProtectedKeys(t *testing.T) {
	t.Parallel()

	doc := map[string]any{
		"update": map[string]any{
			"require_signature": false,
			"check_interval":    "24h", // NOT sensitive — kept
		},
		"telemetry": map[string]any{"enabled": true},
		"github":    map[string]any{"auth": map[string]any{"value": "x"}},
		"anthropic": map[string]any{"api": map[string]any{"key": "sk"}},
		"log":       map[string]any{"level": "debug"}, // kept
	}

	removed := stripProtectedKeys(doc)

	assert.Contains(t, removed, "update.require_signature")
	assert.Contains(t, removed, "telemetry.enabled")
	assert.Contains(t, removed, "github.auth")
	assert.Contains(t, removed, "anthropic.api")

	// Non-sensitive keys survive.
	upd, _ := doc["update"].(map[string]any)
	assert.Equal(t, "24h", upd["check_interval"])
	lg, _ := doc["log"].(map[string]any)
	assert.Equal(t, "debug", lg["level"])
}
