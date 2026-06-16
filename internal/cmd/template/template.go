// Package template is the gtb-only `template` command group. It manages a
// generated project's custom template-overlay sources — adding, updating,
// removing, and listing the sources recorded in .gtb/manifest.yaml. Each
// mutating verb edits the manifest and regenerates so the overlay (and any
// gtb-template.yaml `replaces:` suppression) is applied or undone.
//
// The manifest-edit + `regenerate` path is the source of truth; these
// subcommands are ergonomics over it. See
// docs/development/specs/2026-06-15-generator-custom-partial-templates.md.
package template

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// NewCmdTemplate returns the top-level `gtb template` command group with its
// subcommands attached. Mirrors the shape of internal/cmd/enable so the gtb
// root composes it the same way.
func NewCmdTemplate(p *props.Props) *setup.Command {
	cmd := &cobra.Command{
		Use:   "template",
		Short: "Manage custom template-overlay sources on a generated project",
		Long: `Add, update, remove, and list custom template-overlay sources.

A template source is a local folder or a git repo whose files are rendered over
the embedded skeleton at matching paths: a new path adds a file; a path that
also exists in the skeleton is overwritten (user wins). A source-side
gtb-template.yaml descriptor can suppress whole embedded forge-CI scaffolds.

Available subcommands:
  add      Add a source (<src>@<ref>), pin it, and regenerate.
  update   Re-resolve a git source's ref to a new commit and regenerate.
  remove   Remove a source and restore any scaffold it replaced.
  list     Show recorded sources, refs, and resolved commits.`,
	}

	group := setup.Wrap("", cmd)
	group.Register(
		newCmdTemplateAdd(p),
		newCmdTemplateUpdate(p),
		newCmdTemplateRemove(p),
		newCmdTemplateList(p),
	)

	return group
}
