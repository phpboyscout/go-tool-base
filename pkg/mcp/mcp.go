// Package mcp is GTB's glue over the go/mcp module, and the link that gives a
// tool the mcp feature. A blank import from the tool's main (the generator
// writes cmd/<name>/mcp.go to do so) declares the feature as a link kind and
// contributes the `mcp` command to the root, with the framework's conventions
// applied once (spec 0201 D1, spec 0202 D1). Nothing else in the framework
// imports go/mcp, so a binary without the import carries neither the module
// nor the MCP SDK.
package mcp

import (
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gitlab.com/phpboyscout/go/mcp/cli"
	mcpcobra "gitlab.com/phpboyscout/go/mcp/cobra"
	"gitlab.com/phpboyscout/go/mcp/server"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Feature is the feature this import declares: props.McpCmd, kept there as
// the ID so a main that names it links nothing.
const Feature = props.McpCmd

// PackagePath is this package's import path, the ConstPackage the descriptor
// carries: for a link, the package to blank-import.
const PackagePath = "gitlab.com/phpboyscout/go-tool-base/pkg/mcp"

func init() {
	props.RegisterFeature(props.FeatureDescriptor{
		ID:           Feature,
		ConstName:    "Feature",
		ConstPackage: PackagePath,
		Kind:         props.KindLink,
		Default:      true,
	})

	setup.RegisterRootCommands(Feature, func(p *props.Props) *setup.Command {
		return NewCmdMCP(p, p.GetLogLevel())
	})
}

// globalFlags are the root's persistent flags every command inherits. They
// steer the process, not the command, so a client never sees them: --config
// would let it point the tool at another configuration file, and the rest
// describe the terminal it is not sitting at. --output stays, because a
// client wants JSON.
var globalFlags = map[string]bool{"config": true, "debug": true, "ci": true, "accessible": true}

// NewCmdMCP builds the `mcp` command for a tool: exposure from the command
// tree's own markers, the framework's global flags withheld, operations
// grouped by feature, the publication mode from props.Tool.MCP, the tool's
// identity, and logs on the invocation's error stream at the level the root's
// --debug also moves (stdout carries the protocol).
// extra options come last, so a host or a test can add or override.
func NewCmdMCP(p *props.Props, level *slog.LevelVar, extra ...cli.Option) *setup.Command {
	options := make([]cli.Option, 0, 5+len(extra)) //nolint:mnd // the five conventions below
	options = append(options,
		cli.WithLogLevel(level),
		cli.WithLogger(slog.New(slog.NewTextHandler(p.GetIO().Err(), &slog.HandlerOptions{Level: level}))),
		cli.WithBinding(BindingOptions()...),
		cli.WithServer(ServerOptions(p)...),
		cli.WithServerName(p.Tool.Name),
	)

	cmd := cli.Command(append(options, extra...)...)

	// An MCP server's stdout carries JSON-RPC frames: neither the pre-run
	// update check's output nor a consent prompt may race it. Both stamps
	// cover the whole subtree.
	setup.MarkSkipUpdateCheck(cmd)
	setup.MarkProtocolStdout(cmd)

	return setup.Wrap(props.McpCmd, cmd)
}

// BindingOptions are GTB's conventions for the Cobra binding: exposure by the
// tree's own markers (spec 0089), the global flags withheld, and the wrapper's
// feature as the search group.
func BindingOptions() []mcpcobra.BindingOption {
	return []mcpcobra.BindingOption{
		mcpcobra.WithExposure(exposed),
		mcpcobra.WithFlagFilter(func(_ *cobra.Command, flag *pflag.Flag) bool { return !globalFlags[flag.Name] }),
		mcpcobra.WithGroup(featureGroup),
	}
}

// ServerOptions are the protocol server's identity and publication mode.
func ServerOptions(p *props.Props) []server.Option {
	options := []server.Option{server.WithIdentity(p.Tool.Name, p.Version.GetVersion())}
	if p.Tool.MCP.Direct() {
		options = append(options, server.WithMode(server.Direct))
	}

	return options
}

// exposed is the tree's own exposure decision (spec 0089), minus the pure
// groups cobra counts as runnable.
func exposed(cmd *cobra.Command) bool {
	return setup.IsExposedToMCP(cmd) && !setup.IsGroup(cmd)
}

// featureGroup names the operation's group after the feature its command was
// wrapped with, falling back to the parent command's name for a bare tree.
func featureGroup(cmd *cobra.Command) string {
	if feature := setup.FeatureOf(cmd); feature != "" {
		return string(feature)
	}

	if parent := cmd.Parent(); parent != nil && parent.HasParent() {
		return parent.Name()
	}

	return ""
}
