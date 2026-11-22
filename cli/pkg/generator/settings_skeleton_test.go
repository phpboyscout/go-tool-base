package generator

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

const settingsSkeletonRoot = "/work"

// newSettingsSkeletonProject generates a GitLab project with docs into memory
// and returns a generator configured as gtb set builds its own.
func newSettingsSkeletonProject(t *testing.T) (*Generator, afero.Fs, logBuffer) {
	t.Helper()

	fsys := afero.NewMemMapFs()
	buf := logger.NewBuffer()

	g := New(&props.Props{FS: fsys, Logger: buf, Config: emptyTestStore(t)}, &Config{Overwrite: OverwriteAllow})
	g.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil }

	require.NoError(t, g.GenerateSkeleton(context.Background(), SkeletonConfig{
		Name: "widget", Repo: "acme/widget", Host: "gitlab.com", ForgeBackend: forge.GitlabFeature,
		Description: "a widget", Path: settingsSkeletonRoot,
		Features: []ManifestFeature{
			{Name: "docs", Enabled: true}, {Name: "update", Enabled: true},
			{Name: "changelog", Enabled: false}, {Name: string(forge.GitlabFeature), Enabled: true},
		},
		ReleaseChannel: ReleaseChannelForge,
	}))

	g.config.Path = settingsSkeletonRoot

	return g, fsys, buf
}

// projectFiles reads every file under the project except the manifest, whose
// hashes are the generator's bookkeeping rather than project content.
func projectFiles(t *testing.T, fsys afero.Fs) map[string]string {
	t.Helper()

	files := map[string]string{}

	require.NoError(t, afero.Walk(fsys, settingsSkeletonRoot, func(path string, info fs.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		if path == ManifestPathFor(settingsSkeletonRoot) {
			return nil
		}

		rel, err := filepath.Rel(settingsSkeletonRoot, path)
		if err != nil {
			return err
		}

		content, err := afero.ReadFile(fsys, path)
		files[filepath.ToSlash(rel)] = string(content)

		return err
	}))

	return files
}

// changedFiles lists the paths whose content differs between two snapshots,
// added and removed paths included.
func changedFiles(before, after map[string]string) []string {
	var changed []string

	for path, content := range after {
		if was, ok := before[path]; !ok || was != content {
			changed = append(changed, path)
		}
	}

	for path := range before {
		if _, ok := after[path]; !ok {
			changed = append(changed, path)
		}
	}

	return changed
}

// TestSetSetting_LeavesNothingForRegenerate is spec 0218's destination: after
// set, regenerate changes no file the setting feeds.
func TestSetSetting_LeavesNothingForRegenerate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path  string
		value []string
		feeds string
	}{
		{"ci.component_source", []string{"gitlab.example.org/cicd"}, ".gitlab-ci.yml"},
		{"description", []string{"a better widget"}, "zensical.toml"},
		{"homebrew.tap", []string{"acme/homebrew-tap"}, "scripts/push-homebrew-cask.sh"},
		{"module_path", []string{"gitlab.com/acme/widget2"}, ".golangci.yaml"},
		{"release_source.host", []string{"gitlab.example.org"}, ".goreleaser.yaml"},
		{"release_source.repo", []string{"acme/other"}, "zensical.toml"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			g, fsys, _ := newSettingsSkeletonProject(t)
			before := projectFiles(t, fsys)

			require.NoError(t, g.SetSetting(context.Background(), tt.path, tt.value))
			afterSet := projectFiles(t, fsys)
			assert.Contains(t, changedFiles(before, afterSet), tt.feeds, "set writes the file the setting feeds")

			require.NoError(t, g.RegenerateProject(context.Background()))
			assert.Empty(t, changedFiles(afterSet, projectFiles(t, fsys)), "regenerate has nothing left to change")
		})
	}
}

