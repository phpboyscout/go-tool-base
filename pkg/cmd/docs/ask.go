package docs

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	docslib "github.com/phpboyscout/gtb/pkg/docs"
	"github.com/phpboyscout/gtb/pkg/logger"
	"github.com/phpboyscout/gtb/pkg/props"
)

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

const (
	defaultWordWrap = 80
)

func runAsk(ctx context.Context, p *props.Props, question string, noStyle bool, provider string) error {
	// 1. Load Docs Content
	subFS, err := fs.Sub(p.Assets, "assets/docs")
	if err != nil {
		return errors.Newf("failed to access embedded assets: %w", err)
	}

	// 2. Ask (stdout logger?)
	// Not passing a logger function means it won't stream logs, which is consistent with previous CLI behavior (only "Thinking..." printed).
	// If we want logs, we can pass fmt.Println or similar, but let's keep it simple.
	answer, err := docslib.AskAI(ctx, p, subFS, question, func(s string, level logger.Level) {
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
	}, provider)
	if err != nil {
		return errors.Newf("failed to ask AI: %w", err)
	}

	// 3. Print
	fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true).Render("Answer:"))

	if !noStyle {
		r, _ := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(defaultWordWrap),
		)

		out, err := r.Render(answer)
		if err == nil {
			fmt.Print(out)

			return nil
		}
		// Fallback if render fails
	}

	fmt.Println(answer)

	return nil
}
