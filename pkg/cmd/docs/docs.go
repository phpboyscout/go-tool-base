package docs

import (
	"io/fs"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	docslib "gitlab.com/phpboyscout/go-tool-base/pkg/docs"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// missingAssetsHint returns guidance shown when the running binary lacks the
// embedded documentation assets. It uses the tool author's InstallHint when
// set, otherwise a generic message — the framework never ships a hardcoded
// installer for a specific product to downstream tools.
func missingAssetsHint(tool props.Tool) string {
	install := tool.InstallHint
	if install == "" {
		name := tool.Name
		if name == "" {
			name = "the tool"
		}

		install = "Reinstall " + name + " using its recommended installation method."
	}

	return "This binary has no embedded documentation (assets/docs). A release build embeds it; " +
		"a build from source with 'go install' or 'go build' does not, and a release built without " +
		"the docs step ships without it too.\n" + install
}

// NewCmdDocs creates the docs command with the interactive documentation browser.
func NewCmdDocs(p *props.Props) *setup.Command {
	var provider string

	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Browse documentation",
		Long: `Browse the embedded project documentation in an interactive terminal
markdown browser.

Use the "ask" subcommand for AI-assisted questions over the docs, or "serve"
to host them as a static site. Requires a binary built with the embedded
documentation assets, which a release build carries and a build from source
does not.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			efs, err := p.Assets.Exists("assets/docs")
			if err != nil {
				return errors.WithHint(errors.Wrap(err, "failed to load documentation assets"), missingAssetsHint(p.Tool))
			}

			subFS, err := fs.Sub(efs, "assets/docs")
			if err != nil {
				return errors.Wrap(err, "failed to load documentation assets")
			}

			askFunc := func(question string, logFn func(string, logger.Level), deltaFn func(string)) (string, error) {
				return docslib.AskAI(cmd.Context(), p, subFS, question, logFn, deltaFn, provider)
			}

			m := docslib.NewModel(subFS, docslib.WithTitle("Documentation"), docslib.WithAskFunc(askFunc))

			if _, err = tea.NewProgram(m).Run(); err != nil {
				return errors.Wrap(err, "failed to run documentation viewer")
			}

			return nil
		},
	}
	cmd.PersistentFlags().StringVar(&provider, "provider", "", "AI provider to use ("+chat.ProviderNames()+")")

	docsCmd := setup.Wrap(props.DocsCmd, cmd)
	docsCmd.Register(setup.Wrap(props.DocsCmd, NewCmdDocsAsk(p)))

	// Only add serve command if the static site exists
	if sfs, err := p.Assets.Exists("assets/site"); err == nil {
		docsCmd.Register(setup.Wrap(props.DocsCmd, NewCmdDocsServe(p, sfs)))
	}

	return docsCmd
}
