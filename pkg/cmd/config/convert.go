package config

import (
	"fmt"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/errors"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// ErrConvertTargetExists is a convert whose target is already there.
var ErrConvertTargetExists = errors.NewSentinel("gtb.config.convert_target_exists", "the file to convert to already exists")

// NewCmdConvert returns the "config convert" subcommand, which rewrites a
// config file in another format (spec 0204 D14). It reads --from through its
// extension's codec, writes --to through its own, and leaves --from in place.
func NewCmdConvert(props *p.Props) *cobra.Command {
	var from, to string

	cmd := &cobra.Command{
		Use:   "convert --from <file> --to <file>",
		Short: "Rewrite a config file in another format",
		Long: `Rewrite a config file in another format, each chosen by its extension.

Use it when a tool's own config format has changed and it refuses to start on
the file it finds. The values are converted; comments and formatting are not,
so the original is left in place for you to compare and remove.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := convertConfig(writableFS(props), props, from, to); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "converted %s to %s; %s is left in place, remove it once you are satisfied\n", from, to, from)

			return nil
		},
	}

	cmd.Flags().StringVar(&from, "from", "", "the config file to read")
	cmd.Flags().StringVar(&to, "to", "", "the config file to write, which must not exist")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")

	setup.SkipConfigCheck(cmd)

	return cmd
}

func convertConfig(fs afero.Fs, props *p.Props, from, to string) error {
	fromCodec, err := fileCodec(props, from)
	if err != nil {
		return err
	}

	toCodec, err := fileCodec(props, to)
	if err != nil {
		return err
	}

	if exists, _ := afero.Exists(fs, to); exists {
		return errors.WithHintf(errors.Wrapf(ErrConvertTargetExists, "%s", to),
			"move it aside first if you mean to replace it")
	}

	src, err := afero.ReadFile(fs, from)
	if err != nil {
		return errors.Wrapf(err, "reading %s", from)
	}

	doc, err := setup.DecodeConfig(fromCodec, from, src)
	if err != nil {
		return errors.Wrapf(err, "parsing %s", from)
	}

	out, err := setup.EncodeConfig(toCodec, to, doc)
	if err != nil {
		return err
	}

	return errors.Wrapf(afero.WriteFile(fs, to, out, writtenConfigFilePerm), "writing %s", to)
}