func TestSetSetting_RemovesAFileTheSettingStopsProducing(t *testing.T) {
	t.Parallel()

	g, fsys, _ := newSettingsSkeletonProject(t)
	require.NoError(t, g.SetSetting(context.Background(), "homebrew.tap", []string{"acme/homebrew-tap"}))
	require.NoError(t, g.UnsetSetting(context.Background(), "homebrew.tap"))

	exists, err := afero.Exists(fsys, settingsSkeletonRoot+"/scripts/push-homebrew-cask.sh")
	require.NoError(t, err)
	assert.False(t, exists)

	afterUnset := projectFiles(t, fsys)
	require.NoError(t, g.RegenerateProject(context.Background()))
	assert.Empty(t, changedFiles(afterUnset, projectFiles(t, fsys)))
}

// TestSetSetting_KeepsAHandEditedFile pins spec 0218 D5: the edited file is
// left, named, and the set still succeeds.
func TestSetSetting_KeepsAHandEditedFile(t *testing.T) {
	t.Parallel()

	g, fsys, buf := newSettingsSkeletonProject(t)
	path := settingsSkeletonRoot + "/.goreleaser.yaml"

	edited := readGenerated(t, fsys, path) + "# mine\n"
	require.NoError(t, afero.WriteFile(fsys, path, []byte(edited), 0o644))

	require.NoError(t, g.SetSetting(context.Background(), "release_source.host", []string{"gitlab.example.org"}))

	assert.Equal(t, edited, readGenerated(t, fsys, path))
	assert.True(t, buf.ContainsLevel(logger.WarnLevel, ".goreleaser.yaml was edited by hand, so it was left as it is"), "%v", buf.Messages())
	assert.Contains(t, readGenerated(t, fsys, settingsSkeletonRoot+"/zensical.toml"), "gitlab.example.org",
		"the other files the setting feeds are still written")

	m, err := g.loadManifest()
	require.NoError(t, err)
	assert.Equal(t, "gitlab.example.org", m.ReleaseSource.Host)
}

func TestSetSetting_KeepsAHandEditedFileItWouldRemove(t *testing.T) {
	t.Parallel()

	g, fsys, buf := newSettingsSkeletonProject(t)
	require.NoError(t, g.SetSetting(context.Background(), "homebrew.tap", []string{"acme/homebrew-tap"}))

	script := settingsSkeletonRoot + "/scripts/push-homebrew-cask.sh"
	require.NoError(t, afero.WriteFile(fsys, script, []byte("#!/bin/sh\n# mine\n"), 0o755))

	require.NoError(t, g.UnsetSetting(context.Background(), "homebrew.tap"))

	assert.Equal(t, "#!/bin/sh\n# mine\n", readGenerated(t, fsys, script))
	assert.True(t, buf.ContainsLevel(logger.WarnLevel, "scripts/push-homebrew-cask.sh was edited by hand"), "%v", buf.Messages())
}

func TestSetSetting_LeavesAnIgnoredFile(t *testing.T) {
	t.Parallel()

	g, fsys, _ := newSettingsSkeletonProject(t)
	require.NoError(t, afero.WriteFile(fsys, settingsSkeletonRoot+"/.gtb/ignore", []byte("zensical.toml\n"), 0o644))
	g.rules = nil // gtb set builds a new generator per run, which reads the rule afresh

	before := readGenerated(t, fsys, settingsSkeletonRoot+"/zensical.toml")
	require.NoError(t, g.SetSetting(context.Background(), "description", []string{"a better widget"}))

	assert.Equal(t, before, readGenerated(t, fsys, settingsSkeletonRoot+"/zensical.toml"))
}

