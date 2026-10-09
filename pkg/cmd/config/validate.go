package config

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	cfg "gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// NewCmdValidate returns the "config validate" subcommand.
func NewCmdValidate(props *p.Props) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate the current configuration",
		Long: `Check the current configuration against required key definitions.

Reports missing required fields, type mismatches, and unknown keys.
Exits with a non-zero status code if any validation errors are found.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if props.Config == nil {
				return errors.New("no configuration loaded")
			}

			schema, err := buildBaseSchema()
			if err != nil {
				return errors.Wrap(err, "failed to build validation schema")
			}

			view := props.Config.View()
			result := view.Validate(schema)

			// Unknown-key warnings only help when the user can act on them.
			// Keys supplied solely by embedded defaults (the framework or a
			// feature bundle) are not the user's to remove, and keys the
			// framework or the tool legitimately owns are not typos the schema
			// happens not to enumerate — both are filtered. Anything file-,
			// env- or flag-authored and genuinely unrecognised still warns.
			result.Warnings = actionableWarnings(props, view, result.Warnings)

			printValidationResult(cmd.OutOrStdout(), result, view.Snapshot())

			if !result.Valid() {
				return errors.New("configuration validation failed")
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "configuration is valid")

			return nil
		},
	}

	return cmd
}

// buildBaseSchema returns the minimum schema that every GTB-based tool must satisfy.
func buildBaseSchema() (*cfg.StructSchema, error) {
	type baseConfig struct {
		LogLevel string `config:"log.level" validate:"required" enum:"debug,info,warn,error" description:"log verbosity level"`
	}

	return cfg.NewSchema(cfg.WithStructSchema(baseConfig{}))
}

// unknownKeyMessage is the validation message go/config attaches to a key that
// is not in the schema. Matched to filter those warnings without touching
// value-validation warnings (required, enum, type).
const unknownKeyMessage = "unknown configuration key"

// actionableWarnings trims validation warnings to the ones a user can act on.
//
// Two filters. A warning for a key no user-influenced layer defines is dropped:
// keys supplied solely by embedded defaults are not the user's to remove. And
// an unknown-key warning for a key the framework or the tool legitimately owns
// is dropped: the base schema cannot enumerate every valid key, so an unknown
// key is not reliably a typo — only one that matches neither a framework
// section nor a key the tool declares in its own embedded assets survives as a
// genuine "did you mean…". Value-validation warnings (required, enum, type) are
// never dropped by the unknown-key filter.
func actionableWarnings(props *p.Props, view *cfg.View, warnings []cfg.ValidationError) []cfg.ValidationError {
	recognised := setup.NewConfigKeyRecogniser(props)
	kept := make([]cfg.ValidationError, 0, len(warnings))

	for _, warning := range warnings {
		if warning.Message == unknownKeyMessage && recognised.Recognises(warning.Key) {
			continue
		}

		if warning.Key == "" || userAuthoredKey(view, warning.Key) {
			kept = append(kept, warning)
		}
	}

	return kept
}

func printValidationResult(w io.Writer, result *cfg.ValidationResult, snap *cfg.Snapshot) {
	for _, e := range result.Errors {
		_, _ = fmt.Fprintf(w, "error:   %s%s\n", e.String(), envOriginSuffix(snap, e.Key))
	}

	for _, e := range result.Warnings {
		_, _ = fmt.Fprintf(w, "warning: %s%s\n", e.String(), envOriginSuffix(snap, e.Key))
	}
}

func envOriginSuffix(snap *cfg.Snapshot, key string) string {
	if key == "" {
		return ""
	}

	names := setup.EnvVariablesFor(snap, key)
	switch len(names) {
	case 0:
		return ""
	case 1:
		return " (from environment variable " + names[0] + ")"
	default:
		return " (from environment variables " + strings.Join(names, ", ") + ")"
	}
}
