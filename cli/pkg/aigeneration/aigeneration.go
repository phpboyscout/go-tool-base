// Package aigeneration asks, during gtb init, whether gtb may call a model
// while generating projects whose manifest does not say (spec 0211 D5). The
// question is gtb's own, so it lives here rather than in pkg/setup/ai, which
// every tool built on GTB inherits.
package aigeneration

import (
	"context"

	"charm.land/huh/v2"
	"github.com/spf13/pflag"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func init() {
	setup.Register(props.AiCmd,
		[]setup.InitialiserProvider{
			func(_ *props.Props, flags *pflag.FlagSet) setup.Initialiser {
				if setup.FlagSkips(flags, "skip-ai")["skip-ai"] {
					return nil
				}

				return Initialiser{}
			},
		},
		nil, nil,
	)
}

// Initialiser records the person's default for AI generation as generator.ai.
type Initialiser struct{}

// Name returns the human-readable name for this initialiser.
func (Initialiser) Name() string {
	return "AI generation"
}

// IsConfigured reports whether generator.ai has been answered already.
func (Initialiser) IsConfigured(cfg config.Reader) bool {
	return cfg.IsSet(generator.ConfigKeyGeneratorAI)
}

// Configure asks the question and writes the answer, No included, so the
// person can see the setting in their config.
func (Initialiser) Configure(ctx context.Context, p *props.Props, cfg setup.Editor) error {
	var yes bool

	if err := setup.RunForm(ctx, p, form(&yes)); err != nil {
		return errors.Wrap(err, "AI generation form")
	}

	return cfg.Set(generator.ConfigKeyGeneratorAI, yes)
}

func form(yes *bool) *huh.Form {
	return huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Key("generator-ai").
			Title("Let gtb call a model when it generates code and docs?").
			Description("A model writes richer command docs, and spends API credit each time it does.\n" +
				"This is your default for projects whose manifest does not say; a project's\n" +
				"generator.ai and the --ai / --no-ai flags override it. Saved as generator.ai.").
			Affirmative("Yes").Negative("No").
			Value(yes),
	))
}
