package mcp

import (
	"log/slog"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// go/mcp has no hook that runs before a binding is served, so the stale-path
// check is attached to its subcommands by name. A go/mcp release that adds one
// which binds the tree must fail here, not serve unchecked (spec 0213 D8).
func TestServingCommandsAreEveryRunnableChildOfMCP(t *testing.T) {
	t.Parallel()

	p := &props.Props{Tool: props.Tool{Name: "tool"}, Logger: logger.NewNoop()}

	var unaccounted []string

	for _, leaf := range runnableLeaves(NewCmdMCP(p, &slog.LevelVar{}).Command, "") {
		if !servingCommands[leaf] && !editsAClientConfig[leaf] {
			unaccounted = append(unaccounted, leaf)
		}
	}

	assert.Empty(t, unaccounted, "a new go/mcp command must be guarded, or recorded here as not serving")

	leaves := runnableLeaves(NewCmdMCP(p, &slog.LevelVar{}).Command, "")
	for name := range servingCommands {
		assert.Contains(t, leaves, name, "a guarded name that is not a runnable leaf guards nothing")
	}
}

// editsAClientConfig are go/mcp's per-client commands, which write a client's
// config file and never bind the tree.
var editsAClientConfig = map[string]bool{
	"claude/enable": true, "claude/disable": true, "claude/list": true,
	"cursor/enable": true, "cursor/disable": true, "cursor/list": true,
	"vscode/enable": true, "vscode/disable": true, "vscode/list": true,
}

// runnableLeaves names every runnable leaf under cmd by its path from cmd, so a
// nested serving command shows as "serve/http" and matches no guarded name.
func runnableLeaves(cmd *cobra.Command, prefix string) []string {
	var leaves []string

	for _, sub := range cmd.Commands() {
		path := prefix + sub.Name()

		if sub.HasSubCommands() {
			leaves = append(leaves, runnableLeaves(sub, path+"/")...)
		} else if sub.Runnable() {
			leaves = append(leaves, path)
		}
	}

	return leaves
}

func TestGuardServing_LeavesACommandWithNothingToRunAlone(t *testing.T) {
	t.Parallel()

	serving := &cobra.Command{Use: "start"}

	guardServing(&props.Props{Tool: props.Tool{Name: "tool"}}, serving)

	assert.Nil(t, serving.RunE, "wrapping nothing would make it runnable and panic when run")
	assert.False(t, serving.Runnable())
}

func TestGuardServing_CoversARunOnlyCommand(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		withhold []string
		runs     bool
	}{
		{name: "a stale path refuses", withhold: []string{"config/nope"}},
		{name: "a clean list runs", withhold: []string{"config"}, runs: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ran := false
			serving := &cobra.Command{Use: "start", Run: func(*cobra.Command, []string) { ran = true }}
			root := &cobra.Command{Use: "tool"}
			root.AddCommand(&cobra.Command{Use: "config", Run: func(*cobra.Command, []string) {}}, serving)

			guardServing(&props.Props{Tool: props.Tool{Name: "tool", MCP: props.MCPConfig{Withhold: tc.withhold}}}, serving)

			require.Nil(t, serving.Run)
			err := serving.RunE(serving, nil)

			assert.Equal(t, tc.runs, ran)
			if tc.runs {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrWithheldPathNotFound)
			}
		})
	}
}
