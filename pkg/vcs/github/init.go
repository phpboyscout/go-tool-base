package github

import "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"

func init() {
	release.Register(release.SourceTypeGitHub, func(src release.ReleaseSourceConfig, cfg release.Config) (release.Provider, error) {
		client, err := NewGitHubClient(ClientSettingsFromConfig(src, release.SubConfig(cfg, "github")))
		if err != nil {
			return nil, err
		}

		return NewReleaseProvider(client), nil
	})
}
