package template

import (
	"fmt"

	"github.com/spf13/cobra"

	icmd "gitlab.com/phpboyscout/go-tool-base/internal/cmd"
	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// newCmdTemplateAdd appends a template source to the project manifest, pins it,
// and regenerates. For a remote (git) source it confirms the trust decision
// first (suppressible under --ci / non-interactive).
func newCmdTemplateAdd(p *props.Props) *setup.Command {
	var (
		path string
		name string
	)

	cmd := &cobra.Command{
		Use:   "add <src>@<ref>",
		Short: "Add a custom template-overlay source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path = icmd.ResolveProjectPath(p, path)

			ts, err := generator.ParseTemplateSpec(p.FS, args[0], name)
			if err != nil {
				return err
			}

			if err := icmd.ConfirmRemoteTemplate(p, isCI(cmd, p), ts); err != nil {
				return err
			}

			gen := generator.New(p, &generator.Config{Path: path, Overwrite: "allow"}).EnableRealTemplateClone()

			if err := gen.AddTemplateSource(cmd.Context(), ts); err != nil {
				return err
			}

			p.Logger.Infof("Added template source %q (%s).", labelOf(ts), ts.Type)

			return nil
		},
	}

	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")
	cmd.Flags().StringVar(&name, "name", "", "Source handle for update/remove (defaults to the repo/dir name)")

	return setup.Wrap("", cmd)
}

// newCmdTemplateUpdate re-resolves a named git source's ref to a new commit and
// regenerates (the only pin-advancing path).
func newCmdTemplateUpdate(p *props.Props) *setup.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Re-resolve a git source's ref to a new commit and regenerate",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path = icmd.ResolveProjectPath(p, path)

			gen := generator.New(p, &generator.Config{Path: path, Overwrite: "allow"}).EnableRealTemplateClone()

			if err := gen.UpdateTemplateSource(cmd.Context(), args[0]); err != nil {
				return err
			}

			p.Logger.Infof("Updated template source %q.", args[0])

			return nil
		},
	}

	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")

	return setup.Wrap("", cmd)
}

// newCmdTemplateRemove drops a named source and regenerates, restoring any
// embedded scaffold a `replaces:` had suppressed.
func newCmdTemplateRemove(p *props.Props) *setup.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a source and restore any scaffold it replaced",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path = icmd.ResolveProjectPath(p, path)

			gen := generator.New(p, &generator.Config{Path: path, Overwrite: "allow"}).EnableRealTemplateClone()

			if err := gen.RemoveTemplateSource(cmd.Context(), args[0]); err != nil {
				return err
			}

			p.Logger.Infof("Removed template source %q.", args[0])

			return nil
		},
	}

	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")

	return setup.Wrap("", cmd)
}

// newCmdTemplateList prints the recorded sources, refs, and resolved commits.
func newCmdTemplateList(p *props.Props) *setup.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show recorded template sources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path = icmd.ResolveProjectPath(p, path)

			gen := generator.New(p, &generator.Config{Path: path})

			sources, err := gen.ListTemplateSources()
			if err != nil {
				return err
			}

			if len(sources) == 0 {
				p.Logger.Info("No template sources configured.")

				return nil
			}

			printSources(cmd, sources)

			return nil
		},
	}

	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")

	return setup.Wrap("", cmd)
}

// printSources writes a compact source listing to the command's stdout.
func printSources(cmd *cobra.Command, sources []generator.TemplateSource) {
	out := cmd.OutOrStdout()

	for _, ts := range sources {
		pin := ts.Resolved
		if pin == "" {
			pin = "(local, fingerprint)"
		}

		ref := ts.Ref
		if ref == "" {
			ref = "(default)"
		}

		_, _ = fmt.Fprintf(out, "%-20s %-6s %-40s ref=%s pin=%s\n", labelOf(ts), ts.Type, ts.Location, ref, pin)
	}
}

// labelOf returns the source name or, when unnamed, its location.
func labelOf(ts generator.TemplateSource) string {
	if ts.Name != "" {
		return ts.Name
	}

	return ts.Location
}

// isCI reports whether the tool is running in CI, honouring the global --ci
// persistent flag and the ci config key.
func isCI(cmd *cobra.Command, p *props.Props) bool {
	if ci, err := cmd.Flags().GetBool("ci"); err == nil && ci {
		return true
	}

	if cfg := p.GetConfig(); cfg != nil {
		return cfg.GetBool("ci")
	}

	return false
}
