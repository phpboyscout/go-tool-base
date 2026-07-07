package gitea

import "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"

func init() {
	release.Register(release.SourceTypeGitea, func(src release.ReleaseSourceConfig, cfg release.Config) (release.Provider, error) {
		return NewReleaseProvider(src, cfg, giteaTokenEnv)
	})

	release.Register(release.SourceTypeCodeberg, func(src release.ReleaseSourceConfig, cfg release.Config) (release.Provider, error) {
		// Pre-configure the host for Codeberg so users don't need to set Host.
		if src.Host == "" {
			src.Host = CodebergHost
		}

		return NewReleaseProvider(src, cfg, codebergTokenEnv)
	})
}
