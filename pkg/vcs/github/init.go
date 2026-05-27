package github

import (
	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

func init() {
	release.Register(release.SourceTypeGitHub, func(src release.ReleaseSourceConfig, cfg config.Containable) (release.Provider, error) {
		client, err := NewGitHubClient(src, cfg.Sub("github"))
		if err != nil {
			return nil, err
		}

		return NewReleaseProvider(client), nil
	})
}
