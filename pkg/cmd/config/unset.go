package config

import (
	"context"
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	cfg "gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/output"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// NewCmdUnset returns the "config unset <key>" subcommand — the inverse of
// "config set". It removes a single dot-notation key from the user's writable
// config file, re-validates the result, and persists atomically only if valid.
func NewCmdUnset(props *p.Props) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unset <key>",
		Short: "Remove a configuration value",
		Long: `Remove a single configuration value by its dot-notation key.

Only the writable config file layer is affected — values coming from
environment variables or flags are not file-backed and cannot be unset. The
result is re-validated before it is written: removing a required key is
refused and leaves the file untouched.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			if props.Config == nil {
				return errors.New("no configuration loaded")
			}

			// Only file-sourced keys count — the file layer is the only one
			// unset can affect. A key that exists solely via env/flag is
			// refused rather than silently "succeeding". Origin reports the
			// winning layer and Shadowed every other layer defining the key,
			// so a file value hidden under an env override still qualifies.
			if !fileBacked(props.Config.View(), key) {
				return errors.Newf("config key %q not found", key)
			}

			if err := persistUnset(cmd.Context(), props, key); err != nil {
				return err
			}

			if output.IsJSONOutput(cmd) {
				return output.Emit(cmd, output.Response{
					Status:  output.StatusSuccess,
					Command: "config unset",
					Data:    map[string]string{"key": key},
				})
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "unset %s\n", key)

			return nil
		},
	}

	return cmd
}

// fileBacked reports whether any file layer defines key.
func fileBacked(view *cfg.View, key string) bool {
	if origin, ok := view.Origin(key); ok && origin.Kind == cfg.SourceFile {
		return true
	}

	for _, source := range view.Shadowed(key) {
		if source.Kind == cfg.SourceFile {
			return true
		}
	}

	return false
}

// persistUnset removes key from the user's config file. The candidate result
// is validated against the base schema first, so an unset can never leave the
// file in a state "config validate" would reject; the removal itself then goes
// through the store's Apply, which edits the document in place (comments
// survive) and publishes the new snapshot.
func persistUnset(ctx context.Context, props *p.Props, key string) error {
	_, _, settings, err := loadWritableSettings(props)
	if err != nil {
		return err
	}

	deleteNestedKey(settings, key)

	data, err := yaml.Marshal(settings)
	if err != nil {
		return errors.Wrap(err, "marshalling config")
	}

	if err := validateCandidate(ctx, data); err != nil {
		return err
	}

	if _, err := props.Config.Apply(ctx, cfg.Remove(key)); err != nil {
		return errors.Wrap(err, "removing config value")
	}

	return nil
}

// validateCandidate checks a candidate YAML config document against the base
// schema without mutating the live store. It loads the bytes into a throwaway
// store (which is never watched) and runs the same schema validation as
// "config validate". Returns a descriptive error when invalid.
func validateCandidate(ctx context.Context, data []byte) error {
	schema, err := buildBaseSchema()
	if err != nil {
		return errors.Wrap(err, "failed to build validation schema")
	}

	candidate, err := cfg.NewStore(ctx, cfg.WithReaders(cfg.NamedSource{Name: "candidate", Content: data}))
	if err != nil {
		return errors.Wrap(err, "loading candidate configuration")
	}

	result := candidate.View().Validate(schema)
	if !result.Valid() {
		return errors.WithHintf(
			errors.Newf("resulting configuration is invalid:\n%s", result.Error()),
			"run `config validate` to inspect the current configuration",
		)
	}

	return nil
}
