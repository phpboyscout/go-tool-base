// Package properties links Java properties configuration files into a tool. A blank import
// lets --config, the tool's own files and its embedded defaults be .properties
// files (spec 0204 D2); a tool without it carries none of the format's
// dependencies.
package properties

import (
	configproperties "gitlab.com/phpboyscout/go/config-properties"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Format is the format's name in the manifest's config.formats.
const Format = "properties"

func init() {
	setup.RegisterConfigCodec(Format, configproperties.Codec{}, ".properties")
}
