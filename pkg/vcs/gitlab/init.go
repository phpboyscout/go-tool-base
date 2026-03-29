package gitlab

import (
	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

func init() {
	release.Register(release.SourceTypeGitLab, func(_ release.ReleaseSourceConfig, cfg config.Containable) (release.Provider, error) {
		return NewReleaseProvider(cfg.Sub("gitlab"))
	})
}
