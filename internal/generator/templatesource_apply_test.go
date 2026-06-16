package generator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// seedProject writes a minimal generated project (manifest + a go.mod) so the
// regenerate-style overlay paths have a manifest to read and persist back to.
func seedProject(t *testing.T, fs afero.Fs, root, name string) {
	t.Helper()

	m := &Manifest{
		Properties: ManifestProperties{Name: name, Description: "A tool"},
		ReleaseSource: ManifestReleaseSource{
			Type: "github", Host: "github.com", Owner: "acme", Repo: name,
		},
		Version: ManifestVersion{GoToolBase: "v1.0.0"},
	}
	require.NoError(t, fs.MkdirAll(filepath.Join(root, ".gtb"), DefaultDirMode))
	require.NoError(t, EncodeManifestFile(fs, ManifestPathFor(root), m))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "go.mod"), []byte("module github.com/acme/"+name+"\n"), DefaultFileMode))
}

// --- D6: multi-source layering, last writer wins ---

func TestApplyOverlays_MultiSourceLastWriterWins(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: "/project", Overwrite: "allow"})

	writeLocalSource(t, fs, "/s1", map[string]string{
		"SHARED.md": "from-one",
		"ONLY1.md":  "one",
	})
	writeLocalSource(t, fs, "/s2", map[string]string{
		"SHARED.md": "from-two",
		"ONLY2.md":  "two",
	})

	sources := []TemplateSource{
		{Type: TemplateSourceLocal, Location: "/s1"},
		{Type: TemplateSourceLocal, Location: "/s2"},
	}

	resolved, err := g.resolveAllSources(sources)
	require.NoError(t, err)

	collected := map[string]string{}
	updated, err := g.applyOverlays("/project", sampleContractData(), map[string]string{}, collected, &IgnoreRules{}, sources, resolved)
	require.NoError(t, err)

	shared, err := afero.ReadFile(fs, "/project/SHARED.md")
	require.NoError(t, err)
	assert.Equal(t, "from-two", string(shared), "last source in manifest order wins")

	// Per-source hashes are self-contained.
	assert.Contains(t, updated[0].Hashes, "ONLY1.md")
	assert.Contains(t, updated[1].Hashes, "ONLY2.md")
	assert.Contains(t, updated[1].Hashes, "SHARED.md")
}

// --- D3: replaces suppresses embedded gitlab CI, source supplies its own ---

func TestGenerate_ReplacesGitlabCISuppressesEmbedded(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop(), Version: version.NewInfo("v1.0.0", "", "")}
	g := New(p, &Config{Path: "/project", Overwrite: "allow"})

	// A source that replaces the whole gitlab CI scaffold with a single file.
	writeLocalSource(t, fs, "/tmpl", map[string]string{
		"gtb-template.yaml": "contract: 1\nreplaces:\n  - gitlab-ci\n",
		".gitlab-ci.yml":    "# ACME pipeline for {{ .Name }}",
	})

	err := g.GenerateSkeleton(context.Background(), SkeletonConfig{
		Name:      "mytool",
		Repo:      "acme/mytool",
		Host:      "gitlab.com",
		Path:      "/project",
		Templates: []TemplateSource{{Type: TemplateSourceLocal, Location: "/tmpl", Name: "acme"}},
	})
	require.NoError(t, err)

	// The source's CI is present...
	ci, err := afero.ReadFile(fs, "/project/.gitlab-ci.yml")
	require.NoError(t, err)
	assert.Equal(t, "# ACME pipeline for mytool", string(ci))

	// ...and the embedded gitlab CI subtree was suppressed (not written).
	for _, suppressed := range []string{"/project/.gitlab/ci/release.yml", "/project/renovate.json5"} {
		exists, _ := afero.Exists(fs, suppressed)
		assert.Falsef(t, exists, "%s should have been suppressed", suppressed)
	}

	// Manifest records the source with a fingerprint and per-source hash.
	m, err := DecodeManifestFile(fs, ManifestPathFor("/project"))
	require.NoError(t, err)
	require.Len(t, m.Properties.Templates, 1)
	assert.Equal(t, "acme", m.Properties.Templates[0].Name)
	assert.NotEmpty(t, m.Properties.Templates[0].Fingerprint)
	assert.Contains(t, m.Properties.Templates[0].Hashes, ".gitlab-ci.yml")
}

// --- git source: fake clone pins SHA; regenerate reproduces from the pin ---

