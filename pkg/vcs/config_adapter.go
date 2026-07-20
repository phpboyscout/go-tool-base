package vcs

import (
	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go/forge"
)

type configAdapter struct {
	cfg    config.Reader
	prefix string
}

// ConfigFromReader adapts GTB's resolved configuration to the narrow VCS
// release config reader used by release providers.
func ConfigFromReader(cfg config.Reader) forge.Config {
	if cfg == nil {
		return nil
	}

	return configAdapter{cfg: cfg}
}

func (a configAdapter) GetString(key string) string {
	return a.cfg.GetString(a.prefix + key)
}

// Sub returns a reader scoped under key, or nil when the section is absent —
// the contract forge's nil guards rely on, matching what Containable.Sub did.
func (a configAdapter) Sub(key string) forge.Config {
	full := a.prefix + key
	if !a.cfg.SectionExists(full) {
		return nil
	}

	return configAdapter{cfg: a.cfg, prefix: full + "."}
}
