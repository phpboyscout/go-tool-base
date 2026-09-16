// Package settings is the gtb-only `set`, `unset` and `get` commands: one
// setter over the manifest's author settings, by dotted path, applying the
// same validation as the generate flags and the same derived-file sync as
// regenerate (spec 0197 D6).
package settings

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// NewCmdSet returns `gtb set <path> <value>...`.
func NewCmdSet(p *props.Props) *setup.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "set <setting> <value>...",
		Short: "Change an author setting of a generated project",
		Long: `Write one author setting into .gtb/manifest.yaml by its dotted path, validate
it the way the generate flags are validated, and bring the generated files
into line, so no regenerate is needed afterwards.

The path is the manifest's: 'chat.default.provider', 'telemetry.endpoint',
'bootstrap.skip_config_check', 'release_source.repo', 'version.go'. The
'properties.' prefix may be left off. A list takes several values or a
comma-separated one; a bool takes true or false. Features and templates have
their own commands (enable, disable, template) and are refused here; fields
the generator records (hashes, commands) are too.`,
		Example: `  gtb set chat.default.provider openai
  gtb set telemetry.endpoint https://telemetry.example.internal
  gtb set bootstrap.skip_config_check version doctor
  gtb set version.go 1.27`,
		Args: cobra.MinimumNArgs(2), //nolint:mnd // a path and at least one value
		RunE: func(cmd *cobra.Command, args []string) error {
			return newGenerator(p, path).SetSetting(cmd.Context(), args[0], args[1:])
		},
	}

	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")

	return setup.Wrap("", cmd)
}

// NewCmdUnset returns `gtb unset <path>`.
func NewCmdUnset(p *props.Props) *setup.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "unset <setting>",
		Short: "Clear an author setting of a generated project",
		Long: `Zero one author setting in .gtb/manifest.yaml and bring the generated files
into line. The path vocabulary is 'gtb set''s.`,
		Example: `  gtb unset chat.default.model`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return newGenerator(p, path).UnsetSetting(cmd.Context(), args[0])
		},
	}

	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")

	return setup.Wrap("", cmd)
}

// NewCmdGet returns `gtb get <path>`.
func NewCmdGet(p *props.Props) *setup.Command {
	var path string

	cmd := &cobra.Command{
		Use:     "get <setting>",
		Short:   "Read an author setting of a generated project",
		Long:    `Print one author setting from .gtb/manifest.yaml, one value per line.`,
		Example: `  gtb get chat.providers`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			values, err := newGenerator(p, path).GetSetting(args[0])
			if err != nil {
				return err
			}

			if len(values) > 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(values, "\n"))
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to project root")

	return setup.Wrap("", cmd)
}

func newGenerator(p *props.Props, path string) *generator.Generator {
	return generator.New(p, &generator.Config{Path: path, Overwrite: "allow"})
}
