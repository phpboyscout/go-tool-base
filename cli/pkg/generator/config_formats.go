package generator

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
)

// configFormatPackage is where each format's link package lives in the
// framework (spec 0204 D20).
const configFormatPackage = "gitlab.com/phpboyscout/go-tool-base/pkg/config/formats/"

// configFormats are the formats a tool may link beyond YAML, in the order the
// manifest records them.
var configFormats = []string{"toml", "json", "hcl", "ini", "xml", "dotenv", "properties"}

// writableConfigFormats are the formats a tool's own file may be in.
var writableConfigFormats = []string{"yaml", "toml", "json", "hcl"}

// normaliseConfigFormats drops YAML, which is built in, and repeats, and puts
// the rest in configFormats order, so a manifest reads the same however the
// formats were given. Unknown names are kept for validation to refuse.
func normaliseConfigFormats(formats []string) []string {
	var out []string

	for _, f := range configFormats {
		if slices.Contains(formats, f) {
			out = append(out, f)
		}
	}

	for _, f := range formats {
		if f != "yaml" && !slices.Contains(configFormats, f) && !slices.Contains(out, f) {
			out = append(out, f)
		}
	}

	return out
}

// normaliseOwnFormat records YAML, the default, as nothing.
func normaliseOwnFormat(format string) string {
	if format == "yaml" {
		return ""
	}

	return format
}

// configFormatModules are the link packages cmd/<name>/config.go imports.
func configFormatModules(formats []string) []string {
	var modules []string

	for _, f := range normaliseConfigFormats(formats) {
		if slices.Contains(configFormats, f) {
			modules = append(modules, configFormatPackage+f)
		}
	}

	return modules
}

func configFormatsFile(name string) string {
	return filepath.Join("cmd", name, "config.go")
}

// syncConfigFormatsFile writes cmd/<name>/config.go while the manifest links
// a format beyond YAML and removes it once it links none.
func (g *Generator) syncConfigFormatsFile(name string, formats []string) error {
	modules := configFormatModules(formats)
	if len(modules) == 0 {
		return g.removeGeneratedFile(configFormatsFile(name))
	}

	return g.writeGeneratedGoFile(configFormatsFile(name), templates.SkeletonConfigFormats(modules))
}

// ValidateConfigFormats refuses an unknown format, an own format that cannot
// be written, and an own format the tool does not link (spec 0204 D2).
func ValidateConfigFormats(formats []string, format string) error {
	for _, f := range formats {
		if f != "yaml" && !slices.Contains(configFormats, f) {
			return rejectf("ConfigFormats",
				"unknown config format (valid: yaml, "+strings.Join(configFormats, ", ")+")", f)
		}
	}

	if format == "" {
		return nil
	}

	if !slices.Contains(writableConfigFormats, format) {
		return rejectf("ConfigFormat", "a tool's own config format is yaml, toml, json or hcl", format)
	}

	if format != "yaml" && !slices.Contains(formats, format) {
		return rejectf("ConfigFormat", "the own format must be linked; add it to --config-formats", format)
	}

	return nil
}

// recoverConfigFormats reads the formats cmd/<name>/config.go links on a
// from-scratch rebuild. Nil when the file is absent.
func (g *Generator) recoverConfigFormats() []string {
	matches, err := afero.Glob(g.props.FS, filepath.Join(g.config.Path, "cmd", "*", "config.go"))
	if err != nil || len(matches) == 0 {
		return nil
	}

	src, err := afero.ReadFile(g.props.FS, matches[0])
	if err != nil {
		return nil
	}

	var formats []string

	for _, f := range configFormats {
		if strings.Contains(string(src), `"`+configFormatPackage+f+`"`) {
			formats = append(formats, f)
		}
	}

	return formats
}
