// Package annotate implements `gtb annotate`, which records a command's MCP
// tool annotations in the manifest and re-renders its cmd.go (spec 0201 D5).
package annotate

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"gitlab.com/phpboyscout/go/errors"

	icmd "gitlab.com/phpboyscout/go-tool-base/cli/pkg/cmd"
	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// ErrNothingToAnnotate reports an invocation that names no hint and no --clear.
var ErrNothingToAnnotate = errors.NewSentinel("gtb.annotate.nothing", "annotate: give at least one hint flag, or --clear")

// ErrClearWithHints reports --clear combined with a hint flag.
var ErrClearWithHints = errors.NewSentinel("gtb.annotate.clear_with_hints", "annotate: --clear cannot be combined with a hint flag")

type options struct {
	Path        string
	Title       string
	ReadOnly    bool
	Destructive bool
	Idempotent  bool
	OpenWorld   bool
	Clear       bool
}

// NewCmdAnnotate returns `gtb annotate <command-path>`. Each hint flag is
// tri-state: given bare it sets true, given as --flag=false it sets false, and
// not given it leaves the recorded value alone. --clear removes the block.
func NewCmdAnnotate(p *props.Props) *setup.Command {
	opts := options{}

	cmd := &cobra.Command{
		Use:   "annotate <command-path>",
		Short: "Set a command's MCP tool annotations",
		Long: `Record the MCP tool annotations a command declares about itself and
re-render its cmd.go to carry them: a display title and the four behavioural
hints (read-only, destructive, idempotent, open-world) an MCP client uses to
decide what to confirm and what to call freely.

Each hint flag is tri-state. Given bare (--read-only) it records true; given as
--read-only=false it records false; not given, the recorded value is left as it
is. --clear removes every recorded annotation. The command path is
slash-separated ('post' or 'post/due'). A protected command is refused;
unprotect it first.

Hints describe one command and do not inherit down a subtree.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			hints, clear, err := hintsFromFlags(cmd.Flags(), opts)
			if err != nil {
				return err
			}

			resolved := icmd.ResolveProjectPath(p, opts.Path)
			gen := generator.New(p, &generator.Config{Path: resolved})

			if err := gen.SetMCPHints(cmd.Context(), args[0], hints, clear); err != nil {
				return err
			}

			if clear {
				p.Logger.Info("MCP annotations cleared", "command", args[0])
			} else {
				p.Logger.Info("MCP annotations recorded", "command", args[0])
			}

			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&opts.Path, "path", "p", ".", "Path to project root")
	flags.StringVar(&opts.Title, "title", "", "display title shown by MCP clients")
	flags.BoolVar(&opts.ReadOnly, "read-only", false, "the command mutates nothing (--read-only=false to say it does)")
	flags.BoolVar(&opts.Destructive, "destructive", false, "the command may remove or rewrite state")
	flags.BoolVar(&opts.Idempotent, "idempotent", false, "repeating the call with the same arguments has no further effect")
	flags.BoolVar(&opts.OpenWorld, "open-world", false, "the command reaches outside the tool (a provider, a release source)")
	flags.BoolVar(&opts.Clear, "clear", false, "remove every recorded annotation")

	return setup.Wrap("", cmd)
}

// hintsFromFlags turns the flags that were actually given into hints, so an
// absent flag is nil rather than false.
func hintsFromFlags(flags *pflag.FlagSet, opts options) (setup.MCPHints, bool, error) {
	hints := setup.MCPHints{Title: opts.Title}

	if flags.Changed("read-only") {
		hints.ReadOnly = &opts.ReadOnly
	}

	if flags.Changed("destructive") {
		hints.Destructive = &opts.Destructive
	}

	if flags.Changed("idempotent") {
		hints.Idempotent = &opts.Idempotent
	}

	if flags.Changed("open-world") {
		hints.OpenWorld = &opts.OpenWorld
	}

	switch {
	case opts.Clear && !hints.IsZero():
		return setup.MCPHints{}, false, ErrClearWithHints
	case opts.Clear:
		return setup.MCPHints{}, true, nil
	case hints.IsZero():
		return setup.MCPHints{}, false, ErrNothingToAnnotate
	default:
		return hints, false, nil
	}
}
