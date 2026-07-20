package config

import (
	"context"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	cfg "gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/output"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
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
result is re-validated (layered over the tool's embedded defaults, exactly as
"config validate" resolves it) before it is written: removing a key the
defaults supply simply falls back to the shipped default, while a removal
that would leave the resolved configuration invalid is refused and leaves the
file untouched.`,
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

// persistUnset removes key from the user's config file. The candidate result
// is validated against the base schema first, so an unset can never leave the
// file in a state "config validate" would reject; the removal itself then goes
// through the store's Apply, which edits the document in place (comments
// survive) and publishes the new snapshot.
func persistUnset(ctx context.Context, props *p.Props, key string) error {
	_, settings, err := loadWritableSettings(props)
	if err != nil {
		return err
	}

	deleteNestedKey(settings, key)

	data, err := yaml.Marshal(settings)
	if err != nil {
		return errors.Wrap(err, "marshalling config")
	}

	if err := validateCandidate(ctx, props, data); err != nil {
		return err
	}

	if _, err := props.Config.Apply(ctx, cfg.Remove(key)); err != nil {
		return errors.Wrap(err, "removing config value")
	}

	return nil
}

// validateCandidate checks a candidate YAML config document against the base
// schema without mutating the live store. The candidate is layered over the
// tool's merged embedded defaults — the same resolution "config validate"
// checks — so removing a defaults-supplied key is not refused just because
// the bare file no longer carries it. It loads the layers into a throwaway
// store (which is never watched). Returns a descriptive error when invalid.
func validateCandidate(ctx context.Context, props *p.Props, data []byte) error {
	schema, err := buildBaseSchema()
	if err != nil {
		return errors.Wrap(err, "failed to build validation schema")
	}

	var sources []cfg.NamedSource
	if defaults := setup.AssetDocument(props, setup.DefaultsAssetPath); len(defaults) > 0 {
		sources = append(sources, cfg.NamedSource{Name: "defaults", Content: defaults})
	}

	sources = append(sources, cfg.NamedSource{Name: "candidate", Content: data})

	candidate, err := cfg.NewStore(ctx, cfg.WithReaders(sources...))
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

// deleteNestedKey removes a dot-path entry from a nested map. When
// the path resolves through non-map nodes, the call is a no-op —
// the caller is asking to delete something that isn't there.
func deleteNestedKey(m map[string]any, dotPath string) {
	parts := strings.Split(dotPath, ".")
	current := m

	for i, part := range parts {
		if i == len(parts)-1 {
			delete(current, part)

			return
		}

		next, ok := current[part].(map[string]any)
		if !ok {
			return
		}

		current = next
	}
}

// loadWritableSettings resolves the writable config file and reads it into a
// nested map ready for mutation — the candidate document unset validates
// before applying the removal. It returns an empty (non-nil) map when the
// file does not yet exist.
func loadWritableSettings(props *p.Props) (string, map[string]any, error) {
	fs := writableFS(props)

	path := resolveWritableConfigPath(props, fs)
	if path == "" {
		return "", nil, errors.New("could not resolve a writable config path")
	}

	settings := map[string]any{}
	if data, err := afero.ReadFile(fs, path); err == nil {
		if uerr := yaml.Unmarshal(data, &settings); uerr != nil {
			return "", nil, errors.Wrapf(uerr, "parsing existing config %q", path)
		}

		if settings == nil {
			settings = map[string]any{}
		}
	}

	return path, settings, nil
}
