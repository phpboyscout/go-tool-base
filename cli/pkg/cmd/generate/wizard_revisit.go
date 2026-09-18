package generate

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"gitlab.com/phpboyscout/go/errors"

	icmd "gitlab.com/phpboyscout/go-tool-base/cli/pkg/cmd"
	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// WizardOptions drives `gtb wizard`: the generation wizard over an existing
// project's manifest (spec 0197 D13).
type WizardOptions struct {
	Path   string
	DryRun bool

	// runForm runs the wizard for the loaded options; the default is the
	// interactive form, and tests inject an answerer.
	runForm func(*SkeletonOptions) error
}

// NewCmdWizard returns `gtb wizard [--dry-run]`.
func NewCmdWizard(p *props.Props) *setup.Command {
	opts := &WizardOptions{}

	cmd := &cobra.Command{
		Use:   "wizard",
		Short: "Revisit a generated project's settings with the generation wizard",
		Long: `Run the generation wizard again over this project's .gtb/manifest.yaml. Every
page is pre-filled from the manifest; accept a page to keep it, change an
answer to change the setting. The name is shown, not asked. Ticking a feature
shows its pages in the same run; unticking one clears that page's answers.

What the wizard writes is validated the way the flags are validated, and the
generated files are brought into line in the same run, so no regenerate is
needed afterwards. Requires an interactive terminal.`,
		Example: `  gtb wizard
  gtb wizard --dry-run     # show which settings would change, write nothing`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return opts.Run(cmd.Context(), p, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Path to project root")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show which settings would change and write nothing")

	return setup.Wrap("", cmd)
}

const needsTerminalHint = "gtb wizard needs a terminal; use gtb set <path> <value> in a script."

// run runs the injected answerer, or the interactive form. A stdin that is
// not a terminal is refused before the form opens; a form that still cannot
// open a TTY (stdin is /dev/null, which stats as a character device) gets the
// same hint on its error.
func (o *WizardOptions) run(p *props.Props, so *SkeletonOptions) error {
	if o.runForm != nil {
		return o.runForm(so)
	}

	if !p.GetIO().Interactive() {
		return errors.WithHint(ErrNonInteractive, needsTerminalHint)
	}

	if err := so.runWizard(); err != nil {
		return errors.WithHint(err, needsTerminalHint)
	}

	return nil
}

// Run loads the manifest into the wizard, runs it, and applies the answers.
func (o *WizardOptions) Run(ctx context.Context, p *props.Props, out io.Writer) error {
	o.Path = icmd.ResolveProjectPath(p, o.Path)

	g := generator.New(p, &generator.Config{Path: o.Path, Overwrite: "allow"})

	before, err := g.LoadManifest()
	if err != nil {
		return err
	}

	so := optionsFromManifest(*before)
	so.Path = o.Path

	if err := o.run(p, so); err != nil {
		return err
	}

	so.Repo, so.Host = normalizeRepoHost(so.Repo, so.Host)

	if err := so.validateFields(); err != nil {
		return err
	}

	cfg := so.skeletonConfig(before.Properties.Templates)

	if o.DryRun {
		after := *before
		generator.ApplyAuthorSettingsTo(&after, cfg)

		diff := generator.DiffAuthorSettings(before, &after)
		for _, change := range so.mcpSurfaceChanges() {
			diff = append(diff, change.describe())
		}

		if len(diff) == 0 {
			_, _ = fmt.Fprintln(out, "no changes")

			return nil
		}

		_, _ = fmt.Fprintln(out, "would change:\n  "+strings.Join(diff, "\n  "))

		return nil
	}

	if err := g.ApplyAuthorSettings(ctx, cfg); err != nil {
		return err
	}

	// The surface is per command, not an author setting: each changed tick is
	// the same call gtb enable mcp <path> / gtb disable mcp <path> makes.
	for _, change := range so.mcpSurfaceChanges() {
		if err := g.SetMCPEnabled(ctx, change.Path, change.Exposed); err != nil {
			return err
		}
	}

	return nil
}
