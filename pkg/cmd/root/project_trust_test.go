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
		"azure":     map[string]any{"api": map[string]any{"key": "az"}},
		"log":       map[string]any{"level": "debug"}, // kept
		"config":    map[string]any{"sources": map[string]any{"team": map[string]any{"address": "x"}}},
	}

	removed := stripProtectedKeys(doc)

	assert.Contains(t, removed, "config.sources", "every source pointer is protected (spec 0204 D5)")

	assert.Contains(t, removed, "update.require_signature")
	assert.Contains(t, removed, "telemetry.enabled")
	assert.Contains(t, removed, "github.auth")
	assert.Contains(t, removed, "anthropic.api")
	assert.Contains(t, removed, "azure.api", "every chat credential root is protected (spec 0196 D6)")

	// Non-sensitive keys survive.
	upd, _ := doc["update"].(map[string]any)
	assert.Equal(t, "24h", upd["check_interval"])
	lg, _ := doc["log"].(map[string]any)
	assert.Equal(t, "debug", lg["level"])
}

// TestProjectLocalTrust_ProtectedKeysMatchInAnyCase pins the strip to the same
// folding the store applies, so no spelling of a protected key reaches a reader.
func TestProjectLocalTrust_ProtectedKeysMatchInAnyCase(t *testing.T) {
	cases := []struct {
		name, file, key string
	}{
		{"upper", "UPDATE:\n  REQUIRE_SIGNATURE: false\n", "update.require_signature"},
		{"mixed", "Update:\n  Require_Signature: false\n", "update.require_signature"},
		{"parent only", "UPDATE:\n  require_signature: false\n", "update.require_signature"},
		{"leaf only", "update:\n  REQUIRE_SIGNATURE: false\n", "update.require_signature"},
		{"non-ascii folding", "update:\n  Key_source: wkd\n", "update.key_source"},
		{"telemetry", "TELEMETRY:\n  ENABLED: true\n", "telemetry.enabled"},
		{"credential segment", "github:\n  AUTH:\n    value: x\n", "github.auth.value"},
		{"credential root", "Anthropic:\n  API:\n    key: x\n", "anthropic.api.key"},
		{"dual credential", "BITBUCKET:\n  APP_PASSWORD: x\n", "bitbucket.app_password"},
		{"config sources", "CONFIG:\n  SOURCES:\n    team:\n      address: x\n", "config.sources.team.address"},
		{"nested", "tools:\n  deep:\n    Auth:\n      value: x\n", "tools.deep.auth.value"},
		{"non-string sibling", "github:\n  1: x\n  auth:\n    value: x\n", "github.auth.value"},
		{"non-string sibling, nested", "tools:\n  1: x\n  deep:\n    AUTH:\n      value: x\n", "tools.deep.auth.value"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())

			fs := afero.NewOsFs()
			path := filepath.Join(t.TempDir(), ".mytool.yaml")
			require.NoError(t, afero.WriteFile(fs, path, []byte(tc.file+"log:\n  level: debug\n"), 0o600))

			log := logger.NewBuffer()
			view := buildProjectStore(t, fs, log, path).Config.View()

			assert.False(t, view.IsSet(tc.key), "%s must not reach the store", tc.key)
			assert.Equal(t, "debug", view.GetString("log.level"), "the file's other keys still apply")
			assert.True(t, log.Contains("ignoring security-sensitive keys"))
		})
	}
}

// TestProjectLocalTrust_ExactKeyCaseStripsEverySpelling covers a tool whose store
// does not fold: it reads only one spelling, but the strip still takes them all,
// and a file that is trusted keeps them.
func TestProjectLocalTrust_ExactKeyCaseStripsEverySpelling(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	fs := afero.NewOsFs()
	path := filepath.Join(t.TempDir(), ".mytool.yaml")
	require.NoError(t, afero.WriteFile(fs, path,
		[]byte("update:\n  require_signature: false\n  Require_Signature: false\nlog:\n  Level: debug\n"), 0o600))

	build := func(log logger.Logger) *p.Props {
		props := &p.Props{Tool: p.Tool{Name: "mytool", Config: p.ConfigSpec{KeyCase: p.KeyCaseExact}}, Logger: log, FS: fs}

		store, err := buildConfigStore(t.Context(), ConfigLoadOptions{Props: props, AllowEmpty: true, ProjectConfigPath: path})
		require.NoError(t, err)

		props.Config = store

		return props
	}

	log := logger.NewBuffer()
	view := build(log).Config.View()

	assert.False(t, view.IsSet("update.require_signature"))
	assert.False(t, view.IsSet("update.Require_Signature"))
	assert.Equal(t, "debug", view.GetString("log.Level"), "an unprotected key keeps its exact spelling")
	assert.True(t, log.Contains("ignoring security-sensitive keys"))

	require.NoError(t, setup.TrustProjectConfig(fs, "mytool", path))
	assert.True(t, build(logger.NewNoop()).Config.View().IsSet("update.Require_Signature"))
}

// TestProjectLocalTrust_SourcePointersMatchInAnyCase holds a trusted file to the
// same matching: trust never admits config.sources, however it is spelt.
func TestProjectLocalTrust_SourcePointersMatchInAnyCase(t *testing.T) {
	cases := map[string]string{
		"upper":              "CONFIG:\n  SOURCES:\n    team:\n      address: x\n",
		"leaf only":          "config:\n  Sources:\n    team:\n      address: x\n",
		"non-string sibling": "config:\n  1: x\n  sources:\n    team:\n      address: x\n",
	}

	for name, file := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())

			fs := afero.NewOsFs()
			path := filepath.Join(t.TempDir(), ".mytool.yaml")
			require.NoError(t, afero.WriteFile(fs, path, []byte(file), 0o600))
			require.NoError(t, setup.TrustProjectConfig(fs, "mytool", path))

			log := logger.NewBuffer()
			view := buildProjectStore(t, fs, log, path).Config.View()

			assert.False(t, view.IsSet("config.sources.team.address"))
			assert.True(t, log.Contains("ignoring config.sources"))
		})
	}
}

func TestStripProtectedKeys_WalksEveryContainer(t *testing.T) {
	t.Parallel()

	nonString := map[any]any{1: "kept", "AUTH": map[string]any{"value": "x"}, "name": "kept"}
	listed := map[string]any{"Auth": map[string]any{"value": "x"}, "name": "kept"}
	doc := map[string]any{
		"github": nonString,
		"items":  []any{listed, map[any]any{"auth": "x"}, "scalar"},
	}

	removed := stripProtectedKeys(doc)

	assert.Equal(t, []string{"github.AUTH", "items.Auth", "items.auth"}, removed)
	assert.NotContains(t, nonString, "AUTH")
	assert.Equal(t, "kept", nonString[1])
	assert.Equal(t, "kept", nonString["name"])
	assert.NotContains(t, listed, "Auth")
	assert.Equal(t, "kept", listed["name"])
}

func TestStripProtectedKeys_FlatDottedKeys(t *testing.T) {
	t.Parallel()

	doc := map[string]any{
		"UPDATE.REQUIRE_SIGNATURE": false,
		"github.AUTH.value":        "x",
		"gitlab":                   map[string]any{"auth.value": "x"},
		"update.check_interval":    "24h",
	}

	assert.Equal(t,
		[]string{"UPDATE.REQUIRE_SIGNATURE", "github.AUTH.value", "gitlab.auth.value"},
		stripProtectedKeys(doc))
	assert.Contains(t, doc, "update.check_interval")
}
