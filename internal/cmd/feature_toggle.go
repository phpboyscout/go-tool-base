package cmd

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/utils"
)

// ErrFeatureNameRequired is returned when `gtb enable`/`gtb disable` is run with
// no feature name in a context where the interactive picker cannot run (CI or a
// non-interactive session).
var ErrFeatureNameRequired = errors.New(
	"a feature name is required in non-interactive/CI mode (one of: " +
		strings.Join(generator.ToggleableFeatures, ", ") + ")")

// RunFeatureToggle is the shared implementation behind the positional feature
// arguments of `gtb enable [feature...]` and `gtb disable [feature...]`. With one
// or more feature names it toggles each non-interactively; with no name it opens
// a multi-select picker of the candidate features (the ones not already in the
// target state). enable selects the direction.
func RunFeatureToggle(cmd *cobra.Command, p *props.Props, path string, args []string, enable bool) error {
	path = ResolveProjectPath(p, path)
	gen := generator.New(p, &generator.Config{Path: path, Overwrite: "allow"})

	desired, err := resolveFeatureSelection(cmd, p, gen, args, enable)
	if err != nil {
		return err
	}

	if len(desired) == 0 {
		p.Logger.Info("No features selected; nothing changed.")

		return nil
	}

	changed, err := gen.ApplyFeatures(cmd.Context(), desired)
	if err != nil {
		return err
	}

	reportFeatureChanges(p, changed, enable)

	return nil
}

// resolveFeatureSelection turns the CLI args (a single feature name) or the
// interactive picker into the desired feature→state map ApplyFeatures expects.
func resolveFeatureSelection(cmd *cobra.Command, p *props.Props, gen *generator.Generator, args []string, enable bool) (map[string]bool, error) {
	if len(args) > 0 {
		desired := make(map[string]bool, len(args))

		for _, name := range args {
			if err := generator.ValidateFeatureName(name); err != nil {
				return nil, err
			}

			desired[name] = enable
		}

		return desired, nil
	}

	// No name: pick interactively, unless we cannot prompt.
	if !utils.IsInteractive() || featureToggleIsCI(cmd, p) {
		return nil, ErrFeatureNameRequired
	}

	selected, err := pickFeatures(gen, enable)
	if err != nil {
		return nil, err
	}

	desired := make(map[string]bool, len(selected))
	for _, name := range selected {
		desired[name] = enable
	}

	return desired, nil
}

// pickFeatures presents the multi-select of candidate features — those not
// already in the target state — and returns the chosen names.
func pickFeatures(gen *generator.Generator, enable bool) ([]string, error) {
	candidates, err := featureCandidates(gen, enable)
	if err != nil {
		return nil, err
	}

	verb := "disable"
	if enable {
		verb = "enable"
	}

	if len(candidates) == 0 {
		return nil, nil // caller reports "nothing changed"
	}

	options := make([]huh.Option[string], 0, len(candidates))
	for _, name := range candidates {
		options = append(options, huh.NewOption(name, name))
	}

	var selected []string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Features to " + verb).
				Description("Select the built-in features to " + verb + ". The generated root command is re-rendered to match.").
				Options(options...).
				Value(&selected),
		),
	)

	if err := form.Run(); err != nil {
		return nil, err
	}

	return selected, nil
}

// featureCandidates returns the toggleable features that are not already in the
// target state: the currently-disabled ones for enable, the currently-enabled
// ones for disable.
func featureCandidates(gen *generator.Generator, enable bool) ([]string, error) {
	candidates := make([]string, 0, len(generator.ToggleableFeatures))

	for _, name := range generator.ToggleableFeatures {
		on, err := gen.FeatureEnabled(name)
		if err != nil {
			return nil, err
		}

		if on != enable {
			candidates = append(candidates, name)
		}
	}

	return candidates, nil
}

// reportFeatureChanges logs the outcome of a toggle, including the
// update→ForcedUpdate interaction note when the self-update feature is turned
// off.
func reportFeatureChanges(p *props.Props, changed []string, enable bool) {
	verb := "disabled"
	if enable {
		verb = "enabled"
	}

	if len(changed) == 0 {
		p.Logger.Info(fmt.Sprintf("Already %s; nothing changed.", verb))

		return
	}

	for _, name := range changed {
		p.Logger.Info(fmt.Sprintf("Feature %q %s.", name, verb))
	}

	if !enable && slices.Contains(changed, "update") {
		p.Logger.Warn("Disabling 'update' removes the self-update subsystem, including the ForcedUpdate check — the tool will no longer detect or apply new releases.")
	}
}

// featureToggleIsCI reports whether the tool is running in CI, honouring the
// global --ci persistent flag and the ci config key.
func featureToggleIsCI(cmd *cobra.Command, p props.ConfigProvider) bool {
	if ci, err := cmd.Flags().GetBool("ci"); err == nil && ci {
		return true
	}

	if cfg := p.GetConfig(); cfg != nil {
		return cfg.GetBool("ci")
	}

	return false
}
