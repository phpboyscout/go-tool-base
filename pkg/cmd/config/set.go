package config

import (
	"fmt"
	"strconv"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	p "github.com/phpboyscout/go-tool-base/pkg/props"
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

			props.Config.Set(key, coerceValue(rawVal))

			v := props.Config.GetViper()
			if err := v.WriteConfig(); err != nil {
				if err2 := v.SafeWriteConfig(); err2 != nil {
					return errors.Wrap(err2, "failed to write config file")
				}
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "set %s = %s\n", key, rawVal)

			return nil
		},
	}

	return cmd
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
