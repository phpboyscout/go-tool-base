// Package xml links XML configuration files into a tool. A blank import
// lets --config, the tool's own files and its embedded defaults be .xml
// files (spec 0204 D2); a tool without it carries none of the format's
// dependencies.
package xml

import (
	configxml "gitlab.com/phpboyscout/go/config-xml"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Format is the format's name in the manifest's config.formats.
const Format = "xml"

func init() {
	setup.RegisterConfigCodec(Format, configxml.Codec{}, ".xml")
}
