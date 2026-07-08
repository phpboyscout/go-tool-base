package bitbucket

import "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"

// Settings contains the typed configuration needed to construct a Bitbucket
// release provider without binding the provider to GTB config.
type Settings struct {
	ReleaseSource   release.ReleaseSourceConfig
	Username        string
	AppPassword     string
	FilenamePattern string
}
