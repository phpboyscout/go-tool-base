package docs

import (
	"io/fs"

	tea "charm.land/bubbletea/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	docslib "github.com/phpboyscout/go-tool-base/pkg/docs"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
)

// NewCmdDocs creates the docs command with the interactive documentation browser.
func NewCmdDocs(p *props.Props) *cobra.Command {
	var provider string

	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Browse documentation",
		Long:  "Browse and read the project documentation in the terminal.",
		RunE: func(cmd *cobra.Command, args []string) error {
			efs, err := p.Assets.Exists("assets/docs")
			if err != nil {
				return errors.WithHint(
					errors.Wrap(err, "failed to load documentation assets"),
					"This command requires pre-built documentation assets.\n"+
						"It looks like you might have installed using 'go install', which builds from source and lacks these assets.\n"+
						"Please use the recommended installation method to get the full binary:\n"+
						"  curl -sSL https://raw.githubusercontent.com/phpboyscout/gtb/main/install.sh | bash",
				)
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
	cmd.PersistentFlags().StringVar(&provider, "provider", "", "AI provider to use (openai, claude, gemini)")

	setup.AddCommandWithMiddleware(cmd, NewCmdDocsAsk(p), props.DocsCmd)

	// Only add serve command if the static site exists
	if sfs, err := p.Assets.Exists("assets/site"); err == nil {
		setup.AddCommandWithMiddleware(cmd, NewCmdDocsServe(p, sfs), props.DocsCmd)
	}

	return cmd
}