func TestGitSource_PinsSHAAndRegenerateReproducesFromCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop(), Version: version.NewInfo("v1.0.0", "", "")}
	g := New(p, &Config{Path: "/project", Overwrite: "allow"})

	const sha = "a1b2c3d4e5f600112233445566778899aabbccdd"

	cloneCalls := 0
	g.WithTemplateClone(func(req cloneRequest) (cloneResult, error) {
		cloneCalls++
		// Populate the staging dir with a template tree.
		writeLocalSource(t, fs, req.TargetDir, map[string]string{
			"SECURITY.md": "secure {{ .Name }}",
		})

		return cloneResult{ResolvedSHA: sha}, nil
	})

	err := g.GenerateSkeleton(context.Background(), SkeletonConfig{
		Name:      "mytool",
		Repo:      "acme/mytool",
		Host:      "github.com",
		Path:      "/project",
		Templates: []TemplateSource{{Type: TemplateSourceGit, Location: "acme/templates", Ref: "main"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, cloneCalls)

	m, err := DecodeManifestFile(fs, ManifestPathFor("/project"))
	require.NoError(t, err)
	require.Len(t, m.Properties.Templates, 1)
	assert.Equal(t, sha, m.Properties.Templates[0].Resolved, "ref must be pinned to the resolved SHA")
	assert.Equal(t, "main", m.Properties.Templates[0].Ref)

	sec, err := afero.ReadFile(fs, "/project/SECURITY.md")
	require.NoError(t, err)
	assert.Equal(t, "secure mytool", string(sec))

	// Regenerate with NO clone available (offline): warm cache reproduces.
	g2 := New(p, &Config{Path: "/project", Overwrite: "allow"})
	// cloneTemplate is nil on g2 — offline. The warm SHA-keyed cache must serve.
	require.NoError(t, g2.RegenerateProject(context.Background()))

	sec2, err := afero.ReadFile(fs, "/project/SECURITY.md")
	require.NoError(t, err)
	assert.Equal(t, "secure mytool", string(sec2), "regenerate reproduces from the pinned cache offline")
}

// --- D9: offline cold cache for a git source errors clearly ---

func TestGitSource_OfflineColdCacheErrorsClearly(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	fs := afero.NewMemMapFs()
	seedProject(t, fs, "/project", "mytool")

	// Manifest references a git source with a pin, but the cache is cold and
	// there is no clone available (g.cloneTemplate == nil → offline).
	m, err := DecodeManifestFile(fs, ManifestPathFor("/project"))
	require.NoError(t, err)
	m.Properties.Templates = []TemplateSource{{
		Type:     TemplateSourceGit,
		Location: "acme/templates",
		Ref:      "main",
		Resolved: "a1b2c3d4e5f600112233445566778899aabbccdd",
	}}
	require.NoError(t, EncodeManifestFile(fs, ManifestPathFor("/project"), m))

	g := New(&props.Props{FS: fs, Logger: logger.NewNoop(), Version: version.NewInfo("v1.0.0", "", "")}, &Config{Path: "/project", Overwrite: "allow"})

	err = g.RegenerateProject(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "offline")
}

// --- local source drift warning on regenerate ---

func TestLocalSource_DriftDoesNotBreakRegenerate(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop(), Version: version.NewInfo("v1.0.0", "", "")}
	g := New(p, &Config{Path: "/project", Overwrite: "allow"})

	writeLocalSource(t, fs, "/tmpl", map[string]string{"NOTE.md": "v1 {{ .Name }}"})

	require.NoError(t, g.GenerateSkeleton(context.Background(), SkeletonConfig{
		Name:      "mytool",
		Repo:      "acme/mytool",
		Host:      "github.com",
		Path:      "/project",
		Templates: []TemplateSource{{Type: TemplateSourceLocal, Location: "/tmpl"}},
	}))

	// Drift the on-disk source.
	require.NoError(t, afero.WriteFile(fs, "/tmpl/NOTE.md", []byte("v2 {{ .Name }}"), DefaultFileMode))

	g2 := New(p, &Config{Path: "/project", Overwrite: "allow"})
	require.NoError(t, g2.RegenerateProject(context.Background()))

	note, err := afero.ReadFile(fs, "/project/NOTE.md")
	require.NoError(t, err)
	assert.Equal(t, "v2 mytool", string(note), "regenerate reflects the current on-disk tree")
}

// --- .gtb/ignore suppresses an overlay output path ---

func TestOverlay_IgnoreSuppressesOutput(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: "/project", Overwrite: "allow"})

	writeLocalSource(t, fs, "/tmpl", map[string]string{
		"keep.md": "keep",
		"drop.md": "drop",
	})

	rules := loadIgnoreFromString("drop.md")

	sources := []TemplateSource{{Type: TemplateSourceLocal, Location: "/tmpl"}}
	resolved, err := g.resolveAllSources(sources)
	require.NoError(t, err)

	_, err = g.applyOverlays("/project", sampleContractData(), map[string]string{}, map[string]string{}, rules, sources, resolved)
	require.NoError(t, err)

	keepExists, _ := afero.Exists(fs, "/project/keep.md")
	assert.True(t, keepExists)
	dropExists, _ := afero.Exists(fs, "/project/drop.md")
	assert.False(t, dropExists, "ignored overlay path must not be written")
}

// loadIgnoreFromString builds IgnoreRules from inline ignore content for tests.
func loadIgnoreFromString(content string) *IgnoreRules {
	fs := afero.NewMemMapFs()
	_ = afero.WriteFile(fs, "/p/.gtb/ignore", []byte(content), DefaultFileMode)

	return LoadIgnoreRules(fs, "/p")
}
