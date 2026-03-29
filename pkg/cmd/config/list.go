package config

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/pkg/output"
	p "github.com/phpboyscout/go-tool-base/pkg/props"
)

// configEntry represents a single resolved config key/value pair.
type configEntry struct {
	Key   string `json:"key"   yaml:"key"   table:"KEY,sortable"`
	Value string `json:"value" yaml:"value" table:"VALUE"`
}

// NewCmdList returns the "config list" subcommand.
func NewCmdList(props *p.Props, masker *Masker) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all configuration values",
		Long:  "Display all resolved configuration keys and values. Sensitive values are masked by default.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if props.Config == nil {
				return errors.New("no configuration loaded")
			}

			entries := flattenSettings(props.Config.GetViper().AllSettings(), "", masker)

			slices.SortFunc(entries, func(a, b configEntry) int {
				return cmp.Compare(a.Key, b.Key)
			})

			format, _ := cmd.Flags().GetString("output")
			tw := output.NewTableWriter(cmd.OutOrStdout(), output.Format(format))

			return tw.WriteRows(entries)
		},
	}

	return cmd
}

// flattenSettings recursively flattens a nested map into dot-notation configEntry slice.
func flattenSettings(m map[string]any, prefix string, masker *Masker) []configEntry {
	entries := make([]configEntry, 0, len(m))

	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}

		switch val := v.(type) {
		case map[string]any:
			entries = append(entries, flattenSettings(val, key, masker)...)
		default:
			raw := fmt.Sprintf("%v", val)
			entries = append(entries, configEntry{
				Key:   key,
				Value: masker.MaskIfSensitive(key, raw),
			})
		}
	}

	return entries
}
