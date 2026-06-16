package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

func TestParseTemplateSpec(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/local/tmpl", DefaultDirMode))

	cases := []struct {
		name     string
		spec     string
		wantType TemplateSourceType
		wantRef  string
		wantLoc  string
	}{
		{"git path with ref", "acme/templates@v1.0.0", TemplateSourceGit, "v1.0.0", "acme/templates"},
		{"git path no ref", "acme/templates", TemplateSourceGit, "", "acme/templates"},
		{"local dot path", "./x", TemplateSourceLocal, "", "./x"},
		{"local existing dir", "/local/tmpl", TemplateSourceLocal, "", "/local/tmpl"},
		{"https url ref", "https://gitlab.com/acme/templates@main", TemplateSourceGit, "main", "https://gitlab.com/acme/templates"},
		{"ssh url not mis-split", "ssh://git@host/acme/templates", TemplateSourceGit, "", "ssh://git@host/acme/templates"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ts, err := ParseTemplateSpec(fs, tc.spec, "")
			require.NoError(t, err)
			assert.Equal(t, tc.wantType, ts.Type)
			assert.Equal(t, tc.wantRef, ts.Ref)
			assert.Equal(t, tc.wantLoc, ts.Location)
		})
	}
}

func TestParseTemplateSpec_ExplicitNameWins(t *testing.T) {
	t.Parallel()

	ts, err := ParseTemplateSpec(afero.NewMemMapFs(), "acme/templates@v1", "house")
	require.NoError(t, err)
	assert.Equal(t, "house", ts.Name)
}

func TestTemplateManage_AddListRemoveRoundTrip(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop(), Version: version.NewInfo("v1.0.0", "", "")}
	g := New(p, &Config{Path: "/project", Overwrite: "allow"})

	// Generate a base project first.
	require.NoError(t, g.GenerateSkeleton(context.Background(), SkeletonConfig{
		Name: "mytool", Repo: "acme/mytool", Host: "github.com", Path: "/project",
	}))

	// Add a local source.
	writeLocalSource(t, fs, "/tmpl", map[string]string{"EXTRA.md": "extra {{ .Name }}"})

	g2 := New(p, &Config{Path: "/project", Overwrite: "allow"})
	require.NoError(t, g2.AddTemplateSource(context.Background(),
		TemplateSource{Type: TemplateSourceLocal, Location: "/tmpl", Name: "house"}))

	extra, err := afero.ReadFile(fs, "/project/EXTRA.md")
	require.NoError(t, err)
	assert.Equal(t, "extra mytool", string(extra))

	// List shows it.
	g3 := New(p, &Config{Path: "/project"})
	sources, err := g3.ListTemplateSources()
	require.NoError(t, err)
	require.Len(t, sources, 1)
	assert.Equal(t, "house", sources[0].Name)
	assert.NotEmpty(t, sources[0].Hashes["EXTRA.md"])

	// Adding a duplicate name is rejected.
	require.Error(t, g3.AddTemplateSource(context.Background(),
		TemplateSource{Type: TemplateSourceLocal, Location: "/tmpl", Name: "house"}))

	// Remove drops it from the manifest.
	g4 := New(p, &Config{Path: "/project", Overwrite: "allow"})
	require.NoError(t, g4.RemoveTemplateSource(context.Background(), "house"))

	g5 := New(p, &Config{Path: "/project"})
	after, err := g5.ListTemplateSources()
	require.NoError(t, err)
	assert.Empty(t, after)
}

func TestTemplateManage_AddRollsBackOnProtectedPath(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop(), Version: version.NewInfo("v1.0.0", "", "")}

	require.NoError(t, New(p, &Config{Path: "/project", Overwrite: "allow"}).GenerateSkeleton(
		context.Background(), SkeletonConfig{Name: "mytool", Repo: "acme/mytool", Host: "github.com", Path: "/project"}))

	// A malicious source that writes a protected path.
	writeLocalSource(t, fs, "/evil", map[string]string{"go.mod": "module evil"})

	err := New(p, &Config{Path: "/project", Overwrite: "allow"}).AddTemplateSource(
		context.Background(), TemplateSource{Type: TemplateSourceLocal, Location: "/evil", Name: "evil"})
	require.Error(t, err)

	// The rejected source must not linger in the manifest.
	sources, err := New(p, &Config{Path: "/project"}).ListTemplateSources()
	require.NoError(t, err)
	assert.Empty(t, sources, "a rejected add must roll back the manifest")
}

func TestTemplateManage_RemoveRestoresSuppressedScaffold(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop(), Version: version.NewInfo("v1.0.0", "", "")}

	// Base project on gitlab (so the gitlab CI scaffold is the embedded one).
	require.NoError(t, New(p, &Config{Path: "/project", Overwrite: "allow"}).GenerateSkeleton(
		context.Background(), SkeletonConfig{Name: "mytool", Repo: "acme/mytool", Host: "gitlab.com", Path: "/project"}))

	// Embedded gitlab CI present initially.
	exists, _ := afero.Exists(fs, "/project/renovate.json5")
	require.True(t, exists)

	// Add a replacing source.
	writeLocalSource(t, fs, "/tmpl", map[string]string{
		"gtb-template.yaml": "contract: 1\nreplaces:\n  - gitlab-ci\n",
		".gitlab-ci.yml":    "# acme",
	})
	require.NoError(t, New(p, &Config{Path: "/project", Overwrite: "allow"}).AddTemplateSource(
		context.Background(), TemplateSource{Type: TemplateSourceLocal, Location: "/tmpl", Name: "acme"}))

	// Embedded CI suppressed.
	exists, _ = afero.Exists(fs, "/project/renovate.json5")
	require.False(t, exists)

	// Remove restores the embedded scaffold on regenerate.
	require.NoError(t, New(p, &Config{Path: "/project", Overwrite: "allow"}).RemoveTemplateSource(
		context.Background(), "acme"))

	exists, _ = afero.Exists(fs, "/project/renovate.json5")
	assert.True(t, exists, "removing a replacing source restores the suppressed scaffold")
}
