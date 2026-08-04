package config

import (
	"fmt"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/output"
	ocobra "gitlab.com/phpboyscout/go/output/cobra"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// pathRoleContributing marks a file that contributed to the live config.
const pathRoleContributing = "contributing"

// pathRoleWritable marks the single file that set/unset/edit write to.
const pathRoleWritable = "writable"

// pathEntry is one row of "config path" output: a config file and the role it
// plays in the merge. Struct tags drive the json/yaml/table renderers, matching
// configEntry in list.go.
type pathEntry struct {
	Path string `json:"path"  yaml:"path"  table:"PATH,sortable"`
	Role string `json:"role"  yaml:"role"  table:"ROLE"`
}

// NewCmdPath returns the "config path" subcommand. It prints the ordered list
// of config files that contributed to the live configuration (in merge order)
// followed by the writable target that set/unset/edit mutate. With --writable
// it prints only that target, for use in scripts.
func NewCmdPath(props *p.Props) *cobra.Command {
	var writable bool

	cmd := &cobra.Command{
		Use:   "path",
		Short: "Show the resolved config file path(s)",
		Long: `Print the config files that contributed to the live configuration.

Because configuration is merged across a precedence chain, several files can
contribute. By default this prints each contributing file in merge order plus
the writable target that "set", "unset", and "edit" mutate. Use --writable to
print only that target.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if props.Config == nil {
				return errors.New("no configuration loaded")
			}

			fs := writableFS(props)

			target := resolveWritableConfigPath(props, fs)
			if target == "" {
				return errors.New("could not resolve a writable config path")
			}

			if writable {
				return emitWritable(cmd, target)
			}

			return emitPathList(cmd, p.ConfigFileSources(props.Config.Snapshot()), target)
		},
	}

	cmd.Flags().BoolVar(&writable, "writable", false,
		"print only the writable target path that set/unset/edit mutate")

	return cmd
}

// emitWritable prints just the writable target — a bare line in text mode (for
// scripting) or a structured single value under --output json.
func emitWritable(cmd *cobra.Command, target string) error {
	if ocobra.IsJSONOutput(cmd) {
		return ocobra.Emit(cmd, output.Response{
			Status:  output.StatusSuccess,
			Command: "config path",
			Data:    map[string]string{"writable": target},
		})
	}

	_, _ = fmt.Fprintln(cmd.OutOrStdout(), target)

	return nil
}

// emitPathList prints the contributing files (merge order preserved) plus the
// writable target, honouring the global --output flag via the table writer.
// When no file is loaded it prints only the writable target and notes that no
// file is currently loaded — it does not error.
func emitPathList(cmd *cobra.Command, files []string, target string) error {
	rows := make([]pathEntry, 0, len(files)+1)
	for _, f := range files {
		rows = append(rows, pathEntry{Path: f, Role: pathRoleContributing})
	}

	rows = append(rows, pathEntry{Path: target, Role: pathRoleWritable})

	format, _ := cmd.Flags().GetString("output")
	tw := output.New(output.WithWriter(cmd.OutOrStdout()), output.WithFormat(output.Format(format)))

	if err := tw.Table(rows); err != nil {
		return err
	}

	// The note is for the human-facing text renderer only; structured formats
	// (json/yaml/…) carry just the rows.
	if len(files) == 0 && (format == "" || output.Format(format) == output.FormatText) {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(),
			"# no config file is currently loaded; the writable path is where a future write would land")
	}

	return nil
}
