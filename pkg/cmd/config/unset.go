package config

import (
	"bytes"
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	cfg "gitlab.com/phpboyscout/go-tool-base/pkg/config"
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

			// Has is backed by Viper's InConfig — only file-sourced keys count.
			// A key that exists solely via env/flag is not in the layer unset
			// can affect, so refuse rather than silently "succeed".
			if !props.Config.Has(key) {
				return errors.Newf("config key %q not found", key)
			}

			if err := persistUnset(props, key); err != nil {
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

// persistUnset performs the read-modify-write that removes key from the user's
// config file. It mirrors persistConfigValue, substituting deleteNestedKey for
// setNestedKey and re-validating the candidate against the base schema before
// the atomic write so an unset can never leave the file in a state that
// "config validate" would reject.
func persistUnset(props *p.Props, key string) error {
	fs, path, settings, err := loadWritableSettings(props)
	if err != nil {
		return err
	}

	deleteNestedKey(settings, key)

	data, err := yaml.Marshal(settings)
	if err != nil {
		return errors.Wrap(err, "marshalling config")
	}

	if err := validateCandidate(fs, data); err != nil {
		return err
	}

	if err := writeConfigAtomic(fs, path, data); err != nil {
		return err
	}

	if err := reloadBoundFile(props, "unset"); err != nil {
		return err
	}

	return nil
}

// reloadBoundFile re-reads the config from disk so subsequent props.Config.Get*
// calls reflect a just-written change. It is a no-op when no file is bound to
// the live viper (the embedded-merge case, where there is nothing to reload
// from and the write simply lands at the default path for the next run — the
// same behaviour as "config set"). op names the operation for error context.
func reloadBoundFile(props *p.Props, op string) error {
	v := props.Config.GetViper()
	if v == nil || v.ConfigFileUsed() == "" {
		return nil
	}

	if err := v.ReadInConfig(); err != nil {
		return errors.Wrapf(err, "reload config after %s", op)
	}

	return nil
}

// validateCandidate checks a candidate YAML config document against the base
// schema without mutating the live container. It loads the bytes into a
// throwaway reader container (which is never watched) and runs the same schema
// validation as "config validate". Returns a descriptive error when invalid.
func validateCandidate(fs afero.Fs, data []byte) error {
	schema, err := buildBaseSchema()
	if err != nil {
		return errors.Wrap(err, "failed to build validation schema")
	}

	candidate := cfg.NewReaderContainer(fs,
		cfg.WithConfigFormat("yaml"),
		cfg.WithConfigReaders(bytes.NewReader(data)),
	)

	result := candidate.Validate(schema)
	if !result.Valid() {
		return errors.WithHintf(
			errors.Newf("resulting configuration is invalid:\n%s", result.Error()),
			"run `config validate` to inspect the current configuration",
		)
	}

	return nil
}
