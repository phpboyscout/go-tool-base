// Package toml links TOML configuration files into a tool. A blank import
// lets --config, the tool's own files and its embedded defaults be .toml
// files (spec 0204 D2); a tool without it carries none of the format's
// dependencies.
package toml

import (
	configtoml "gitlab.com/phpboyscout/go/config-toml"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Format is the format's name in the manifest's config.formats.
const Format = "toml"

func init() {
	setup.RegisterConfigCodec(Format, configtoml.Codec{}, ".toml")
}
