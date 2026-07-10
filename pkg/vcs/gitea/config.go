package gitea

import (
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// Settings contains the typed configuration needed to construct a Gitea release
// provider without binding the provider to GTB config.
//
// Fields are populated by the GTB config adapter via the narrow vcs.TokenConfig
// seam, not decoded with mapstructure. The json/yaml tags are for documentation
// and serialisation only.
type Settings struct {
	ReleaseSource    release.ReleaseSourceConfig
	APIURL           string `json:"api_url" yaml:"api_url"`
	Auth             vcs.AuthConfig
	TokenFallbackEnv string
}
