package gitea

import (
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// SettingsFromConfig adapts the gitea config subtree into typed provider
// settings. It preserves the existing `url.api` and `gitea.auth.*` key layout.
func SettingsFromConfig(src release.ReleaseSourceConfig, cfg release.Config, tokenFallbackEnv string) Settings {
	settings := Settings{
		ReleaseSource:    src,
		TokenFallbackEnv: tokenFallbackEnv,
	}
	if cfg == nil {
		return settings
	}

	settings.APIURL = cfg.GetString("url.api")
	if sub := release.SubConfig(cfg, "gitea"); sub != nil {
		settings.Auth = vcs.AuthConfig{
			Env:      sub.GetString("auth.env"),
			Value:    sub.GetString("auth.value"),
			Keychain: sub.GetString("auth.keychain"),
		}
	}

	return settings
}
