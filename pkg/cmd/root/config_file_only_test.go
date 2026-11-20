package root

import (
	"fmt"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

const fileOnlyUserConfig = `update:
  require_signature: true
  channel: stable
a:
  b:
    c: one
    e: two
  d: three
`

type fileOnlyRun struct {
	props *p.Props
	log   interface{ Entries() []logger.Entry }
	view  *config.View
}

// buildFileOnly builds the tool's store over fileOnlyUserConfig with the
// given file-only patterns, key case and environment.
func buildFileOnly(t *testing.T, keyCase p.KeyCase, fileOnly []string, env map[string]string) fileOnlyRun {
	t.Helper()

	for name, value := range env {
		t.Setenv(name, value)
	}

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/mytool/config.yaml", []byte(fileOnlyUserConfig), 0o600))

	log := logger.NewBuffer()
	props := &p.Props{
		Tool: p.Tool{
			Name:      "mytool",
			EnvPrefix: "MYTOOL",
			Config:    p.ConfigSpec{KeyCase: keyCase, FileOnly: fileOnly},
		},
		Logger: log,
		FS:     fs,
	}

	store, err := buildConfigStore(t.Context(), ConfigLoadOptions{Props: props, CfgPaths: []string{"/etc/mytool/config.yaml"}})
	require.NoError(t, err)

	return fileOnlyRun{props: props, log: log, view: store.View()}
}

func TestFileOnly_ProbeRows(t *testing.T) {
	rows := []struct {
		name     string
		variable string
		value    string
	}{
		{"canonical spelling", "MYTOOL_UPDATE_REQUIRE_SIGNATURE", "false"},
		{"lower-case spelling", "MYTOOL_update_require_signature", "false"},
		{"bare section", "MYTOOL_UPDATE", "x"},
		{"bare lower-case section", "MYTOOL_update", "x"},
	}

	for _, keyCase := range []p.KeyCase{p.KeyCaseFolded, p.KeyCaseExact} {
		for _, row := range rows {
			t.Run(string(keyCase)+"/"+row.name, func(t *testing.T) {
				run := buildFileOnly(t, keyCase, []string{"update.**"}, map[string]string{row.variable: row.value})

				assert.True(t, run.view.GetBool("update.require_signature"), "the file's value stands")
				assert.Equal(t, "stable", run.view.GetString("update.channel"), "the section is intact")
				assert.False(t, run.view.Has("update.require.signature"), "no stray key is left behind")
			})
		}
	}
}

func TestFileOnly_UnfilteredBaseline(t *testing.T) {
	run := buildFileOnly(t, p.KeyCaseFolded, nil, map[string]string{"MYTOOL_UPDATE_REQUIRE_SIGNATURE": "false"})

	assert.False(t, run.view.GetBool("update.require_signature"), "with nothing declared the environment still applies")
	assert.Empty(t, run.props.WithheldEnv)
}

func TestFileOnly_SingleKeyLeavesItsSiblings(t *testing.T) {
	run := buildFileOnly(t, p.KeyCaseFolded, []string{"update.require_signature"}, map[string]string{
		"MYTOOL_UPDATE_REQUIRE_SIGNATURE": "false",
		"MYTOOL_UPDATE_CHANNEL":           "beta",
	})

	assert.True(t, run.view.GetBool("update.require_signature"))
	assert.Equal(t, "beta", run.view.GetString("update.channel"), "a sibling key still takes the environment")
}

func TestFileOnly_SingleKeyBlocksItsBareSection(t *testing.T) {
	run := buildFileOnly(t, p.KeyCaseFolded, []string{"update.require_signature"}, map[string]string{"MYTOOL_UPDATE": "x"})

	assert.True(t, run.view.GetBool("update.require_signature"))
	assert.Equal(t, "stable", run.view.GetString("update.channel"))
}

func TestFileOnly_ThreeSegmentPatternBlocksBothAncestors(t *testing.T) {
	for _, variable := range []string{"MYTOOL_A", "MYTOOL_A_B", "MYTOOL_A_B_C"} {
		t.Run(variable, func(t *testing.T) {
			run := buildFileOnly(t, p.KeyCaseFolded, []string{"a.b.c"}, map[string]string{variable: "x"})

			assert.Equal(t, "one", run.view.GetString("a.b.c"))
			assert.Equal(t, []string{variable}, run.props.WithheldEnv)
		})
	}

	run := buildFileOnly(t, p.KeyCaseFolded, []string{"a.b.c"}, map[string]string{"MYTOOL_A_B_E": "x", "MYTOOL_A_D": "y"})
	assert.Equal(t, "x", run.view.GetString("a.b.e"), "a sibling under the same section applies")
	assert.Equal(t, "y", run.view.GetString("a.d"))
	assert.Empty(t, run.props.WithheldEnv)
}

func TestFileOnly_WildcardSegmentCarriesIntoTheSection(t *testing.T) {
	run := buildFileOnly(t, p.KeyCaseFolded, []string{"a.*.c"}, map[string]string{"MYTOOL_A_B": "x"})
	assert.Equal(t, "one", run.view.GetString("a.b.c"), "a.* blocks the section holding a.b.c")

	run = buildFileOnly(t, p.KeyCaseFolded, []string{"a.*.c"}, map[string]string{"MYTOOL_A_B_E": "x"})
	assert.Equal(t, "x", run.view.GetString("a.b.e"), "a.b.e is neither a.*.c nor a section above it")
}

func TestFileOnly_WithheldVariableIsNamedNeverItsValue(t *testing.T) {
	run := buildFileOnly(t, p.KeyCaseFolded, []string{"update.**"}, map[string]string{
		"MYTOOL_UPDATE_REQUIRE_SIGNATURE": "s3cr3t-value",
		"MYTOOL_UPDATE":                   "another-s3cr3t",
		"MYTOOL_A_D":                      "kept",
	})

	assert.Equal(t, []string{"MYTOOL_UPDATE", "MYTOOL_UPDATE_REQUIRE_SIGNATURE"}, run.props.WithheldEnv)

	logged := fmt.Sprint(run.log.Entries())
	assert.Contains(t, logged, "file-only")
	assert.Contains(t, logged, "MYTOOL_UPDATE_REQUIRE_SIGNATURE")
	assert.NotContains(t, logged, "s3cr3t", "a withheld value is never logged")
	assert.Equal(t, "kept", run.view.GetString("a.d"))
}

func TestFileOnly_NothingWithheldLogsNothing(t *testing.T) {
	run := buildFileOnly(t, p.KeyCaseFolded, []string{"update.**"}, map[string]string{"MYTOOL_A_D": "kept"})

	assert.Empty(t, run.props.WithheldEnv)
	assert.NotContains(t, fmt.Sprint(run.log.Entries()), "file-only")
}

// Spec 0214 D4: the first pass, which reads a source's settings, is filtered
// too, so a variable cannot point a source somewhere else.
func TestFileOnly_TheBootstrapPassIsFiltered(t *testing.T) {
	t.Setenv("MYTOOL_CONFIG_SOURCES_VAULT_ADDRESS", "https://elsewhere.invalid")
	t.Setenv("MYTOOL_CONFIG", "x")

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/mytool/config.yaml",
		[]byte("config:\n  sources:\n    vault:\n      address: https://vault.internal\n"), 0o600))

	props := &p.Props{
		Tool: p.Tool{
			Name:      "mytool",
			EnvPrefix: "MYTOOL",
			Config:    p.ConfigSpec{FileOnly: []string{"config.sources.**"}},
		},
		Logger: logger.NewNoop(),
		FS:     fs,
	}
	opts := ConfigLoadOptions{Props: props, CfgPaths: []string{"/etc/mytool/config.yaml"}}

	store, err := bootstrapStore(t.Context(), opts, p.DefaultConfigLayers())
	require.NoError(t, err)

	assert.Equal(t, "https://vault.internal", store.View().GetString("config.sources.vault.address"))
}
