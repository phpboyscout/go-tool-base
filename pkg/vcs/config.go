package vcs

// AuthConfig is the typed auth shape shared by VCS providers. It still
// satisfies TokenConfig so existing token-resolution semantics are reused.
type AuthConfig struct {
	Env      string `mapstructure:"env"      json:"env"      yaml:"env"`
	Value    string `mapstructure:"value"    json:"value"    yaml:"value"`
	Keychain string `mapstructure:"keychain" json:"keychain" yaml:"keychain"`
}

func (cfg AuthConfig) GetString(key string) string {
	switch key {
	case "auth.env":
		return cfg.Env
	case "auth.value":
		return cfg.Value
	case "auth.keychain":
		return cfg.Keychain
	default:
		return ""
	}
}
