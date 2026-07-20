package github

import (
	"gitlab.com/phpboyscout/go/forge"
)

// ClientSettings contains the typed configuration NewGitHubClient needs.
//
// Fields are populated by the GTB config adapter (ClientSettingsFromConfig) via
// the narrow forge.TokenConfig seam, not decoded with mapstructure. The json/yaml
// tags are for documentation and serialisation only.
type ClientSettings struct {
	ReleaseSource forge.ReleaseSourceConfig
	APIURL        string `json:"api_url"    yaml:"api_url"`
	UploadURL     string `json:"upload_url" yaml:"upload_url"`
	Auth          forge.AuthConfig
}
