package github

import (
	"gitlab.com/phpboyscout/go/forge"
)

// ClientSettingsFromConfig adapts the github config subtree into typed client
// settings. It preserves the existing `url.*` and `auth.*` key layout.
func ClientSettingsFromConfig(src forge.ReleaseSourceConfig, cfg forge.TokenConfig) ClientSettings {
	settings := ClientSettings{ReleaseSource: src}
	if cfg == nil {
		return settings
	}

	settings.APIURL = cfg.GetString("url.api")
	settings.UploadURL = cfg.GetString("url.upload")
	settings.Auth = forge.AuthConfig{
		Env:      cfg.GetString("auth.env"),
		Value:    cfg.GetString("auth.value"),
		Keychain: cfg.GetString("auth.keychain"),
	}

	return settings
}
