package docs

import (
	"context"
	"fmt"
	"io/fs"

	"charm.land/lipgloss/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	docslib "github.com/phpboyscout/go-tool-base/pkg/docs"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/output"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

// NewCmdDocsAsk creates the docs ask subcommand for AI-powered documentation Q&A.
func NewCmdDocsAsk(p *props.Props) *cobra.Command {
	var noStyle bool

	cmd := &cobra.Command{
		Use:     "ask [question]",
		Aliases: []string{"?"},
		Short:   "Ask a question about the documentation",
		Args:    cobra.ExactArgs(1),
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
func logToProps(p *props.Props, s string, level logger.Level) {
	switch level {
	case logger.DebugLevel:
		p.Logger.Debug(s)
	case logger.InfoLevel:
		p.Logger.Info(s)
	case logger.WarnLevel:
		p.Logger.Warn(s)
	case logger.ErrorLevel:
		p.Logger.Error(s)
	case logger.FatalLevel:
		p.Logger.Fatal(s)
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
