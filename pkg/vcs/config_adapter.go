package vcs

import (
	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go/forge"
)

// configAdapter reads GTB configuration under a prefix with no interpretation.
// It is the view a provider factory reads its subtree through and the one
// ForgeCredential walks, so the dereferencing rungs see the real auth.env and
// auth.keychain values. It used to disguise the chain's result as auth.value
// for the factories; since go/forge v0.30.0 they are handed the chain itself
// (CredentialOption) and read nothing under auth.* on their own.
type configAdapter struct {
	cfg    config.Reader
	prefix string
}

// ConfigFromReader adapts GTB's resolved configuration to the forge config
// reader a provider factory takes. Hand it the ROOT configuration; the
// factory scopes it to the endpoint's section itself.
func ConfigFromReader(cfg config.Reader) forge.Config {
	if cfg == nil {
		return nil
	}

	return configAdapter{cfg: cfg}
}

func (a configAdapter) GetString(key string) string {
	return a.cfg.GetString(a.prefix + key)
}

// Sub returns a reader scoped under key, or nil when the section is absent,
// the contract forge's nil guards rely on, matching what Containable.Sub did.
func (a configAdapter) Sub(key string) forge.Config {
	full := a.prefix + key
	if !a.cfg.SectionExists(full) {
		return nil
	}

	return configAdapter{cfg: a.cfg, prefix: full + "."}
}
