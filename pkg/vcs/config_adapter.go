package vcs

import (
	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go/forge"
)

type configAdapter struct {
	cfg config.Containable
}

// ConfigFromContainable adapts GTB's config container to the narrow VCS release
// config reader used by release providers.
func ConfigFromContainable(cfg config.Containable) forge.Config {
	if cfg == nil {
		return nil
	}

	return configAdapter{cfg: cfg}
}

func (a configAdapter) GetString(key string) string {
	return a.cfg.GetString(key)
}

func (a configAdapter) Sub(key string) forge.Config {
	return ConfigFromContainable(a.cfg.Sub(key))
}
