package docs

import (
	"context"
	"io/fs"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	gochat "gitlab.com/phpboyscout/go/chat"
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

const (
	docsLongIntro = `Browse the embedded project documentation in an interactive terminal
markdown browser.`
	docsLongAsk  = `Use the "ask" subcommand for AI-assisted questions over the docs.`
	docsLongRest = `A binary built with the static site embedded also has "serve", which hosts it
locally. Requires a binary built with the embedded documentation assets, which
a release build carries and a build from source does not.`
)

// NewCmdDocs creates the docs command with the interactive documentation browser.
func NewCmdDocs(p *props.Props) *setup.Command {
	var provider string

	// ask is the AI-based feature the ai flag switches; the linked chat
	// providers are the tool's wiring and do not decide this (#94). Nothing
	// describes ask without it (#98).
	aiEnabled := p.GetFeatures().Enabled(props.AiCmd)

	long := []string{docsLongIntro}
	if aiEnabled {
		long = append(long, docsLongAsk)
	}

	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Browse documentation",
		Long:  strings.Join(append(long, docsLongRest), "\n\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			efs, err := p.Assets.Exists("assets/docs")
			if err != nil {
				return errors.WithHint(errors.Wrap(err, "failed to load documentation assets"), missingAssetsHint(p.Tool))
			}

			subFS, err := fs.Sub(efs, "assets/docs")
			if err != nil {
				return errors.Wrap(err, "failed to load documentation assets")
			}

			m := docslib.NewModel(subFS, docslib.WithTitle("Documentation"),
				docslib.WithAskFunc(askFuncFor(p, subFS, &provider, cmd.Context)))

			if _, err = tea.NewProgram(m).Run(); err != nil {
				return errors.Wrap(err, "failed to run documentation viewer")
			}

			return nil
		},
	}
	docsCmd := setup.AnnotateMCP(setup.Wrap(props.DocsCmd, cmd), setup.MCPReadOnly())

	if aiEnabled {
		linked, _ := chat.LinkedProviders(p.GetFeatures())
		cmd.PersistentFlags().StringVar(&provider, "provider", "", providerFlagUsage(linked))
		docsCmd.Register(setup.Wrap(props.AiCmd, NewCmdDocsAsk(p)))
	}

	// Only add serve command if the static site exists
	if sfs, err := p.Assets.Exists("assets/site"); err == nil {
		docsCmd.Register(setup.Wrap(props.DocsCmd, NewCmdDocsServe(p, sfs)))
	}

	return docsCmd
}

// askFuncFor is the browser's ask hook, nil when ai is off so the browser
// offers no ask at all (#98).
func askFuncFor(p *props.Props, subFS fs.FS, provider *string, ctx func() context.Context) docslib.AskFunc {
	if !p.GetFeatures().Enabled(props.AiCmd) {
		return nil
	}

	return func(question string, logFn func(string, logger.Level), deltaFn func(string)) (string, error) {
		return docslib.AskAI(ctx(), p, subFS, question, logFn, deltaFn, *provider)
	}
}

// providerFlagUsage names the providers this binary links, not the whole
// catalogue (#98).
func providerFlagUsage(linked []gochat.Provider) string {
	if len(linked) == 0 {
		return "AI provider to use"
	}

	names := make([]string, len(linked))
	for i, provider := range linked {
		names[i] = string(provider)
	}

	return "AI provider to use (" + strings.Join(names, ", ") + ")"
}