// TestSetSetting_WritesOnlyWhatTheSettingMoves pins spec 0218 D4: a file
// whose template differs from disk for some other reason is not touched.
func TestSetSetting_WritesOnlyWhatTheSettingMoves(t *testing.T) {
	t.Parallel()

	g, fsys, _ := newSettingsSkeletonProject(t)

	// Stands in for a project an older gtb generated: the file is gtb's (its
	// hash is recorded) but no longer what this gtb renders.
	path := settingsSkeletonRoot + "/.golangci.yaml"
	older := "# rendered by an older gtb\n"
	require.NoError(t, afero.WriteFile(fsys, path, []byte(older), 0o644))

	m, err := g.loadManifest()
	require.NoError(t, err)
	m.Hashes[".golangci.yaml"] = calculateHash([]byte(older))
	require.NoError(t, g.writeManifest(m))

	require.NoError(t, g.SetSetting(context.Background(), "description", []string{"a better widget"}))

	assert.Equal(t, older, readGenerated(t, fsys, path))
}

// The wizard revisit commits through the same step (spec 0218 D1).
func TestApplyAuthorSettings_LeavesNothingForRegenerate(t *testing.T) {
	t.Parallel()

	g, fsys, _ := newSettingsSkeletonProject(t)

	m, err := g.loadManifest()
	require.NoError(t, err)

	cfg := SkeletonConfig{
		Name: m.Properties.Name, Repo: "acme/widget", Host: "gitlab.com", ForgeBackend: forge.GitlabFeature,
		Description: "described by the wizard", Path: settingsSkeletonRoot, Features: m.Properties.Features,
		ReleaseChannel: ReleaseChannelForge, GoVersion: m.Version.Go,
	}
	require.NoError(t, g.ApplyAuthorSettings(context.Background(), cfg))

	afterApply := projectFiles(t, fsys)
	assert.Contains(t, afterApply["zensical.toml"], "described by the wizard")

	require.NoError(t, g.RegenerateProject(context.Background()))
	assert.Empty(t, changedFiles(afterApply, projectFiles(t, fsys)))
}

// A manifest an older gtb wrote lacks fields the commit derives and saves
// first; the skeleton diff must still be against what was on disk.
func TestSetSetting_SyncsAnOlderManifest(t *testing.T) {
	t.Parallel()

	g, fsys, _ := newSettingsSkeletonProject(t)

	m, err := g.loadManifest()
	require.NoError(t, err)
	m.Version.Go = ""
	require.NoError(t, g.writeManifest(m))

	require.NoError(t, g.SetSetting(context.Background(), "description", []string{"a better widget"}))

	assert.Contains(t, readGenerated(t, fsys, settingsSkeletonRoot+"/zensical.toml"), "a better widget")
}

// Regenerate records a seed file's hash from disk, so after one an edited
// README matches its hash; it is still the developer's.
func TestSetSetting_LeavesAnEditedSeedFileAfterRegenerate(t *testing.T) {
	t.Parallel()

	g, fsys, _ := newSettingsSkeletonProject(t)
	path := settingsSkeletonRoot + "/README.md"

	edited := readGenerated(t, fsys, path) + "\nmine\n"
	require.NoError(t, afero.WriteFile(fsys, path, []byte(edited), 0o644))
	require.NoError(t, g.RegenerateProject(context.Background()))

	require.NoError(t, g.SetSetting(context.Background(), "description", []string{"a better widget"}))

	assert.Equal(t, edited, readGenerated(t, fsys, path))
}

// withOverlay records an overlay that last rendered outputs, as a template
// add leaves the manifest.
func withOverlay(t *testing.T, g *Generator, fsys afero.Fs, outputs ...string) {
	t.Helper()

	m, err := g.loadManifest()
	require.NoError(t, err)

	hashes := map[string]string{}
	for _, rel := range outputs {
		content, err := afero.ReadFile(fsys, settingsSkeletonRoot+"/"+rel)
		require.NoError(t, err)
		hashes[rel] = calculateHash(content)
		m.Hashes[rel] = hashes[rel]
	}

	m.Properties.Templates = []TemplateSource{{Name: "house", Type: TemplateSourceLocal, Location: "/overlay", Hashes: hashes}}
	require.NoError(t, g.writeManifest(m))
}

