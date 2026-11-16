package mcp_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	mcpcobra "gitlab.com/phpboyscout/go/mcp/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/mcp"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func leaf(use string) *cobra.Command {
	return &cobra.Command{Use: use, Short: use, Run: func(*cobra.Command, []string) {}}
}

// surface binds a tree holding a built-in with two subcommands, a command
// attached from another module, and a group whose one child is stamped as
// included, and returns the names of the operations the list leaves.
func surface(t *testing.T, withhold ...string) []string {
	t.Helper()

	root := &cobra.Command{Use: "tool"}

	config := setup.Wrap(props.ConfigCmd, &cobra.Command{Use: "config", Short: "c", RunE: setup.GroupRunE})
	config.Register(setup.Wrap(props.ConfigCmd, leaf("get")), setup.Wrap(props.ConfigCmd, leaf("trust")))

	keys := setup.Wrap("", &cobra.Command{Use: "keys", Short: "k", RunE: setup.GroupRunE})
	keys.Register(setup.Wrap("", leaf("mint")), setup.IncludeInMCP(setup.Wrap("", leaf("publish"))))

	root.AddCommand(config.Command, keys.Command, setup.Wrap("", leaf("sign")).Command, leaf("post"))

	p := &props.Props{Tool: props.Tool{Name: "tool", MCP: props.MCPConfig{Withhold: withhold}}, Logger: logger.NewNoop()}

	binding, err := mcpcobra.Bind(root, append(mcp.BindingOptions(p), mcpcobra.WithExecutable(os.Args[0]))...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = binding.Shutdown(context.Background()) })

	names := make([]string, 0, len(binding.Operations()))
	for _, operation := range binding.Operations() {
		names = append(names, operation.Definition.Name)
	}

	slices.Sort(names)

	return names
}

// Spec 0213 D2: a listed path covers its subtree and no annotation overrules it.
func TestBindingOptions_WithholdsListedCommands(t *testing.T) {
	t.Parallel()

	everything := []string{"tool_config_get", "tool_config_trust", "tool_keys_mint", "tool_keys_publish", "tool_post", "tool_sign"}

	for name, tc := range map[string]struct {
		withhold []string
		gone     []string
	}{
		"an empty list withholds nothing":                  {},
		"a built-in's subcommand":                          {withhold: []string{"config/trust"}, gone: []string{"tool_config_trust"}},
		"a command attached from another module":           {withhold: []string{"sign"}, gone: []string{"tool_sign"}},
		"a command the tool wrote itself":                  {withhold: []string{"post"}, gone: []string{"tool_post"}},
		"a group takes its subtree, an included child too": {withhold: []string{"keys"}, gone: []string{"tool_keys_mint", "tool_keys_publish"}},
		"a command stamped as included":                    {withhold: []string{"keys/publish"}, gone: []string{"tool_keys_publish"}},
		"several paths":                                    {withhold: []string{"sign", "config/trust", "keys"}, gone: []string{"tool_sign", "tool_config_trust", "tool_keys_mint", "tool_keys_publish"}},
		"a path is whole segments, not a prefix":           {withhold: []string{"key", "config/tru", "pos"}},
		"a path is anchored at the root":                   {withhold: []string{"trust", "publish"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			want := slices.DeleteFunc(slices.Clone(everything), func(operation string) bool { return slices.Contains(tc.gone, operation) })
			assert.Equal(t, want, surface(t, tc.withhold...))
		})
	}
}

// Spec 0213 D8: a typo guarding a command fails the author's first run
// instead of shipping.
func TestNewCmdMCP_RefusesToServeOnAStalePath(t *testing.T) {
	t.Parallel()

	for _, serving := range []string{"start", "stream", "tools"} {
		t.Run(serving, func(t *testing.T) {
			t.Parallel()

			err := runMCP(t, props.Tool{Name: "tool", MCP: props.MCPConfig{Withhold: []string{"config/get", "sgin", "config/nope"}}}, serving)
			require.ErrorIs(t, err, mcp.ErrWithheldPathNotFound)
			assert.Contains(t, err.Error(), "sgin")
			assert.Contains(t, err.Error(), "config/nope")
			assert.NotContains(t, err.Error(), "config/get", "a path that names a command is not reported")
		})
	}
}

func runMCP(t *testing.T, author props.Tool, args ...string) error {
	t.Helper()

	root, _ := toolFor(t, author)
	root.SetArgs(append([]string{"mcp"}, args...))
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	return root.Execute()
}

func exportedNames(t *testing.T, author props.Tool) ([]string, error) {
	t.Helper()

	out := filepath.Join(t.TempDir(), "tools.json")

	if err := runMCP(t, author, "tools", "--output", out); err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(out)
	require.NoError(t, err)

	return toolNames(t, []byte(`{"tools":`+string(raw)+`}`)), nil
}

func TestNewCmdMCP_ServesWithTheListApplied(t *testing.T) {
	t.Parallel()

	names, err := exportedNames(t, props.Tool{Name: "tool", MCP: props.MCPConfig{Withhold: []string{"config"}}})
	require.NoError(t, err)
	assert.Equal(t, []string{"tool_post"}, names)
}

// The root registers no command for a feature that is off, so its path
// matching nothing is not a typo.
func TestNewCmdMCP_APathUnderAFeatureThatIsOffIsNotStale(t *testing.T) {
	t.Parallel()

	withheld := props.MCPConfig{Withhold: []string{"update"}}

	_, err := exportedNames(t, props.Tool{Name: "tool", MCP: withheld, Features: props.SetFeatures(props.Disable(props.UpdateCmd))})
	require.NoError(t, err)

	_, err = exportedNames(t, props.Tool{Name: "tool", MCP: withheld})
	require.ErrorIs(t, err, mcp.ErrWithheldPathNotFound, "update is on by default, and this tree has no update command")
}
