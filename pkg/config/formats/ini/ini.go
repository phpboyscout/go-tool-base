// Package ini links INI configuration files into a tool. A blank import
// lets --config, the tool's own files and its embedded defaults be .ini
// files (spec 0204 D2); a tool without it carries none of the format's
// dependencies.
package ini

import (
	configini "gitlab.com/phpboyscout/go/config-ini"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Format is the format's name in the manifest's config.formats.
const Format = "ini"

func init() {
	setup.RegisterConfigCodec(Format, configini.Codec{}, ".ini")
}
