package direct

import "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"

// Settings contains the typed configuration needed to construct a Direct
// release provider without binding the provider to GTB config.
type Settings struct {
	ReleaseSource release.ReleaseSourceConfig
	Token         string `mapstructure:"token" json:"token" yaml:"token"`
}
