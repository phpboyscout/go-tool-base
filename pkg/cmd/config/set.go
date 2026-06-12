package config

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

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

			val := coerceValue(rawVal)
			props.Config.Set(key, val)

			if err := persistConfigValue(props, key, val); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "set %s = %s\n", key, rawVal)

			return nil
		},
	}

	return cmd
}

// persistConfigValue writes a single key into the user's config file via a
// read-modify-write. It resolves the file the loaded config is bound to, or —
// in the embedded-merge configuration where the active viper has no file —
// the user's default config path. Only the user's own file is rewritten, so
// embedded defaults are never materialised into it.
func persistConfigValue(props *p.Props, key string, val any) error {
	fs := writableFS(props)

	path := resolveWritableConfigPath(props, fs)
	if path == "" {
		return errors.New("could not resolve a writable config path")
	}

	settings := map[string]any{}
	if data, err := afero.ReadFile(fs, path); err == nil {
		if uerr := yaml.Unmarshal(data, &settings); uerr != nil {
			return errors.Wrapf(uerr, "parsing existing config %q", path)
		}

		if settings == nil {
			settings = map[string]any{}
		}
	}

	setNestedKey(settings, key, val)

	data, err := yaml.Marshal(settings)
	if err != nil {
		return errors.Wrap(err, "marshalling config")
	}

	return writeConfigAtomic(fs, path, data)
}

// resolveWritableConfigPath returns the file the loaded config is bound to, or
// the user's default config path when none is bound.
func resolveWritableConfigPath(props *p.Props, fs afero.Fs) string {
	if props.Config != nil {
		if v := props.Config.GetViper(); v != nil {
			if used := v.ConfigFileUsed(); used != "" {
				return used
			}
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
