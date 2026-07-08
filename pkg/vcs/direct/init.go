package direct

import "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"

func init() {
	release.Register(release.SourceTypeDirect, func(src release.ReleaseSourceConfig, cfg release.Config) (release.Provider, error) {
		return NewReleaseProvider(SettingsFromConfig(src, cfg))
	})
}
