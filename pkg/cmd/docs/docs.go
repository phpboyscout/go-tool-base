package docs

import (
	"io/fs"

	tea "charm.land/bubbletea/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

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
	if tool.InstallHint != "" {
		return tool.InstallHint
	}

	name := tool.Name
	if name == "" {
		name = "the tool"
	}

	return "Please reinstall " + name + " using its recommended installation method to get the full release binary."
}

// NewCmdDocs creates the docs command with the interactive documentation browser.
func NewCmdDocs(p *props.Props) *setup.Command {
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
						missingAssetsHint(p.Tool),
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

	docsCmd := setup.Wrap(props.DocsCmd, cmd)
	docsCmd.Register(setup.Wrap(props.DocsCmd, NewCmdDocsAsk(p)))

	// Only add serve command if the static site exists
	if sfs, err := p.Assets.Exists("assets/site"); err == nil {
		docsCmd.Register(setup.Wrap(props.DocsCmd, NewCmdDocsServe(p, sfs)))
	}

	return docsCmd
}