// Spec 0218 leaves overlays to regenerate: set does not replace a file an
// overlay owns with the embedded render.
func TestSetSetting_LeavesAnOverlayOutput(t *testing.T) {
	t.Parallel()

	g, fsys, _ := newSettingsSkeletonProject(t)
	path := settingsSkeletonRoot + "/zensical.toml"
	require.NoError(t, afero.WriteFile(fsys, path, []byte("# the house docs config\n"), 0o644))
	withOverlay(t, g, fsys, "zensical.toml")

	require.NoError(t, g.SetSetting(context.Background(), "description", []string{"a better widget"}))

	assert.Equal(t, "# the house docs config\n", readGenerated(t, fsys, path))
}

// An overlay's replaces: may have removed a skeleton file on purpose, so set
// does not bring it back.
func TestSetSetting_DoesNotCreateAFileInAProjectWithOverlays(t *testing.T) {
	t.Parallel()

	g, fsys, _ := newSettingsSkeletonProject(t)
	withOverlay(t, g, fsys, "README.md")
	require.NoError(t, fsys.Remove(settingsSkeletonRoot+"/zensical.toml"))

	require.NoError(t, g.SetSetting(context.Background(), "description", []string{"a better widget"}))

	exists, err := afero.Exists(fsys, settingsSkeletonRoot+"/zensical.toml")
	require.NoError(t, err)
	assert.False(t, exists)
}

// A field an older manifest lacks is derived on both sides, so the files it
// feeds are not candidates and an edited one draws no warning.
func TestSetSetting_DerivedFieldIsNotAChange(t *testing.T) {
	t.Parallel()

	g, fsys, buf := newSettingsSkeletonProject(t)
	path := settingsSkeletonRoot + "/.goreleaser.yaml"
	require.NoError(t, afero.WriteFile(fsys, path, []byte(readGenerated(t, fsys, path)+"# mine\n"), 0o644))

	m, err := g.loadManifest()
	require.NoError(t, err)
	m.ReleaseSource.Host, m.ReleaseSource.Backend = "", ""
	require.NoError(t, g.writeManifest(m))

	require.NoError(t, g.SetSetting(context.Background(), "description", []string{"a better widget"}))

	assert.False(t, buf.ContainsLevel(logger.WarnLevel, ".goreleaser.yaml"), "%v", buf.Messages())
}

// The withhold list commits through the same step (spec 0218 D1).
func TestSetMCPExposure_LeavesNothingForRegenerate(t *testing.T) {
	t.Parallel()

	g, fsys, _ := newSettingsSkeletonProject(t)

	_, err := g.SetMCPExposure(context.Background(), []string{"update"}, false)
	require.NoError(t, err)

	afterSet := projectFiles(t, fsys)
	require.NoError(t, g.RegenerateProject(context.Background()))
	assert.Empty(t, changedFiles(afterSet, projectFiles(t, fsys)))
}

// A hosted project moves to the static channel by setting its base URL and
// then its type (#143).
func TestSetSetting_MovesAHostedProjectToTheStaticChannel(t *testing.T) {
	t.Parallel()

	g, fsys, _ := newSettingsSkeletonProject(t)
	ctx := context.Background()

	require.NoError(t, g.SetSetting(ctx, "release_source.static.base_url", []string{"https://pkg.acme.dev/widget"}))
	require.NoError(t, g.SetSetting(ctx, "release_source.type", []string{"static"}))

	m, err := g.loadManifest()
	require.NoError(t, err)
	assert.Equal(t, props.ReleaseSourceStatic, m.ReleaseSource.Type)

	afterSet := projectFiles(t, fsys)
	require.NoError(t, g.RegenerateProject(ctx))
	assert.Empty(t, changedFiles(afterSet, projectFiles(t, fsys)))
}
