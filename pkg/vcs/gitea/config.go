package gitea

import (
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// Settings contains the typed configuration needed to construct a Gitea release
// provider without binding the provider to GTB config.
type Settings struct {
	ReleaseSource    release.ReleaseSourceConfig
	APIURL           string `mapstructure:"url.api" json:"api_url" yaml:"api_url"`
	Auth             vcs.AuthConfig
	TokenFallbackEnv string
}
