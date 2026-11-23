package generator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Generating a command again keeps every recorded setting the second call
// leaves out, in the manifest and in cmd.go alike (#230).
func TestGenerate_AgainKeepsWhatItLeavesOut(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		parent, dir string
		again       Config
	}{
		"top level":              {parent: "root", dir: "deploy"},
		"nested":                 {parent: "ops", dir: "ops/deploy"},
		"top level, with a flag": {parent: "root", dir: "deploy", again: Config{Flags: []string{"dry:bool:Dry run"}}},
		"nested, with a flag":    {parent: "ops", dir: "ops/deploy", again: Config{Flags: []string{"dry:bool:Dry run"}}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			const path = "/work"

			p := setupTestProject(t, path)
			if tc.parent != "root" {
				generateCmd(t, p, path, tc.parent, "root")
			}

			generateCmd(t, p, path, "deploy", tc.parent)

			load := func() *ManifestCommand {
				g := New(p, &Config{Path: path, Name: "deploy", Parent: tc.parent})
				cmd, err := g.findManifestCommand()
				require.NoError(t, err)

				return cmd
			}

			excluded, readOnly := false, true
			g := New(p, &Config{Path: path, Name: "deploy", Parent: tc.parent})
			m, err := g.loadManifest()
			require.NoError(t, err)

			recorded := walkCommandPath(m.Commands, joinCommandPath(g.getParentPathParts(), "deploy"))
			recorded.Aliases = []string{"ship"}
			recorded.Args = "ExactArgs(1)"
			recorded.Hidden = true
			recorded.LongDescription = "Ships it."
			recorded.MCPEnabled = &excluded
			recorded.MCPHints = &ManifestMCPHints{ReadOnly: &readOnly}
			require.NoError(t, g.marshalManifestFile(ManifestPathFor(path), m))

			again := tc.again
			again.Path, again.Name, again.Parent, again.Force = path, "deploy", tc.parent, true
			require.NoError(t, New(p, &again).Generate(context.Background()))

			got := load()
			assert.Equal(t, "deploy command", string(got.Description))
			assert.Equal(t, "Ships it.", string(got.LongDescription))
			assert.Equal(t, []string{"ship"}, got.Aliases)
			assert.Equal(t, "ExactArgs(1)", got.Args)
			assert.True(t, got.Hidden)
			assert.Equal(t, &excluded, got.MCPEnabled)
			assert.Equal(t, &ManifestMCPHints{ReadOnly: &readOnly}, got.MCPHints)

			src, err := afero.ReadFile(p.FS, filepath.Join(path, "pkg", "cmd", tc.dir, "cmd.go"))
			require.NoError(t, err)

			for _, want := range []string{
				`"deploy command"`, `"Ships it."`, `Aliases: []string{"ship"}`, "cobra.ExactArgs(1)",
				"Hidden:", "ExcludeFromMCP", "AnnotateMCP",
			} {
				assert.Contains(t, string(src), want)
			}

			require.NoError(t, New(p, &Config{Path: path, Force: true}).RegenerateProject(context.Background()))

			after, err := afero.ReadFile(p.FS, filepath.Join(path, "pkg", "cmd", tc.dir, "cmd.go"))
			require.NoError(t, err)
			assert.Equal(t, string(src), string(after), "regenerate changes nothing")
		})
	}
}

func TestGenerate_AgainTakesWhatItStates(t *testing.T) {
	t.Parallel()

	const path = "/work"

	p := setupTestProject(t, path)
	exposed, excluded := true, false

	for _, c := range []Config{
		{Short: "one", Long: "Long one.", Aliases: []string{"a"}, Args: "ExactArgs(1)", MCPEnabled: &exposed, MCPHints: setup.MCPHints{Title: "One"}},
		{Short: "two", Long: "Long two.", Aliases: []string{"b"}, Args: "ExactArgs(2)", MCPEnabled: &excluded, MCPHints: setup.MCPHints{Title: "Two"}, Hidden: true},
	} {
		c.Path, c.Name, c.Parent, c.Force = path, "deploy", "root", true
		require.NoError(t, New(p, &c).Generate(context.Background()))
	}

	g := New(p, &Config{Path: path, Name: "deploy", Parent: "root"})
	got, err := g.findManifestCommand()
	require.NoError(t, err)
	assert.Equal(t, "two", string(got.Description))
	assert.Equal(t, "Long two.", string(got.LongDescription))
	assert.Equal(t, []string{"b"}, got.Aliases)
	assert.Equal(t, "ExactArgs(2)", got.Args)
	assert.Equal(t, &excluded, got.MCPEnabled)
	assert.Equal(t, "Two", got.MCPHints.Title)
	assert.True(t, got.Hidden)

	src, err := afero.ReadFile(p.FS, path+"/pkg/cmd/deploy/cmd.go")
	require.NoError(t, err)
	assert.Contains(t, string(src), "setup.ExcludeFromMCP")
}

func TestGenerate_AgainLongDescription(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		first, again Config
		want         string
	}{
		"stated then left out":             {Config{Short: "one", Long: "Long."}, Config{Short: "one"}, "Long."},
		"stated then a new short":          {Config{Short: "one", Long: "Long."}, Config{Short: "two"}, "Long."},
		"never stated follows the short":   {Config{Short: "one"}, Config{Short: "two"}, "two"},
		"never stated, then neither given": {Config{Short: "one"}, Config{}, "one"},
		"never stated, then stated":        {Config{Short: "one"}, Config{Short: "one", Long: "Long."}, "Long."},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			const path = "/work"

			p := setupTestProject(t, path)

			for _, c := range []Config{tc.first, tc.again} {
				c.Path, c.Name, c.Parent, c.Force = path, "deploy", "root", true
				require.NoError(t, New(p, &c).Generate(context.Background()))
			}

			got, err := New(p, &Config{Path: path, Name: "deploy", Parent: "root"}).findManifestCommand()
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got.LongDescription))

			src, err := afero.ReadFile(p.FS, path+"/pkg/cmd/deploy/cmd.go")
			require.NoError(t, err)
			assert.Regexp(t, `Long:\s+"`+tc.want+`"`, string(src))
		})
	}
}

func TestGenerateDryRun_AgainPreviewsNoLoss(t *testing.T) {
	t.Parallel()

	const path = "/work"

	p := setupTestProject(t, path)
	excluded := false

	require.NoError(t, New(p, &Config{
		Path: path, Name: "deploy", Parent: "root", Short: "dep", Aliases: []string{"ship"}, MCPEnabled: &excluded, Force: true,
	}).Generate(context.Background()))

	result, err := New(p, &Config{Path: path, Name: "deploy", Parent: "root", Force: true, DryRun: true}).GenerateDryRun(context.Background())
	require.NoError(t, err)

	for _, f := range result.Modified {
		assert.NotContains(t, f.Path, "pkg/cmd/deploy/cmd.go", f.Diff)
	}
}
