// Package attach is the gtb-only `attach` command. It attaches whole external
// Cobra command trees — from a separate Go module — onto a generated project's
// root, as a first-class, regeneration-safe manifest entity. It replaces the
// hand-edited cmd/<tool>/main.go + .gtb/ignore workaround: because the
// attachment lives in .gtb/manifest.yaml and is re-rendered into the root on
// every regenerate, `gtb regenerate` and `gtb enable signing` never drop it.
//
// Two channels:
//
//   - `attach command <module>@<version>` — declarative: name a constructor and
//     the props-derived dependencies it takes; gtb renders the call into the
//     root and pins the module require. Re-run for each constructor from the
//     same module.
//   - `attach adapter` — the escape hatch: scaffold a small author-owned
//     pkg/cmd/external/attach.go for any shape the declarative vocabulary cannot
//     express.
//
// This package lives under internal/cmd/ because the commands belong to the
// framework author, not downstream consumers. See
// docs/development/specs/2026-07-29-external-command-attachment.md.
package attach

import (
	"fmt"
	"io"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	icmd "gitlab.com/phpboyscout/go-tool-base/internal/cmd"
	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// argVocabulary documents the closed injection vocabulary in --arg help.
const argVocabulary = "logger, props, config, fs, version"

// NewCmdAttach returns the top-level `gtb attach` command with its command,
// adapter, and list subcommands.
func NewCmdAttach(p *props.Props) *setup.Command {
	cmd := &cobra.Command{
		Use:   "attach",
		Short: "Attach external command trees to a generated project's root",
		Long: `Attach whole Cobra command trees from an external Go module onto a generated
project's root, as a first-class, regeneration-safe manifest entity.

Unlike a hand-edit in cmd/<tool>/main.go (which must be held hands-off via
.gtb/ignore and then forfeits generator updates), an attachment lives in
.gtb/manifest.yaml and is re-rendered into the root on every regenerate — so it
survives 'gtb regenerate' and 'gtb enable signing'.

Available subcommands:
  command   Declaratively attach an external constructor (module + version pin).
  adapter   Scaffold the author-owned adapter escape hatch for arbitrary shapes.
  list      Show the declared attachments.`,
	}

	group := setup.Wrap("", cmd)
	group.Register(
		newCmdAttachCommand(p),
		newCmdAttachAdapter(p),
		newCmdAttachList(p),
	)

	return group
}

type attachCommandOptions struct {
	path        string
	constructor string
	args        []string
	wrap        bool
	importPath  string
	alias       string
	name        string
}

func newCmdAttachCommand(p *props.Props) *setup.Command {
	opts := &attachCommandOptions{}

	cmd := &cobra.Command{
		Use:   "command <module>@<version>",
		Short: "Declaratively attach an external constructor to the root",
		Long: `Attach one external constructor to the generated root. Pass the module path and
an explicit version pin as <module>@<version>, the exported constructor via
--constructor, and the props-derived dependencies it takes via one or more
--arg tokens (` + argVocabulary + `). Use --wrap when the constructor returns a
*cobra.Command (it is wrapped in setup.Wrap); omit it when it returns a
*setup.Command.

Re-run for each constructor from the same module — a second attach for the same
module appends to it. The module require is pinned at the given version.`,
		Example: `  # Attach the sign and keys trees from go/signing-cli (two invocations):
  gtb attach command gitlab.com/phpboyscout/go/signing-cli@v0.1.0 \
    --constructor NewCmdSign --arg logger --wrap
  gtb attach command gitlab.com/phpboyscout/go/signing-cli@v0.1.0 \
    --constructor NewCmdKeys --arg logger --wrap`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, cmdArgs []string) error {
			module, version, err := splitModuleVersion(cmdArgs[0])
			if err != nil {
				return err
			}

			spec := generator.ExternalCommandSpec{
				Module:     module,
				Version:    version,
				ImportPath: opts.importPath,
				Alias:      opts.alias,
				Attach: []generator.ManifestExternalAttach{{
					Constructor: opts.constructor,
					Args:        opts.args,
					Wrap:        opts.wrap,
					Name:        opts.name,
				}},
			}

			gen := generator.New(p, &generator.Config{
				Path:      icmd.ResolveProjectPath(p, opts.path),
				Overwrite: "allow",
			})

			if err := gen.AttachExternalCommand(cmd.Context(), spec); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "attached %s from %s@%s\n", opts.constructor, module, version)

			return nil
		},
	}

	cmd.Flags().StringVarP(&opts.path, "path", "p", ".", "Path to project root")
	cmd.Flags().StringVar(&opts.constructor, "constructor", "", "Exported constructor to call, e.g. NewCmdSign (required)")
	cmd.Flags().StringArrayVar(&opts.args, "arg", nil, "Injection token passed to the constructor, repeatable: "+argVocabulary)
	cmd.Flags().BoolVar(&opts.wrap, "wrap", false, "Wrap the constructor's *cobra.Command return in setup.Wrap (omit if it returns *setup.Command)")
	cmd.Flags().StringVar(&opts.importPath, "import-path", "", "Package import path when it differs from the module path")
	cmd.Flags().StringVar(&opts.alias, "alias", "", "Import alias for the package in the generated root")
	cmd.Flags().StringVar(&opts.name, "name", "", "Expected top-level command name (for collision detection)")

	_ = cmd.MarkFlagRequired("constructor")

	return setup.Wrap("", cmd)
}

