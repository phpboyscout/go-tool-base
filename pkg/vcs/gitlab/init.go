package gitlab

import "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"

func init() {
	release.Register(release.SourceTypeGitLab, func(src release.ReleaseSourceConfig, cfg release.Config) (release.Provider, error) {
		return NewReleaseProvider(src, release.SubConfig(cfg, "gitlab"))
	})
}
