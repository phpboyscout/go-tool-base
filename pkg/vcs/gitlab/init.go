package gitlab

import (
	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

func init() {
	release.Register(release.SourceTypeGitLab, func(src release.ReleaseSourceConfig, cfg config.Containable) (release.Provider, error) {
		return NewReleaseProvider(src, cfg.Sub("gitlab"))
	})
}
