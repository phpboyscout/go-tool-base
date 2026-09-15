package vcs

import (
	"context"
	"strings"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go/forge"
)

// rawAdapter reads GTB configuration under a prefix with no interpretation.
// It is what ForgeCredential walks, so the dereferencing rungs see the real
// auth.env and auth.keychain values.
type rawAdapter struct {
	cfg    config.Reader
	prefix string
}

func (a rawAdapter) GetString(key string) string {
	return a.cfg.GetString(a.prefix + key)
}

// Sub returns a reader scoped under key, or nil when the section is absent,
// the contract forge's nil guards rely on, matching what Containable.Sub did.
func (a rawAdapter) Sub(key string) forge.Config {
	full := a.prefix + key
	if !a.cfg.SectionExists(full) {
		return nil
	}

	return rawAdapter{cfg: a.cfg, prefix: full + "."}
}

// configAdapter is the forge-facing view of GTB configuration. GTB owns the
// credential precedence (ForgeCredential, spec 0183): auth.env and
// auth.keychain are pointers GTB dereferences, and go/forge v0.8.0 stopped
// reading them, so a provider factory that finds them beside an empty
// auth.value reports the configuration as stale and fails construction. That
// is what every consumer hit in a bare CI image, where GTB's shipped
// `<forge>.auth.env: <FORGE>_TOKEN` default was the only auth key present
// (#76). The view resolves GTB's chain into auth.value and shows the pointer
// keys as unset, so a factory's own composition (ConfigCredential, then the
// well-known variable) sees a credential or an honest absence.
type configAdapter struct {
	raw rawAdapter
}

// ConfigFromReader adapts GTB's resolved configuration to the forge config
// reader a provider factory takes. Hand it the ROOT configuration; the
// factory scopes it to the endpoint's section itself.
func ConfigFromReader(cfg config.Reader) forge.Config {
	if cfg == nil {
		return nil
	}

	return configAdapter{raw: rawAdapter{cfg: cfg}}
}

const (
	authEnvSuffix      = "." + authEnvKey
	authKeychainSuffix = "." + authKeychainKey
	authValueSuffix    = "." + authValueKey
)

func (a configAdapter) GetString(key string) string {
	full := a.raw.prefix + key

	switch {
	case strings.HasSuffix(full, authEnvSuffix), strings.HasSuffix(full, authKeychainSuffix):
		return ""
	case strings.HasSuffix(full, authValueSuffix):
		return a.resolveCredential(strings.TrimSuffix(full, authValueSuffix))
	default:
		return a.raw.GetString(key)
	}
}

// resolveCredential runs GTB's chain over the forge section that owns the
// auth block. A factory resolves its credential once, at construction, which
// is when GTB would dereference the pointers anyway; an error is an absence
// here, and doctor's per-forge credential check is where it is explained.
func (a configAdapter) resolveCredential(section string) string {
	forgeName, _, _ := strings.Cut(section, ".")
	fallbackEnv := strings.ToUpper(forgeName) + "_TOKEN"

	sub := rawAdapter{cfg: a.raw.cfg, prefix: section + "."}

	value, err := ForgeCredential(sub, fallbackEnv)(context.Background())
	if err != nil {
		return ""
	}

	return value
}

// Sub returns the forge-facing view scoped under key, or nil when the section
// is absent.
func (a configAdapter) Sub(key string) forge.Config {
	sub := a.raw.Sub(key)
	if sub == nil {
		return nil
	}

	raw, _ := sub.(rawAdapter)

	return configAdapter{raw: raw}
}
