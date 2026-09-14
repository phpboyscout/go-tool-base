package generator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// normWS collapses runs of whitespace to a single space so assertions are
// robust to gofmt's struct-field colon alignment (e.g. "AutoInitialise:  true").
func normWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// TestManifestBootstrap_RoundTrip proves the bootstrap block survives a
// marshal/unmarshal cycle, so `gtb regenerate` reproduces the chosen policy
// deterministically.
func TestManifestBootstrap_RoundTrip(t *testing.T) {
	t.Parallel()

	m := Manifest{}
	m.Properties.Bootstrap = ManifestBootstrap{
		AutoInitialise:  true,
		SkipConfigCheck: []string{"studio", "app tools studio"},
	}

	data, err := yaml.Marshal(m)
	require.NoError(t, err)
	assert.Contains(t, string(data), "auto_initialise: true")
	assert.Contains(t, string(data), "skip_config_check:")

	var got Manifest
	require.NoError(t, yaml.Unmarshal(data, &got))
	assert.Equal(t, m.Properties.Bootstrap, got.Properties.Bootstrap)
}

func bootstrapSkeletonConfig(path string, bootstrap ManifestBootstrap) SkeletonConfig {
	return SkeletonConfig{
		Name:        "boot-tool",
		Repo:        "test/boot-tool",
		Host:        "github.com",
		Description: "bootstrap scaffold test",
		Path:        path,
		Features: []ManifestFeature{
			{Name: "changelog", Enabled: false},
			{Name: "docs", Enabled: false},
		},
		Bootstrap: bootstrap,
	}
}

// TestGenerateSkeleton_BootstrapPolicy asserts a non-default bootstrap policy is
// wired into the generated root command.
func TestGenerateSkeleton_BootstrapPolicy(t *testing.T) {
	path := t.TempDir()
	g := newSkeletonGeneratorForTest(t, afero.NewOsFs())

	cfg := bootstrapSkeletonConfig(path, ManifestBootstrap{
		AutoInitialise:  true,
		SkipConfigCheck: []string{"studio"},
	})
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	cmdGo, err := os.ReadFile(filepath.Join(path, "pkg", "cmd", "root", "cmd.go"))
	require.NoError(t, err)

	src := normWS(string(cmdGo))
	assert.Contains(t, src, "Bootstrap: props.BootstrapPolicy{")
	assert.Contains(t, src, "AutoInitialise: true")
	assert.Contains(t, src, `SkipConfigCheck: []string{"studio"}`)
}

// TestGenerateSkeleton_BootstrapSkipOnly emits only the skip list when
// auto-init is off.
func TestGenerateSkeleton_BootstrapSkipOnly(t *testing.T) {
	path := t.TempDir()
	g := newSkeletonGeneratorForTest(t, afero.NewOsFs())

	cfg := bootstrapSkeletonConfig(path, ManifestBootstrap{SkipConfigCheck: []string{"studio"}})
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	cmdGo, err := os.ReadFile(filepath.Join(path, "pkg", "cmd", "root", "cmd.go"))
	require.NoError(t, err)

	src := normWS(string(cmdGo))
	assert.Contains(t, src, "Bootstrap: props.BootstrapPolicy{")
	assert.Contains(t, src, `SkipConfigCheck: []string{"studio"}`)
	assert.NotContains(t, src, "AutoInitialise:")
}

// TestGenerateSkeleton_BootstrapDefault asserts an empty policy scaffolds no
// Bootstrap field, keeping default output unchanged.
func TestGenerateSkeleton_BootstrapDefault(t *testing.T) {
	path := t.TempDir()
	g := newSkeletonGeneratorForTest(t, afero.NewOsFs())

	require.NoError(t, g.GenerateSkeleton(context.Background(), bootstrapSkeletonConfig(path, ManifestBootstrap{})))

	cmdGo, err := os.ReadFile(filepath.Join(path, "pkg", "cmd", "root", "cmd.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(cmdGo), "Bootstrap:")
}
