// Package json links JSON configuration files into a tool. A blank import
// lets --config, the tool's own files and its embedded defaults be .json
// files (spec 0204 D2); a tool without it carries none of the format's
// dependencies.
package json

import (
	configjson "gitlab.com/phpboyscout/go/config-json"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Format is the format's name in the manifest's config.formats.
const Format = "json"

func init() {
	setup.RegisterConfigCodec(Format, configjson.Codec{}, ".json")
}
