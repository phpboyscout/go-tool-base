package config

import (
	"fmt"
	"io"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	cfg "github.com/phpboyscout/go-tool-base/pkg/config"
	p "github.com/phpboyscout/go-tool-base/pkg/props"
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

			result := props.Config.Validate(schema)

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

func printValidationResult(w io.Writer, result *cfg.ValidationResult) {
	for _, e := range result.Errors {
		_, _ = fmt.Fprintf(w, "error:   %s\n", e.String())
	}

	for _, e := range result.Warnings {
		_, _ = fmt.Fprintf(w, "warning: %s\n", e.String())
	}
}
