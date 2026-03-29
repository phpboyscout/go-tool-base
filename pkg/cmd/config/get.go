package config

import (
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/pkg/output"
	p "github.com/phpboyscout/go-tool-base/pkg/props"
)

// NewCmdGet returns the "config get <key>" subcommand.
func NewCmdGet(props *p.Props, masker *Masker) *cobra.Command {
	var unmask bool

	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Long:  "Read and display a single configuration value by its dot-notation key (e.g. log.level, github.url.api).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			if props.Config == nil || !props.Config.IsSet(key) {
				return errors.Newf("config key %q not found", key)
			}

			value := fmt.Sprintf("%v", props.Config.Get(key))

			if !unmask {
				value = masker.MaskIfSensitive(key, value)
			}

			if output.IsJSONOutput(cmd) {
				return output.Emit(cmd, output.Response{
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
