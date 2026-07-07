package github

import (
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// ClientSettings contains the typed configuration NewGitHubClient needs.
type ClientSettings struct {
	ReleaseSource release.ReleaseSourceConfig
	APIURL        string `mapstructure:"url.api"    json:"api_url"    yaml:"api_url"`
	UploadURL     string `mapstructure:"url.upload" json:"upload_url" yaml:"upload_url"`
	Auth          vcs.AuthConfig
}
