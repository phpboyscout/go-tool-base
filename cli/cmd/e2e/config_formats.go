package main

import (
	configtoml "gitlab.com/phpboyscout/go/config-toml"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// The e2e binary reads TOML so the CLI scenarios can pass a --config file in a
// linked format and be refused one in a format it does not link (spec 0204
// D2). It registers the codec the way pkg/config/formats/toml does rather than
// importing it: cli/go.mod is tidied against the released framework, which
// predates that package.
func init() {
	setup.RegisterConfigCodec("toml", configtoml.Codec{}, ".toml")
}
