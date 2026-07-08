package gitlab

import (
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// SettingsFromConfig adapts the gitlab config subtree into typed provider
// settings. It preserves the existing `url.api` and `auth.*` key layout.
func SettingsFromConfig(src release.ReleaseSourceConfig, cfg vcs.TokenConfig) Settings {
	settings := Settings{ReleaseSource: src}
	if cfg == nil {
		return settings
	}

	settings.APIURL = cfg.GetString("url.api")
	settings.Auth = vcs.AuthConfig{
		Env:      cfg.GetString("auth.env"),
		Value:    cfg.GetString("auth.value"),
		Keychain: cfg.GetString("auth.keychain"),
	}

	return settings
}
