package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"
)

func ptr(b bool) *bool { return &b }

// Spec 0204 D3, D15: a source's kind is one the framework ships or one an
// author's override builds, and its name follows D15's rules.
func TestValidateConfigSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sources  []ManifestConfigSource
		features []ManifestFeature
		wantErr  string
	}{
		{name: "none"},
		{name: "a shipped kind", sources: []ManifestConfigSource{{Name: "team", Kind: "consul"}}},
		{name: "an override-only kind", sources: []ManifestConfigSource{{Name: "legacy", Kind: "etcd"}}},
		{name: "an unknown kind", sources: []ManifestConfigSource{{Name: "team", Kind: "zookeeper"}}, wantErr: "unknown config source kind"},
		{name: "a held kind", sources: []ManifestConfigSource{{Name: "mount", Kind: "filekv"}}, wantErr: "unknown config source kind"},
		{name: "a bad name", sources: []ManifestConfigSource{{Name: "Team", Kind: "consul"}}, wantErr: "command word"},
		{
			name:     "a keychain source beside a disabled keychain",
			sources:  []ManifestConfigSource{{Name: "tokens", Kind: "keychain"}},
			features: []ManifestFeature{{Name: "keychain", Enabled: false}},
			wantErr:  "keychain",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateConfigSources(tc.sources, defaultSourceLayers(tc.sources), tc.features)
			if tc.wantErr == "" {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, errors.FlattenHints(err)+err.Error(), tc.wantErr)
		})
	}
}

// Spec 0204 R9: with no layer list the sources take the default placement,
// so an author declaring a slot need not also write the stack.
func TestValidateConfigSources_NoLayerListTakesTheDefault(t *testing.T) {
	t.Parallel()

	sources := []ManifestConfigSource{{Name: "team", Kind: "consul"}}
	require.NoError(t, ValidateConfigSources(sources, nil, nil))

	err := ValidateConfigSources(sources, []string{"defaults", "files", "env", "flags"}, nil)
	require.Error(t, err, "a stated list must place the slot")
	assert.Contains(t, errors.FlattenHints(err), "not in the layer list")
}

// A manifest's layer list names its sources, which the builtin-only layer
// check alone would refuse.
func TestValidateManifestConfig_SourceLayers(t *testing.T) {
	t.Parallel()

	p := &ManifestProperties{Config: ManifestConfig{
		Layers:  []string{"defaults", "team", "files", "project", "env", "flags"},
		Sources: []ManifestConfigSource{{Name: "team", Kind: "consul"}},
	}}
	require.NoError(t, validateManifestConfig(p))

	p.Config.Layers = []string{"defaults", "team", "flags", "env"}
	require.Error(t, validateManifestConfig(p), "the order rules still hold with sources")
}

// With sources and no stated order, they sit above the defaults and below
// the user's own files (R9), in the order declared.
func TestDefaultSourceLayers(t *testing.T) {
	t.Parallel()

	assert.Nil(t, defaultSourceLayers(nil))
	assert.Equal(t,
		[]string{"defaults", "shared", "secrets", "files", "project", "env", "flags"},
		defaultSourceLayers([]ManifestConfigSource{{Name: "shared", Kind: "consul"}, {Name: "secrets", Kind: "vault"}}))
}

func generateWithSources(t *testing.T, fs afero.Fs, cfg SkeletonConfig) *Generator {
	t.Helper()

	g := newSkeletonGeneratorForTest(t, fs)
	cfg.Name, cfg.Repo, cfg.Host, cfg.ForgeBackend = "src-tool", "acme/src-tool", "github.com", "github"
	cfg.Description, cfg.Path = "sources", "/work"
	cfg.Features = append(cfg.Features, ManifestFeature{Name: "changelog", Enabled: false}, ManifestFeature{Name: "docs", Enabled: false}, ManifestFeature{Name: "github", Enabled: true})
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	g.config.Path = "/work"

	return g
}

// A declared source is recorded, placed, rendered into the root, and linked
// by config.go; the tool gains an environment prefix (D21).
func TestGenerateSkeleton_DeclaresSources(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := generateWithSources(t, fs, SkeletonConfig{ConfigSources: []ManifestConfigSource{
		{Name: "team", Kind: "consul", Required: ptr(false)},
		{Name: "legacy", Kind: "etcd"},
	}})

	m, err := g.loadManifest()
	require.NoError(t, err)
	assert.Len(t, m.Properties.Config.Sources, 2)
	assert.Equal(t, []string{"defaults", "team", "legacy", "files", "project", "env", "flags"}, m.Properties.Config.Layers)
	assert.Equal(t, "SRC_TOOL", m.Properties.EnvPrefix)

	root := readGenerated(t, fs, "/work/pkg/cmd/root/cmd.go")
	assert.Contains(t, root, `props.ConfigLayer("team")`, "a source's layer is its slot name, not a constant")
	assert.NotContains(t, root, "props.LayerTeam")
	assert.Contains(t, root, `"consul"`)
	assert.Contains(t, root, "props.BoolPtr(false)", "an optional slot says so")
	assert.Contains(t, root, `"SRC_TOOL"`)

	linked := readGenerated(t, fs, "/work/cmd/src-tool/config.go")
	assert.Contains(t, linked, `"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/consul"`)
	assert.NotContains(t, linked, "sources/etcd", "an override-only kind has no package: the author's override builds it")
}

// An author's own prefix is kept; D21 only fills an empty one.
func TestGenerateSkeleton_SourcesKeepTheAuthorsPrefix(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := generateWithSources(t, fs, SkeletonConfig{EnvPrefix: "ACME", ConfigSources: []ManifestConfigSource{{Name: "team", Kind: "consul"}}})

	m, err := g.loadManifest()
	require.NoError(t, err)
	assert.Equal(t, "ACME", m.Properties.EnvPrefix)
}

// Spec 0204 D12: a keychain source implies the keychain feature.
func TestGenerateSkeleton_AKeychainSourceLinksTheKeychain(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	generateWithSources(t, fs, SkeletonConfig{ConfigSources: []ManifestConfigSource{{Name: "tokens", Kind: "keychain"}}})

	exists, err := afero.Exists(fs, "/work/cmd/src-tool/keychain.go")
	require.NoError(t, err)
	assert.True(t, exists)
}
