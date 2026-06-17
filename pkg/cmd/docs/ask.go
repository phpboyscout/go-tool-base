package docs

import (
	"context"
	"fmt"
	"io/fs"

	"charm.land/lipgloss/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	docslib "gitlab.com/phpboyscout/go-tool-base/pkg/docs"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/output"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// NewCmdDocsAsk creates the docs ask subcommand for AI-powered documentation Q&A.
func NewCmdDocsAsk(p *props.Props) *cobra.Command {
	var noStyle bool

	cmd := &cobra.Command{
		Use:     "ask [question]",
		Aliases: []string{"?"},
		Short:   "Ask a question about the documentation",
		Long: `Ask a natural-language question and get an AI-assisted answer grounded in
the embedded project documentation.

Requires a configured AI provider; use --provider to override the default.
Answers are derived only from the bundled docs, so a binary built without the
documentation assets cannot answer.`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			question := args[0]
			provider, _ := cmd.Flags().GetString("provider")
			p.ErrorHandler.Fatal(runAsk(cmd.Context(), p, question, noStyle, provider))
		},
	}
	cmd.Flags().BoolVarP(&noStyle, "no-style", "n", false, "Disable markdown styling")

	return cmd
}

// logToProps forwards a docs log message to the props logger at the appropriate level.
func logToProps(p props.LoggerProvider, s string, level logger.Level) {
	log := p.GetLogger()

	switch level {
	case logger.DebugLevel:
		log.Debug(s)
	case logger.InfoLevel:
		log.Info(s)
	case logger.WarnLevel:
		log.Warn(s)
	case logger.ErrorLevel:
		log.Error(s)
	case logger.FatalLevel:
		log.Fatal(s)
	}
}

func runAsk(ctx context.Context, p *props.Props, question string, noStyle bool, provider string) error {
	subFS, err := fs.Sub(p.Assets, "assets/docs")
	if err != nil {
		return errors.Newf("failed to access embedded assets: %w", err)
	}

	fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true).Render("Answer:"))

	// In no-style mode stream deltas directly so the answer appears progressively.
	// In styled mode suppress deltas and render the complete response at the end.
	var didStream bool

	var deltaFn func(string)
	if noStyle {
		deltaFn = func(delta string) {
			didStream = true

			fmt.Print(delta)
		}
	}

	answer, err := docslib.AskAI(ctx, p, subFS, question, func(s string, level logger.Level) {
		logToProps(p, s, level)
	}, deltaFn, provider)
	if err != nil {
		return errors.Newf("failed to ask AI: %w", err)
	}

	if noStyle {
		if didStream {
			fmt.Println() // newline after streamed output
		} else {
			fmt.Println(answer)
		}

		return nil
	}

	fmt.Print(output.RenderMarkdown(answer))

	return nil
}
