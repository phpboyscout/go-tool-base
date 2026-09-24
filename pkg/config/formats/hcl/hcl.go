// Package hcl links HCL configuration files into a tool. A blank import
// lets --config, the tool's own files and its embedded defaults be .hcl
// files (spec 0204 D2); a tool without it carries none of the format's
// dependencies.
package hcl

import (
	confighcl "gitlab.com/phpboyscout/go/config-hcl"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Format is the format's name in the manifest's config.formats.
const Format = "hcl"

func init() {
	setup.RegisterConfigCodec(Format, confighcl.Codec{}, ".hcl")
}
