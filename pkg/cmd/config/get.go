package config

import (
	"fmt"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/output"
	ocobra "gitlab.com/phpboyscout/go/output/cobra"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// NewCmdGet returns the "config get <key>" subcommand.
func NewCmdGet(props *p.Props, masker *Masker) *cobra.Command {
	var unmask bool

	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Long: `Read and display a single resolved configuration value by its dot-notation key.

The value reflects the full resolution chain (flags, env vars, file, defaults).
Sensitive values are masked unless --unmask is passed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			if props.Config == nil {
				return errors.Newf("config key %q not found", key)
			}

			view := props.Config.View()
			if !view.IsSet(key) {
				return errors.Newf("config key %q not found", key)
			}

			value := fmt.Sprintf("%v", view.Get(key))

			if !unmask {
				value = masker.MaskIfSensitive(key, value)
			}

			if ocobra.IsJSONOutput(cmd) {
				return ocobra.Emit(cmd, output.Response{
					Status:  output.StatusSuccess,
					Command: "config get",
					Data:    map[string]string{"key": key, "value": value},
				})
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), value)

			return nil
		},
	}

	cmd.Flags().BoolVar(&unmask, "unmask", false, "show sensitive values unmasked")

	return cmd
}
