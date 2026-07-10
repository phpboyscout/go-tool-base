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
		Long: `Add a local folder or a git repo as a template-overlay source, record it in
.gtb/manifest.yaml, pin it, and regenerate so its files render over the embedded
skeleton.

Pass the source as <src>@<ref>: <src> is a local path or git URL, and the
optional @<ref> pins a git source to a branch, tag, or commit (omit it for a
local folder). Use --name to set the handle later passed to 'template update'
and 'template remove'. A remote (git) source prompts for a trust decision before
cloning; that confirmation is skipped under --ci or a non-interactive session.`,
		Args: cobra.ExactArgs(1),
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

			p.Logger.Info("added template source", "source", labelOf(ts), "type", ts.Type)

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
		Long: `Re-resolve the named git source's ref to its current commit, advance the pin in
.gtb/manifest.yaml, and regenerate so the overlay reflects the new commit.

This is the only path that advances a source's pin; a plain 'gtb regenerate'
keeps the existing pin. Only git sources can be updated — a local-folder source
has no ref to re-resolve.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path = icmd.ResolveProjectPath(p, path)

			gen := generator.New(p, &generator.Config{Path: path, Overwrite: "allow"}).EnableRealTemplateClone()

			if err := gen.UpdateTemplateSource(cmd.Context(), args[0]); err != nil {
				return err
			}

			p.Logger.Info("updated template source", "source", args[0])

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
		Long: `Drop the named template source from .gtb/manifest.yaml and regenerate, restoring
any embedded scaffold that the source's gtb-template.yaml 'replaces:' directive
had suppressed.

Use this to back out a source added with 'gtb template add'. Author-added files
that the overlay merely overwrote are re-rendered from the skeleton.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path = icmd.ResolveProjectPath(p, path)

			gen := generator.New(p, &generator.Config{Path: path, Overwrite: "allow"}).EnableRealTemplateClone()

			if err := gen.RemoveTemplateSource(cmd.Context(), args[0]); err != nil {
				return err
			}

			p.Logger.Info("removed template source", "source", args[0])

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
		Long: `List the template-overlay sources recorded in .gtb/manifest.yaml, showing each
source's handle, type, location, ref, and resolved commit (or a local-folder
fingerprint).

This is a read-only view; it never edits the manifest or regenerates. Use it to
confirm what 'gtb template update' would advance or 'gtb template remove' would
drop.`,
		Args: cobra.NoArgs,
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