func newCmdAttachAdapter(p *props.Props) *setup.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "adapter",
		Short: "Scaffold the author-owned external-command adapter",
		Long: `Scaffold pkg/cmd/external/attach.go — the external-command adapter escape hatch —
and wire external.Commands(p) into the generated root. The file is created once
and never overwritten by gtb (it is preserved across regenerate), so you own its
Commands(p) body: attach external command trees of any shape the declarative
'attach command' vocabulary cannot express.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			gen := generator.New(p, &generator.Config{
				Path:      icmd.ResolveProjectPath(p, path),
				Overwrite: "allow",
			})

			if err := gen.AttachExternalAdapter(cmd.Context()); err != nil {
				return err
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(),
				"scaffolded pkg/cmd/external/attach.go — edit its Commands(p) to attach external command trees")

			return nil
		},
	}

	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")

	return setup.Wrap("", cmd)
}

func newCmdAttachList(p *props.Props) *setup.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List declared external-command attachments",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			gen := generator.New(p, &generator.Config{Path: icmd.ResolveProjectPath(p, path)})

			ecs, adapter, err := gen.ListExternalCommands()
			if err != nil {
				return err
			}

			printAttachments(cmd.OutOrStdout(), ecs, adapter)

			return nil
		},
	}

	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")

	return setup.Wrap("", cmd)
}

// splitModuleVersion parses a <module>@<version> argument into its parts,
// splitting on the last '@' so a versioned module path is unambiguous.
func splitModuleVersion(s string) (module, version string, err error) {
	i := strings.LastIndex(s, "@")
	if i <= 0 || i == len(s)-1 {
		return "", "", errors.WithHint(generator.ErrInvalidInput,
			"argument must be <module>@<version>, e.g. gitlab.com/org/mod@v1.2.3")
	}

	return s[:i], s[i+1:], nil
}

func printAttachments(out io.Writer, ecs []generator.ManifestExternalCommand, adapter bool) {
	if len(ecs) == 0 && !adapter {
		_, _ = fmt.Fprintln(out, "No external command attachments.")

		return
	}

	for _, ec := range ecs {
		_, _ = fmt.Fprintf(out, "%s@%s\n", ec.Module, ec.Version)

		for _, a := range ec.Attach {
			wrap := ""
			if a.Wrap {
				wrap = " (wrapped)"
			}

			_, _ = fmt.Fprintf(out, "  - %s(%s)%s\n", a.Constructor, strings.Join(a.Args, ", "), wrap)
		}
	}

	if adapter {
		_, _ = fmt.Fprintln(out, "adapter: pkg/cmd/external/attach.go (external.Commands)")
	}
}
