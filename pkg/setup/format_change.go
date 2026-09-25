package setup

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// ErrConfigFormatChanged is a tool whose own config file is in its previous
// format and absent in its current one (spec 0204 D14).
var ErrConfigFormatChanged = errors.NewSentinel("gtb.setup.config_format_changed", "config file is in the tool's previous format")

// ownConfigFormats are the formats a tool's own file can have been in.
var ownConfigFormats = []string{"yaml", "toml", "json", "hcl"}

// DefaultConfigPaths are where a tool looks for its own config file when
// --config names none, lowest precedence first: the system-wide file, then
// the user's.
func DefaultConfigPaths(p *props.Props) []string {
	name := p.Tool.ConfigFilename()

	return []string{
		fmt.Sprintf("%s%s", string(os.PathSeparator), filepath.Join("etc", p.Tool.Name, name)),
		filepath.Join(GetDefaultConfigDir(p.FS, p.Tool.Name), name),
	}
}

// StrandedConfig returns the first of paths that names the tool's own config
// file, is absent, and has a file in another own format beside it: the file
// left in the old format and the one the tool now reads. Both are empty when
// nothing is stranded.
func StrandedConfig(fs afero.Fs, tool props.Tool, paths []string) (old, want string) {
	own := tool.ConfigFilename()

	for _, path := range paths {
		if filepath.Base(path) != own || exists(fs, path) {
			continue
		}

		for _, format := range ownConfigFormats {
			candidate := filepath.Join(filepath.Dir(path), props.Tool{Config: props.ConfigSpec{Format: format}}.ConfigFilename())
			if candidate != path && exists(fs, candidate) {
				return candidate, path
			}
		}
	}

	return "", ""
}

// ConfigFormatChangedError refuses a stranded config file, naming both files
// and the way out. The old file is never converted for the user: comments and
// formatting do not survive, and nobody would be watching.
func ConfigFormatChangedError(p *props.Props, old, want string) error {
	err := errors.Wrapf(ErrConfigFormatChanged, "found %s but not %s", old, want)

	if p.GetFeatures().Enabled(props.ConfigCmd) {
		return errors.WithHintf(err,
			"run `%s config convert --from %s --to %s`; the old file is left in place for you to remove",
			p.Tool.Name, old, want)
	}

	return errors.WithHintf(err,
		"convert %s to %s by hand, or move it aside to start with a fresh configuration", old, want)
}

func exists(fs afero.Fs, path string) bool {
	_, err := fs.Stat(path)

	return err == nil
}
