package root

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// sourcePointers is a project file that tries to point the tool at a config
// source of its choosing, beside an ordinary workflow key.
const sourcePointers = `config:
  sources:
    team:
      address: https://consul.attacker.example
      auth:
        env: ATTACKER_TOKEN
  other: kept
log:
  level: debug
`

// Spec 0204 D5: where configuration comes from is never a repository's to
// choose. The whole config.sources subtree is stripped from an untrusted
// project file, whatever the source is called.
func TestProjectLocalTrust_UntrustedCannotPointAtASource(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	fs := afero.NewOsFs()
	path := filepath.Join(t.TempDir(), ".mytool.yaml")
	require.NoError(t, afero.WriteFile(fs, path, []byte(sourcePointers), 0o600))

	log := logger.NewBuffer()
	view := buildProjectStore(t, fs, log, path).Config.View()

	assert.False(t, view.IsSet("config.sources"), "no source pointer from an untrusted project file")
	assert.Equal(t, "kept", view.GetString("config.other"), "the rest of config is not a pointer")
	assert.Equal(t, "debug", view.GetString("log.level"))
	assert.True(t, logged(log.Entries(), "config.sources"), "the stripped subtree is named in the warning")
}

// config trust does not re-admit it: trusting a repository to set its own
// log level is a smaller act than trusting it to choose your secret store.
// Every other key of a trusted file still applies, and the file stays
// writable.
func TestProjectLocalTrust_TrustDoesNotReadmitSources(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	fs := afero.NewOsFs()
	path := filepath.Join(t.TempDir(), ".mytool.yaml")
	require.NoError(t, afero.WriteFile(fs, path, []byte(sourcePointers+"telemetry:\n  enabled: true\n"), 0o600))
	require.NoError(t, setup.TrustProjectConfig(fs, "mytool", path))

	log := logger.NewBuffer()
	props := buildProjectStore(t, fs, log, path)
	view := props.Config.View()

	assert.False(t, view.IsSet("config.sources"), "trust does not admit source pointers")
	assert.True(t, view.GetBool("telemetry.enabled"), "trust still admits every other key")
	assert.True(t, logged(log.Entries(), "config.sources"))

	_, err := props.Config.Apply(t.Context(), config.Set("log.level", "warn"))
	require.NoError(t, err, "a trusted project file stays writable")

	written, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Contains(t, string(written), "level: warn")
}

// logged reports whether any entry's message or attribute values mention
// substr.
func logged(entries []logger.Entry, substr string) bool {
	for _, e := range entries {
		if strings.Contains(e.Message+" "+fmt.Sprint(e.Keyvals...), substr) {
			return true
		}
	}

	return false
}

func TestStripSourcePointers(t *testing.T) {
	t.Parallel()

	doc := map[string]any{
		"config": map[string]any{
			"sources": map[string]any{"vault": map[string]any{"address": "x"}},
			"other":   "kept",
		},
		"github": map[string]any{"auth": map[string]any{"value": "x"}},
	}

	assert.Equal(t, []string{"config.sources"}, stripSourcePointers(doc))
	assert.Equal(t, map[string]any{"other": "kept"}, doc["config"])
	assert.Contains(t, doc, "github", "only the source pointers go; trust governs the rest")
}
