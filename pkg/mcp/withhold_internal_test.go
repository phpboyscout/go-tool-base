package mcp

import (
	"log/slog"
	"reflect"
	"runtime"
	"strings"
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
func TestEveryRunnableMCPCommandIsGuardedOrKnownNotToServe(t *testing.T) {
	t.Parallel()

	p := &props.Props{Tool: props.Tool{Name: "tool"}, Logger: logger.NewNoop()}

	var unaccounted []string

	mcpCmd := NewCmdMCP(p, &slog.LevelVar{}).Command
	runnable := runnableCommands(mcpCmd, "")

	for _, path := range runnable {
		if !servingCommands[path] && !editsAClientConfig[path] && !printsUsage[path] {
			unaccounted = append(unaccounted, path)
		}
	}

	assert.Empty(t, unaccounted, "a new go/mcp command must be guarded, or recorded here as not serving")

	for name := range servingCommands {
		assert.Contains(t, runnable, name, "a guarded name that is not runnable guards nothing")
	}

	for path := range printsUsage {
		assert.Equal(t, groupRunE, funcName(commandAt(t, mcpCmd, path).RunE), "%q must still only print usage", path)
	}
}

const groupRunE = "gitlab.com/phpboyscout/go/mcp/cli.groupRunE"

func funcName(fn func(*cobra.Command, []string) error) string {
	if fn == nil {
		return ""
	}

	return runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
}

func commandAt(t *testing.T, cmd *cobra.Command, path string) *cobra.Command {
	t.Helper()

	if path == "" {
		return cmd
	}

	found, _, err := cmd.Find(strings.Split(path, "/"))
	require.NoError(t, err)

	return found
}

// editsAClientConfig are go/mcp's per-client commands, which write a client's
// config file and never bind the tree.
var editsAClientConfig = map[string]bool{
	"claude/enable": true, "claude/disable": true, "claude/list": true,
	"cursor/enable": true, "cursor/disable": true, "cursor/list": true,
	"vscode/enable": true, "vscode/disable": true, "vscode/list": true,
}

// printsUsage are the mcp command itself ("") and go/mcp's per-client groups,
// whose RunE prints usage (groupRunE, go/mcp v0.2.4 cli/command.go:109).
var printsUsage = map[string]bool{"": true, "claude": true, "cursor": true, "vscode": true}

// runnableCommands names every runnable command from cmd down by its path from
// cmd, groups and cmd itself included ("" is cmd), so a nested serving command
// shows as "serve/http" and matches no guarded name.
func runnableCommands(cmd *cobra.Command, path string) []string {
	var found []string

	if cmd.Runnable() {
		found = append(found, path)
	}

	for _, sub := range cmd.Commands() {
		child := sub.Name()
		if path != "" {
			child = path + "/" + child
		}

		found = append(found, runnableCommands(sub, child)...)
	}

	return found
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
