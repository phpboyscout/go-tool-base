// Package ignore is the gtb-only `ignore` command group. It manages a
// generated project's .gtb/ignore rules — the file that marks deliberately
// diverged generated files hands-off so `regenerate` stops re-rendering them
// and stops raising conflicts.
//
// Unlike `gtb template`, the ignore verbs do NOT regenerate: the ignore file is
// read fresh on the next `regenerate`, and editing it changes nothing already
// on disk. So `add`/`remove` are pure file edits and `--dry-run` means "show
// the resulting file", not "simulate a regenerate". See
// https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0181-ignore-command-and-discoverability.
package ignore

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// NewCmdIgnore returns the top-level `gtb ignore` command group with its
// subcommands attached. Mirrors the shape of internal/cmd/template so the gtb
// root composes it the same way.
func NewCmdIgnore(p *props.Props) *setup.Command {
	cmd := &cobra.Command{
		Use:   "ignore",
		Short: "Manage .gtb/ignore rules that mark generated files hands-off",
		Long: `Add, remove, list, and check the .gtb/ignore rules that mark generated files
hands-off for 'gtb regenerate'.

A rule marks a file (or glob) so regenerate skips it — never re-rendering it and
never raising a conflict prompt. Rules are evaluated top-to-bottom; a later rule
wins, and a leading '!' re-includes a file an earlier rule excluded.

These verbs are pure file edits: unlike 'gtb template', they do NOT regenerate,
because the ignore file is read fresh on the next regenerate.

Available subcommands:
  add      Append pattern(s) to .gtb/ignore (idempotent; creates it with a header).
  remove   Drop a literal rule line from .gtb/ignore.
  list     Resolve rules against the manifest: which tracked file each governs.
  check    Report whether path(s) are ignored, and which rule decides it.`,
	}

	group := setup.Wrap("", cmd)
	group.Register(
		newCmdIgnoreAdd(p),
		newCmdIgnoreRemove(p),
		newCmdIgnoreSeal(p),
		newCmdIgnoreUnseal(p),
		newCmdIgnoreList(p),
		newCmdIgnoreCheck(p),
	)

	return group
}
