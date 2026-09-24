// Package dotenv links dotenv configuration files into a tool. A blank import
// lets --config, the tool's own files and its embedded defaults be .env
// files (spec 0204 D2); a tool without it carries none of the format's
// dependencies.
package dotenv

import (
	configdotenv "gitlab.com/phpboyscout/go/config-dotenv"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Format is the format's name in the manifest's config.formats.
const Format = "dotenv"

func init() {
	setup.RegisterConfigCodec(Format, configdotenv.Codec{}, ".env")
}
