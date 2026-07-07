package vcs

import (
	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

type configAdapter struct {
	cfg config.Containable
}

// ConfigFromContainable adapts GTB's config container to the narrow VCS release
// config reader used by release providers.
func ConfigFromContainable(cfg config.Containable) release.Config {
	if cfg == nil {
		return nil
	}

	return configAdapter{cfg: cfg}
}

func (a configAdapter) GetString(key string) string {
	return a.cfg.GetString(key)
}

func (a configAdapter) Sub(key string) release.Config {
	return ConfigFromContainable(a.cfg.Sub(key))
}
