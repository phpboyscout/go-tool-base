package direct

import "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"

// SettingsFromConfig adapts the direct config subtree into typed provider
// settings. It preserves the existing `direct.token` key layout.
func SettingsFromConfig(src release.ReleaseSourceConfig, cfg release.Config) Settings {
	settings := Settings{ReleaseSource: src}
	if cfg == nil {
		return settings
	}

	if sub := release.SubConfig(cfg, "direct"); sub != nil {
		settings.Token = sub.GetString("token")
	}

	return settings
}
