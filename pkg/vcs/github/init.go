package github

import (
	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

func init() {
	release.Register(release.SourceTypeGitHub, func(_ release.ReleaseSourceConfig, cfg config.Containable) (release.Provider, error) {
		client, err := NewGitHubClient(cfg.Sub("github"))
		if err != nil {
			return nil, err
		}

		return NewReleaseProvider(client), nil
	})
}
