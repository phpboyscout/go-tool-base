package generate

import (
	"bytes"
	"context"
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestWizard_MCPPage pins spec 0202 D9: the MCP page is asked only with the
// feature selected, the mode select binds to MCPMode, the surface list is
// asked on a revisit only, and both clear when the feature is unticked.
func TestWizard_MCPPage(t *testing.T) {
	t.Parallel()

	t.Run("asks the mode with the feature", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Features: []string{"update", "mcp"}}
		f, m := startWizard(o)
		m = driveUntilKey(f, m, o, "mcp-mode")
		require.Equal(t, "mcp-mode", f.GetFocusedField().GetKey())

		m = typeAnswer(m, "↓")
		_, ok := advance(f, m)
		require.True(t, ok)
		assert.Equal(t, string(props.MCPDirect), o.MCPMode)
	})

	t.Run("hidden without the feature and cleared", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Features: []string{"update"}, MCPMode: string(props.MCPDirect), MCPExposed: []string{"post"}}
		f, m := startWizard(o)
		m = driveUntilKey(f, m, o, "mcp-mode")
		assert.NotEqual(t, "mcp-mode", f.GetFocusedField().GetKey())

		for i := 0; i < 20 && f.State != huh.StateCompleted; i++ {
			m, _ = advance(f, m)
		}

		require.Equal(t, huh.StateCompleted, f.State)
		require.NoError(t, o.afterWizard())
		assert.Empty(t, o.MCPMode)
		assert.Empty(t, o.MCPExposed)
	})

	t.Run("the surface is asked on a revisit only", func(t *testing.T) {
		t.Parallel()

		choices := []mcpCommandChoice{{Path: "post", Exposed: true}, {Path: "post/due", Exposed: false}}

		first := &SkeletonOptions{Features: []string{"update", "mcp"}, mcpCommands: choices}
		f, m := startWizard(first)
		driveUntilKey(f, m, first, "mcp-surface")
		assert.NotEqual(t, "mcp-surface", f.GetFocusedField().GetKey(), "no command exists on a first run")

		again := &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "github", hosted: true,
			Features: []string{"update", "mcp"}, revisit: true, mcpCommands: choices, MCPExposed: []string{"post"}}
		f, m = startWizard(again)
		m = driveUntilKey(f, m, again, "mcp-mode")
		m, ok := advance(f, m)
		require.True(t, ok)
		require.Equal(t, "mcp-surface", f.GetFocusedField().GetKey())

		_, ok = advance(f, m)
		require.True(t, ok)
		assert.Equal(t, []string{"post"}, again.MCPExposed, "the pre-ticked surface is kept when accepted")
		assert.Empty(t, again.mcpSurfaceChanges())
	})
}

// TestMCPCommandChoices: the walk resolves each command's effective exposure
// the way the generated markers do, joins slash paths, and leaves a protected
// command out.
func TestMCPCommandChoices(t *testing.T) {
	t.Parallel()

	off, on := false, true
	commands := []generator.ManifestCommand{
		{Name: "post", MCPEnabled: &off, Commands: []generator.ManifestCommand{
			{Name: "due"},
			{Name: "now", MCPEnabled: &on},
		}},
		{Name: "status"},
		{Name: "danger", Protected: &on},
	}

	assert.Equal(t, []mcpCommandChoice{
		{Path: "post", Exposed: false},
		{Path: "post/due", Exposed: false},
		{Path: "post/now", Exposed: true},
		{Path: "status", Exposed: true},
	}, mcpCommandChoices(commands))
}

// TestMCPSurfaceChanges: only a changed tick becomes a change, and nothing
// changes when the feature is off.
func TestMCPSurfaceChanges(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Features: []string{"mcp"},
		mcpCommands: []mcpCommandChoice{{Path: "post", Exposed: true}, {Path: "status", Exposed: false}, {Path: "same", Exposed: true}},
		MCPExposed:  []string{"status", "same"}}

	assert.Equal(t, []mcpSurfaceChange{{Path: "post", Exposed: false}, {Path: "status", Exposed: true}}, o.mcpSurfaceChanges())
	assert.Equal(t, "mcp surface: post withheld", mcpSurfaceChange{Path: "post"}.describe())
	assert.Equal(t, "mcp surface: status exposed", mcpSurfaceChange{Path: "status", Exposed: true}.describe())

	o.Features = nil
	assert.Empty(t, o.mcpSurfaceChanges())
}

const revisitManifestWithCommands = revisitManifestHead +
	"commands:\n  - name: post\n    description: post\n  - name: status\n    description: status\n"

func revisitProjectWithCommands(t *testing.T) (*props.Props, afero.Fs) {
	t.Helper()

	return revisitProjectFrom(t, revisitManifestWithCommands)
}

// TestWizardRun_Surface: a revisit that unticks a command records mcp_enabled
// false through the same setter as gtb disable mcp <path>, and a dry run
// names the change and writes nothing (spec 0202 D9).
func TestWizardRun_Surface(t *testing.T) {
	t.Parallel()

	t.Run("applies the surface", func(t *testing.T) {
		t.Parallel()

		p, _ := revisitProjectWithCommands(t)
		o := &WizardOptions{Path: "/work", runForm: func(so *SkeletonOptions) error {
			require.Equal(t, []string{"post", "status"}, so.MCPExposed, "both exposed before")
			so.MCPExposed = []string{"status"}

			return so.afterWizard()
		}}

		require.NoError(t, o.Run(context.Background(), p, &bytes.Buffer{}))

		m, err := generator.New(p, &generator.Config{Path: "/work"}).LoadManifest()
		require.NoError(t, err)
		require.NotNil(t, m.Commands[0].MCPEnabled)
		assert.False(t, *m.Commands[0].MCPEnabled, "post is withheld")
		assert.Nil(t, m.Commands[1].MCPEnabled, "status is untouched")
	})

	t.Run("dry run names the change and writes nothing", func(t *testing.T) {
		t.Parallel()

		p, _ := revisitProjectWithCommands(t)
		o := &WizardOptions{Path: "/work", DryRun: true, runForm: func(so *SkeletonOptions) error {
			so.MCPExposed = []string{"status"}
			so.MCPMode = string(props.MCPDirect)

			return so.afterWizard()
		}}

		var out bytes.Buffer
		require.NoError(t, o.Run(context.Background(), p, &out))
		assert.Contains(t, out.String(), "mcp surface: post withheld")
		assert.Contains(t, out.String(), "mcp.mode:")

		m, err := generator.New(p, &generator.Config{Path: "/work"}).LoadManifest()
		require.NoError(t, err)
		assert.Nil(t, m.Commands[0].MCPEnabled)
		assert.Empty(t, m.Properties.MCP.Mode)
	})
}
