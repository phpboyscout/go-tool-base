package mcp_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/mcp/cli"
	mcpcobra "gitlab.com/phpboyscout/go/mcp/cobra"
	"gitlab.com/phpboyscout/go/mcp/server"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/mcp"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// tool builds a GTB-shaped root: the global persistent flags, a
// feature-wrapped built-in with a subcommand, a generated-style command, an
// excluded one, and the mcp command under test.
func tool(t *testing.T, mode props.MCPMode, extra ...cli.Option) (*cobra.Command, *slog.LevelVar) {
	t.Helper()

	p := &props.Props{
		Tool:    props.Tool{Name: "tool", MCP: props.MCPConfig{Mode: mode}},
		Logger:  logger.NewNoop(),
		Version: version.Info{Version: "1.2.3"},
	}

	root := &cobra.Command{Use: "tool"}
	root.PersistentFlags().StringArray("config", nil, "config files")
	root.PersistentFlags().Bool("debug", false, "debug")
	root.PersistentFlags().Bool("ci", false, "ci")
	root.PersistentFlags().Bool("accessible", false, "accessible")
	root.PersistentFlags().String("output", "text", "output format")

	config := setup.Wrap(props.ConfigCmd, &cobra.Command{Use: "config", Short: "c", RunE: setup.GroupRunE})
	config.Register(setup.Wrap(props.ConfigCmd, &cobra.Command{Use: "get", Short: "g", Run: func(*cobra.Command, []string) {}}))

	post := setup.Wrap("post", &cobra.Command{Use: "post", Short: "publish", Run: func(*cobra.Command, []string) {}})
	post.Flags().String("channel", "", "target")

	secret := setup.ExcludeFromMCP(setup.Wrap("secret", &cobra.Command{Use: "secret", Short: "s", Run: func(*cobra.Command, []string) {}}))

	level := &slog.LevelVar{}
	options := append([]cli.Option{cli.WithBinding(mcpcobra.WithExecutable(os.Args[0])), cli.WithLogger(slog.New(slog.DiscardHandler))}, extra...)
	mcpCmd := mcp.NewCmdMCP(p, level, options...)

	root.AddCommand(config.Command, post.Command, secret.Command, mcpCmd.Command)

	return root, level
}

type exported struct {
	Name   string `json:"name"`
	Schema struct {
		Properties struct {
			Flags struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"flags"`
		} `json:"properties"`
	} `json:"inputSchema"`
}

func TestNewCmdMCP_AppliesGTBConventions(t *testing.T) {
	t.Parallel()

	root, level := tool(t, "")
	out := filepath.Join(t.TempDir(), "tools.json")

	root.SetArgs([]string{"mcp", "tools", "--output", out, "--log-level", "debug"})
	root.SetOut(io.Discard)
	require.NoError(t, root.Execute())

	assert.Equal(t, slog.LevelDebug, level.Level(), "--log-level moves the root's level variable")

	raw, err := os.ReadFile(out)
	require.NoError(t, err)

	var tools []exported
	require.NoError(t, json.Unmarshal(raw, &tools))

	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}

	assert.Equal(t, []string{"tool_config_get", "tool_post"}, names, "excluded and mcp commands are absent; groups are not published")

	flags := tools[1].Schema.Properties.Flags.Properties
	assert.Contains(t, flags, "channel")
	assert.Contains(t, flags, "output", "--output stays: a client wants JSON")

	for _, global := range []string{"config", "debug", "ci", "accessible"} {
		assert.NotContains(t, flags, global, "GTB's global flag %s is not published", global)
	}
}

func TestNewCmdMCP_IsWrappedForTheFeatureAndSkipsUpdateChecks(t *testing.T) {
	t.Parallel()

	root, _ := tool(t, "")
	cmd, _, err := root.Find([]string{"mcp"})
	require.NoError(t, err)

	assert.Equal(t, props.McpCmd, setup.FeatureOf(cmd))
	assert.Equal(t, "true", cmd.Annotations[setup.SkipUpdateCheckAnnotation])
	assert.Equal(t, "true", cmd.Annotations[setup.ProtocolStdoutAnnotation], "a protocol stdout gets no prompt UI")
	assert.Equal(t, "true", cmd.Annotations["mcp.phpboyscout.uk/omit"])

	for _, sub := range []string{"start", "stream", "tools", "claude", "cursor", "vscode"} {
		_, _, err := root.Find([]string{"mcp", sub})
		require.NoError(t, err, sub)
	}
}

type rpc struct {
	in  io.Writer
	out *bufio.Reader
	id  int
}

func (c *rpc) call(t *testing.T, method, params string) json.RawMessage {
	t.Helper()

	c.id++
	_, err := fmt.Fprintf(c.in, `{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`+"\n", c.id, method, params)
	require.NoError(t, err)

	for {
		line, err := c.out.ReadBytes('\n')
		require.NoError(t, err)

		var message map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(line, &message))

		if id, ok := message["id"]; ok && string(id) == fmt.Sprint(c.id) {
			require.NotContains(t, message, "error", "%s: %s", method, message["error"])

			return message["result"]
		}
	}
}

// session serves the tool over an in-process stdio pair, initialises, and
// hands back a client. The returned stop closes the client side and waits
// for the server to return cleanly.
func session(t *testing.T, mode props.MCPMode) (*rpc, func()) {
	t.Helper()

	clientIn, serverIn := io.Pipe()
	serverOut, clientOut := io.Pipe()

	root, _ := tool(t, mode, cli.WithServer(server.WithStdio(clientIn, clientOut)))

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)

	root.SetArgs([]string{"mcp", "start"})

	done := make(chan error, 1)

	go func() { done <- root.ExecuteContext(ctx) }()

	client := &rpc{in: serverIn, out: bufio.NewReader(serverOut)}
	client.call(t, "initialize", `{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}`)
	_, _ = fmt.Fprintln(serverIn, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	return client, func() {
		defer cancel()

		_ = serverIn.Close()
		require.NoError(t, <-done)
	}
}

func toolNames(t *testing.T, result json.RawMessage) []string {
	t.Helper()

	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(result, &list))

	names := make([]string, 0, len(list.Tools))
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}

	return names
}

func TestNewCmdMCP_PublicationModeFollowsTheTool(t *testing.T) {
	t.Parallel()

	compact, stop := session(t, "")
	assert.Equal(t, []string{"search_tools", "get_tool_details", "call_tool"}, toolNames(t, compact.call(t, "tools/list", `{}`)))
	stop()

	direct, stop := session(t, props.MCPDirect)
	assert.Equal(t, []string{"tool_config_get", "tool_post"}, toolNames(t, direct.call(t, "tools/list", `{}`)))
	stop()
}

func TestNewCmdMCP_GroupsByFeatureAndServesIdentity(t *testing.T) {
	t.Parallel()

	client, stop := session(t, "")
	defer stop()

	var search struct {
		Structured struct {
			Operations []struct {
				Name  string `json:"name"`
				Group string `json:"group"`
			} `json:"operations"`
		} `json:"structuredContent"`
	}
	raw := client.call(t, "tools/call", `{"name":"search_tools","arguments":{"group":"config"}}`)
	require.NoError(t, json.Unmarshal(raw, &search))

	require.Len(t, search.Structured.Operations, 1, "%s", raw)
	assert.Equal(t, "tool_config_get", search.Structured.Operations[0].Name)
	assert.Equal(t, "config", search.Structured.Operations[0].Group, "the wrapper's feature is the group")
}
