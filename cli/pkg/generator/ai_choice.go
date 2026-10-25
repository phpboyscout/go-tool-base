package generator

import (
	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// ConfigKeyGeneratorAI is the person's own default for AI generation in
// projects whose manifest does not say. gtb init writes it (spec 0211 D7).
const ConfigKeyGeneratorAI = "generator.ai"

// ErrAIFlagsConflict is returned when a run both asks for a model and refuses one.
var ErrAIFlagsConflict = errors.NewSentinel("gtb.generator.ai_flags_conflict", "--no-ai cannot be combined with --ai, --prompt, --script or --update-docs")

// aiRung names the precedence rung that settled whether a model is called.
type aiRung string

const (
	aiRungRefused  aiRung = "--no-ai"
	aiRungClient   aiRung = "injected client"
	aiRungRun      aiRung = "the run asked"
	aiRungManifest aiRung = "manifest " + ConfigKeyGeneratorAI
	aiRungConfig   aiRung = "config " + ConfigKeyGeneratorAI
	aiRungUnasked  aiRung = "not asked"
)

type aiDecision struct {
	wanted bool
	rung   aiRung
}

// CheckAIFlags refuses a run that both asks for a model and refuses one.
func CheckAIFlags(c *Config) error {
	if c.NoAI && c.asksForAI() {
		return ErrAIFlagsConflict
	}

	return nil
}

func (c *Config) asksForAI() bool {
	return c.AI || c.Prompt != "" || c.ScriptPath != "" || c.UpdateDocs
}

// aiChoice decides once per generator whether a model may be called, in spec
// 0211 D8's order, so every log line and gate in the run reads one answer.
func (g *Generator) aiChoice() aiDecision {
	g.aiOnce.Do(func() {
		g.ai = g.decideAI()

		if !g.ai.wanted && g.config.AIProvider != "" {
			g.props.Logger.Warn("--provider names a model but the run did not ask for one; pass --ai to call it", "provider", g.config.AIProvider)
		}
	})

	return g.ai
}

func (g *Generator) decideAI() aiDecision {
	switch {
	case g.config.NoAI:
		return aiDecision{false, aiRungRefused}
	case g.chatClient != nil:
		return aiDecision{true, aiRungClient}
	case g.config.asksForAI():
		return aiDecision{true, aiRungRun}
	}

	if recorded := g.manifestAI(); recorded != nil {
		return aiDecision{*recorded, aiRungManifest}
	}

	// A person's default stays out of an unattended run, as ai.provider did
	// (#35); the manifest above is the project's own committed choice.
	if g.ciConfigured() || setup.CIDefault() {
		return aiDecision{false, aiRungUnasked}
	}

	if g.props.Config != nil && g.props.Config.View().GetBool(ConfigKeyGeneratorAI) {
		return aiDecision{true, aiRungConfig}
	}

	return aiDecision{false, aiRungUnasked}
}

func (g *Generator) manifestAI() *bool {
	if exists, _ := afero.Exists(g.props.FS, ManifestPathFor(g.config.Path)); !exists {
		return nil
	}

	m, err := g.loadManifest()
	if err != nil {
		return nil
	}

	return m.Properties.Generator.AI
}
