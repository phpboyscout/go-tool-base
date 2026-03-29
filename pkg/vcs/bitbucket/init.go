package bitbucket

import (
	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

func init() {
	release.Register(release.SourceTypeBitbucket, func(src release.ReleaseSourceConfig, cfg config.Containable) (release.Provider, error) {
		return NewReleaseProvider(src, cfg)
	})
}
