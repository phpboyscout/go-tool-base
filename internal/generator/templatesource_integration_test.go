package generator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// TestGitTemplateSource_RealCloneResolvesAndPins exercises the production
// provider-aware clone leg (realCloneTemplate → pkg/vcs/repo) against a real
// on-disk git repository — no network. It proves the clone resolves a ref to a
// concrete commit, checks it out, and returns the matching SHA. The clone URL
// builder and validation are deliberately bypassed here (they enforce
// https/ssh transports); this test targets the clone+resolve+checkout leg the
// generator wires into the SHA-keyed cache.
func TestGitTemplateSource_RealCloneResolvesAndPins(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "vcs")

	srcRepo := filepath.Join(t.TempDir(), "tmpl-repo")
	wantSHA := initTemplateRepo(t, srcRepo)

	target := filepath.Join(t.TempDir(), "clone")

	p := &props.Props{FS: afero.NewOsFs(), Logger: logger.NewNoop()}
	g := New(p, &Config{}).EnableRealTemplateClone()

	res, err := g.cloneTemplate(cloneRequest{URL: srcRepo, Ref: "master", TargetDir: target})
	require.NoError(t, err)
	assert.Equal(t, wantSHA, res.ResolvedSHA, "clone resolves the ref to its commit SHA")

	// The checked-out tree contains the template file.
	content, err := os.ReadFile(filepath.Join(target, "SECURITY.md"))
	require.NoError(t, err)
	assert.Equal(t, "secure {{ .Name }}", string(content))
}

// TestGitTemplateSource_OverlayAndOfflineRegenerate exercises the full git
// overlay + SHA-keyed cache + offline regenerate path with the clone leg faked
// (the real clone is covered above). It proves the pin makes regenerate
// reproduce byte-identically with no clone available.
func TestGitTemplateSource_OverlayAndOfflineRegenerate(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "vcs")

	srcRepo := filepath.Join(t.TempDir(), "tmpl-repo")
	wantSHA := initTemplateRepo(t, srcRepo)

	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	projectDir := filepath.Join(t.TempDir(), "project")
	p := &props.Props{FS: afero.NewOsFs(), Logger: logger.NewNoop(), Version: version.NewInfo("v1.0.0", "", "")}

	g := New(p, &Config{Path: projectDir, Overwrite: "allow"})
	// Fake clone delegates to a real clone of the local repo, so the cache is
	// populated exactly as production would, without an https transport.
	g.WithTemplateClone(func(req cloneRequest) (cloneResult, error) {
		real := New(p, &Config{}).EnableRealTemplateClone()

		return real.cloneTemplate(cloneRequest{URL: srcRepo, Ref: req.Ref, TargetDir: req.TargetDir})
	})

	err := g.GenerateSkeleton(context.Background(), SkeletonConfig{
		Name: "mytool", Repo: "acme/mytool", Host: "github.com", Path: projectDir,
		Templates: []TemplateSource{{Type: TemplateSourceGit, Location: "acme/templates", Ref: "master", Name: "house"}},
	})
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(projectDir, "SECURITY.md"))
	require.NoError(t, err)
	assert.Equal(t, "secure mytool", string(content))

	m, err := DecodeManifestFile(p.FS, ManifestPathFor(projectDir))
	require.NoError(t, err)
	require.Len(t, m.Properties.Templates, 1)
	assert.Equal(t, wantSHA, m.Properties.Templates[0].Resolved)

	// Offline regenerate from the warm SHA cache (no clone wired).
	g2 := New(p, &Config{Path: projectDir, Overwrite: "allow"})
	require.NoError(t, g2.RegenerateProject(context.Background()))

	again, err := os.ReadFile(filepath.Join(projectDir, "SECURITY.md"))
	require.NoError(t, err)
	assert.Equal(t, "secure mytool", string(again))
}

// initTemplateRepo creates a git repo at dir with a single template file and
// returns the commit SHA.
func initTemplateRepo(t *testing.T, dir string) string {
	t.Helper()

	require.NoError(t, os.MkdirAll(dir, 0o755))

	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "SECURITY.md"), []byte("secure {{ .Name }}"), 0o644))

	wt, err := repo.Worktree()
	require.NoError(t, err)
	_, err = wt.Add("SECURITY.md")
	require.NoError(t, err)

	hash, err := wt.Commit("init template", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example.com"},
	})
	require.NoError(t, err)

	return hash.String()
}
