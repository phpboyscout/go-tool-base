package config

import (
	"fmt"
	"io"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	cfg "gitlab.com/phpboyscout/go/config"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
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
			// feature bundle) are not the user's to remove, so they are
			// filtered; anything file-, env- or flag-authored still warns.
			result.Warnings = userActionableWarnings(view, result.Warnings)

			printValidationResult(cmd.OutOrStdout(), result)

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
func buildBaseSchema() (*cfg.Schema, error) {
	type baseConfig struct {
		LogLevel string `config:"log.level" validate:"required" enum:"debug,info,warn,error" description:"log verbosity level"`
	}

	return cfg.NewSchema(cfg.WithStructSchema(baseConfig{}))
}

// userActionableWarnings drops warnings for keys no user-influenced layer
// defines.
func userActionableWarnings(view *cfg.View, warnings []cfg.ValidationError) []cfg.ValidationError {
	kept := make([]cfg.ValidationError, 0, len(warnings))

	for _, warning := range warnings {
		if warning.Key == "" || userAuthoredKey(view, warning.Key) {
			kept = append(kept, warning)
		}
	}

	return kept
}

func printValidationResult(w io.Writer, result *cfg.ValidationResult) {
	for _, e := range result.Errors {
		_, _ = fmt.Fprintf(w, "error:   %s\n", e.String())
	}

	for _, e := range result.Warnings {
		_, _ = fmt.Fprintf(w, "warning: %s\n", e.String())
	}
}
