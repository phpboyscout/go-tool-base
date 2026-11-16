package mcp

import (
	"log/slog"
	"maps"
	"slices"
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

	var runnable []string

	for _, sub := range NewCmdMCP(p, &slog.LevelVar{}).Commands() {
		if sub.Runnable() && !sub.HasSubCommands() {
			runnable = append(runnable, sub.Name())
		}
	}

	slices.Sort(runnable)

	assert.Equal(t, slices.Sorted(maps.Keys(servingCommands)), runnable)
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
