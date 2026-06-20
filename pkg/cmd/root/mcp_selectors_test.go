package root

import (
	"testing"

	"github.com/njayp/ophis"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// exposes runs the single MCP selector's CmdSelector against cmd, mirroring how
// ophis decides whether to register a command as a tool.
func exposes(t *testing.T, sels []ophis.Selector, cmd *cobra.Command) bool {
	t.Helper()

	require.Len(t, sels, 1)
	require.NotNil(t, sels[0].CmdSelector)

	return sels[0].CmdSelector(cmd)
}

func TestMCPSelectors_GatesExcludedSubtreeWithOverride(t *testing.T) {
	t.Parallel()

	// root
	//  ├── post           (excluded)
	//  │    ├── due        inherit -> excluded
	//  │    └── status     (exposed) -> override
	//  │         └── watch inherit -> exposed
	//  └── list           inherit -> exposed (sibling, untouched)
	rootCmd := &cobra.Command{Use: "tool"}
	post := setup.ExcludeFromMCP(&setup.Command{Command: &cobra.Command{Use: "post"}}).Command
	due := &cobra.Command{Use: "due"}
	status := setup.IncludeInMCP(&setup.Command{Command: &cobra.Command{Use: "status"}}).Command
	watch := &cobra.Command{Use: "watch"}
	list := &cobra.Command{Use: "list"}

	rootCmd.AddCommand(post, list)
	post.AddCommand(due, status)
	status.AddCommand(watch)

	sels := mcpSelectors()

	assert.True(t, exposes(t, sels, rootCmd), "root is exposed by default")
	assert.False(t, exposes(t, sels, post), "explicitly excluded command is gated")
	assert.False(t, exposes(t, sels, due), "child inherits excluded from parent")
	assert.True(t, exposes(t, sels, status), "explicit exposed overrides excluded ancestor")
	assert.True(t, exposes(t, sels, watch), "descendant inherits the exposed override")
	assert.True(t, exposes(t, sels, list), "unmarked sibling subtree is untouched")
}

func TestMCPSelectors_NothingMarkedExposesEverything(t *testing.T) {
	t.Parallel()

	rootCmd := &cobra.Command{Use: "tool"}
	a := &cobra.Command{Use: "a"}
	b := &cobra.Command{Use: "b"}
	rootCmd.AddCommand(a)
	a.AddCommand(b)

	sels := mcpSelectors()

	// With no markers, every command resolves to exposed — equivalent to
	// ophis' nil-selector "expose all" default.
	for _, c := range []*cobra.Command{rootCmd, a, b} {
		assert.Truef(t, exposes(t, sels, c), "%q should be exposed when nothing is marked", c.Use)
	}
}
