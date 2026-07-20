package config

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	cfg "gitlab.com/phpboyscout/go/config"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// setArgCount is the exact number of positional arguments required by "config set".
const setArgCount = 2

// NewCmdSet returns the "config set <key> <value>" subcommand.
func NewCmdSet(props *p.Props) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: `Write a single configuration value by its dot-notation key.

The value is type-coerced: booleans (true/false) and integers are stored as
their native types; everything else is stored as a string.`,
		Args: cobra.ExactArgs(setArgCount),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, rawVal := args[0], args[1]

			if props.Config == nil {
				return errors.New("no configuration loaded")
			}

			// The default config dir may not exist yet (tools without
			// InitCmd); a missing file is a target Apply creates, but its
			// directory must exist.
			ensureDefaultConfigDir(props)

			if _, err := props.Config.Apply(cmd.Context(), cfg.Set(key, coerceValue(rawVal))); err != nil {
				return errors.Wrap(err, "persisting config value")
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "set %s = %s\n", key, rawVal)

			return nil
		},
	}

	return cmd
}

// ensureDefaultConfigDir creates the tool's default config directory so a
// write routed to the default path has somewhere to land. Best-effort: a
// custom --config path's directory already exists, because its file loaded.
func ensureDefaultConfigDir(props *p.Props) {
	if dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name); dir != "" {
		const configDirPerm = 0o700

		_ = writableFS(props).MkdirAll(dir, configDirPerm)
	}
}

// loadWritableSettings resolves the writable config file and reads it into a
// nested map ready for mutation. It returns an empty (non-nil) map when the
// file does not yet exist, so callers can write a fresh file. Shared by
// "config set" and "config unset", which differ only in how they mutate the
// returned map and whether they validate/reload afterwards.
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

// resolveWritableConfigPath returns the file the loaded config is bound to, or
// the user's default config path when none is bound.
func resolveWritableConfigPath(props *p.Props, fs afero.Fs) string {
	if props.Config != nil {
		// The highest-precedence loaded file layer — the same answer Viper's
		// ConfigFileUsed() gave (it reported the last file it loaded).
		last := ""

		for _, layer := range props.Config.Snapshot().Layers() {
			if layer.Source.Kind == cfg.SourceFile {
				last = layer.Source.Name
			}
		}

		if last != "" {
			return last
		}
	}

	dir := setup.GetDefaultConfigDir(fs, props.Tool.Name)
	if dir == "" {
		return ""
	}

	return filepath.Join(dir, setup.DefaultConfigFilename)
}

// writableFS returns props.FS, defaulting to the real OS filesystem.
func writableFS(props *p.Props) afero.Fs {
	if props.FS != nil {
		return props.FS
	}

	return afero.NewOsFs()
}

// coerceValue attempts to parse s as bool then int64; falls back to string.
func coerceValue(s string) any {
	if b, err := strconv.ParseBool(s); err == nil {
		return b
	}

	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}

	return s
}
