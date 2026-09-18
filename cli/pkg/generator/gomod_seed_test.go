package generator

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/gomod"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// settledGoMod is what tidy leaves behind on a scaffold with the github
// forge and one chat provider: direct lines, indirects, the tool block.
const settledGoMod = `module github.com/acme/mytool

go 1.27.1

tool (
	gitlab.com/phpboyscout/go-tool-base/cmd/changelog
	gitlab.com/phpboyscout/go-tool-base/cmd/docs
)

require (
	github.com/acme/shared v1.2.3
	github.com/spf13/afero v1.15.0
	github.com/spf13/cobra v1.10.2
	gitlab.com/phpboyscout/go-tool-base v0.42.0
	gitlab.com/phpboyscout/go/chat-anthropic v0.14.1
	gitlab.com/phpboyscout/go/errorhandling v0.6.0
	gitlab.com/phpboyscout/go/forge-github v0.22.0
)

require golang.org/x/text v0.31.0 // indirect
`

// seededProject is a settled project whose manifest names the github forge
// and the claude provider, regenerated with the toolchain stubbed out: what a
// machine without Go sees.
func seededProject(t *testing.T, manifestFeatures string) (*Generator, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	root := "/work"
	manifest := "properties:\n  name: mytool\n  module_path: github.com/acme/mytool\n  features:\n" + manifestFeatures +
		"  chat:\n    providers: [claude]\n    default:\n      provider: claude\nrelease_source:\n  type: github\n  backend: github\n  host: github.com\n  owner: acme\n  repo: mytool\nversion:\n  gtb: v0.42.0\n  go: 1.27.1\ncommands: []\n"

	require.NoError(t, fs.MkdirAll(root+"/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte(manifest), 0o644))
	require.NoError(t, afero.WriteFile(fs, root+"/go.mod", []byte(settledGoMod), 0o644))
	require.NoError(t, fs.MkdirAll(root+"/pkg/cmd/root", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/pkg/cmd/root/cmd.go", []byte("package root\nfunc NewCmdRoot(p interface{}) {}\n"), 0o644))

	g, _ := newPureGenerator(t, &Config{Path: root, Overwrite: OverwriteAllow, NoVerify: true})
	g.props.FS = fs
	g.versions = gomod.MapSource{
		"gitlab.com/phpboyscout/go/forge-gitlab":   "v0.21.0",
		"gitlab.com/phpboyscout/go/forge-github":   "v0.22.0",
		"gitlab.com/phpboyscout/go/chat-anthropic": "v0.14.1",
		"gitlab.com/phpboyscout/go/errorhandling":  "v0.6.0",
		"github.com/spf13/cobra":                   "v1.10.2",
		"github.com/spf13/afero":                   "v1.15.0",
	}

	return g, fs
}

const githubOnly = "    - name: ai\n      enabled: true\n    - name: github\n      enabled: true\n"

// TestRegenerateProject_DropsThePrePhase4ToolDirectives (#86): a v0.42.0
// scaffold's go.mod carried tool lines for gtb, golangci-lint and mockery;
// spec 0197 D12 made those installed binaries, and the migration note says
// an existing project loses the lines on its next regenerate.
func TestRegenerateProject_DropsThePrePhase4ToolDirectives(t *testing.T) {
	t.Parallel()

	g, fs := seededProject(t, githubOnly)
	old := strings.Replace(settledGoMod, "tool (\n", "tool (\n\tgithub.com/golangci/golangci-lint/cmd/golangci-lint\n\tgithub.com/vektra/mockery/v3\n\tgitlab.com/phpboyscout/go-tool-base/cli/cmd/gtb\n", 1)
	require.NoError(t, afero.WriteFile(fs, "/work/go.mod", []byte(old), 0o644))

	require.NoError(t, g.RegenerateProject(context.Background()))

	out, err := afero.ReadFile(fs, "/work/go.mod")
	require.NoError(t, err)

	s := string(out)
	assert.NotContains(t, s, "cli/cmd/gtb")
	assert.NotContains(t, s, "golangci-lint")
	assert.NotContains(t, s, "mockery")
	assert.Contains(t, s, "gitlab.com/phpboyscout/go-tool-base/cmd/changelog", "the framework's own tools stay")
	assert.Contains(t, s, "gitlab.com/phpboyscout/go-tool-base/cmd/docs")
}

// TestRegenerateProject_RaisesAnOwnedAdapterToTheVersionThisGtbKnows (#87,
// spec 0200 D9): the adapters move with the framework, so a present adapter
// line below the version the running gtb links is raised on regenerate; a
// module the generator does not own keeps whatever it holds.
func TestRegenerateProject_RaisesAnOwnedAdapterToTheVersionThisGtbKnows(t *testing.T) {
	t.Parallel()

	g, fs := seededProject(t, githubOnly)
	g.versions = gomod.MapSource{
		"gitlab.com/phpboyscout/go/forge-github":   "v0.25.0",
		"gitlab.com/phpboyscout/go/chat-anthropic": "v0.14.2",
		"github.com/spf13/cobra":                   "v1.99.0",
	}

	require.NoError(t, g.RegenerateProject(context.Background()))

	out, err := afero.ReadFile(fs, "/work/go.mod")
	require.NoError(t, err)

	s := string(out)
	assert.Contains(t, s, "gitlab.com/phpboyscout/go/forge-github v0.25.0", "the forge adapter is raised")
	assert.Contains(t, s, "gitlab.com/phpboyscout/go/chat-anthropic v0.14.2", "the chat adapter is raised")
	assert.Contains(t, s, "github.com/spf13/cobra v1.10.2", "a module the generator does not own is left alone")
	assert.Contains(t, s, "github.com/acme/shared v1.2.3")
}

// TestRegenerateProject_RaisesTheFrameworkToTheRunningGtb (F10 of the
// v0.43.0 manual round): the framework is the module the regenerated code is
// written against, so a present line below the running gtb's version is a
// floor like an owned adapter's (spec 0200 D9). It was the one line the seed
// left alone, and a v0.42.0 project regenerated by a newer gtb failed
// typecheck on the framework symbols the new templates use until the author
// ran go get by hand.
func TestRegenerateProject_RaisesTheFrameworkToTheRunningGtb(t *testing.T) {
	t.Parallel()

	g, fs := seededProject(t, githubOnly)
	g.props.Version = version.NewInfo("v1.0.0", "", "")

	require.NoError(t, g.RegenerateProject(context.Background()))

	out, err := afero.ReadFile(fs, "/work/go.mod")
	require.NoError(t, err)
	assert.Contains(t, string(out), "gitlab.com/phpboyscout/go-tool-base v1.0.0", "the project pinned v0.42.0; the running gtb is v1.0.0")
	assert.NotContains(t, string(out), "go-tool-base v0.42.0")
}

// TestSeedGoMod_LeavesAFrameworkLineAboveTheGivenVersion: a floor raises,
// never lowers. regenerate's version gate refuses an older gtb before the
// seed runs, so this is asserted at the seed, where a go.mod already ahead
// of the version handed in keeps its line.
func TestSeedGoMod_LeavesAFrameworkLineAboveTheGivenVersion(t *testing.T) {
	t.Parallel()

	g, fs := seededProject(t, githubOnly)
	require.NoError(t, g.seedGoMod("/work", "example.com/mytool", "1.27.1", "v0.41.0"))

	out, err := afero.ReadFile(fs, "/work/go.mod")
	require.NoError(t, err)
	assert.Contains(t, string(out), "gitlab.com/phpboyscout/go-tool-base v0.42.0", "a line above the version given is left alone")
}

// TestRegenerateProject_NoVerifyKeepsEveryRequirement (spec 0200 D1): the
// measured hole. A regenerate with the toolchain absent left go.mod with no
// require line at all, because the file was re-rendered from a template that
// carries none and tidy was what put them back.
func TestRegenerateProject_NoVerifyKeepsEveryRequirement(t *testing.T) {
	t.Parallel()

	g, fs := seededProject(t, githubOnly)
	require.NoError(t, g.RegenerateProject(context.Background()))

	out, err := afero.ReadFile(fs, "/work/go.mod")
	require.NoError(t, err)

	s := string(out)
	for _, line := range []string{
		"github.com/acme/shared v1.2.3",
		"gitlab.com/phpboyscout/go-tool-base v0.42.0",
		"gitlab.com/phpboyscout/go/chat-anthropic v0.14.1",
		"gitlab.com/phpboyscout/go/forge-github v0.22.0",
		"golang.org/x/text v0.31.0 // indirect",
		"gitlab.com/phpboyscout/go-tool-base/cmd/docs",
	} {
		assert.Contains(t, s, line)
	}
}

// TestRegenerateProject_EnableAddsAndDisableDropsTheAdapterLine (D2, D3):
// enabling a forge on a machine without Go adds its require line at the
// version gtb was built with; disabling it drops the line the generator
// added, and only that line.
func TestRegenerateProject_EnableAddsAndDisableDropsTheAdapterLine(t *testing.T) {
	t.Parallel()

	withGitlab := githubOnly + "    - name: gitlab\n      enabled: true\n"
	g, fs := seededProject(t, withGitlab)
	require.NoError(t, g.RegenerateProject(context.Background()))

	out, err := afero.ReadFile(fs, "/work/go.mod")
	require.NoError(t, err)
	assert.Contains(t, string(out), "gitlab.com/phpboyscout/go/forge-gitlab v0.21.0", "enabled: seeded at the build-info version")

	// Disable it again: the manifest no longer names gitlab.
	manifest, err := afero.ReadFile(fs, "/work/.gtb/manifest.yaml")
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/manifest.yaml", []byte(replaceOnce(t, string(manifest), "    - name: gitlab\n      enabled: true\n", "")), 0o644))
	require.NoError(t, g.RegenerateProject(context.Background()))

	out, err = afero.ReadFile(fs, "/work/go.mod")
	require.NoError(t, err)
	assert.NotContains(t, string(out), "forge-gitlab", "disabled: the generator's own line goes")
	assert.Contains(t, string(out), "github.com/acme/shared v1.2.3", "a developer's line is not the generator's to drop")
	assert.Contains(t, string(out), "gitlab.com/phpboyscout/go/forge-github v0.22.0")
}

func replaceOnce(t *testing.T, s, old, repl string) string {
	t.Helper()

	require.Contains(t, s, old)

	return strings.Replace(s, old, repl, 1)
}
