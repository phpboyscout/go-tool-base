package repo

import (
	"strings"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// SettingsFromProps adapts GTB props into the typed repo settings used by
// NewRepo. It preserves the existing config layout and precedence.
func SettingsFromProps(p *props.Props) Settings {
	if p == nil {
		return Settings{}
	}

	source := release.ReleaseSourceConfig{
		Type:    p.Tool.ReleaseSource.Type,
		Host:    p.Tool.ReleaseSource.Host,
		Owner:   p.Tool.ReleaseSource.Owner,
		Repo:    p.Tool.ReleaseSource.Repo,
		Private: p.Tool.ReleaseSource.Private,
		Params:  p.Tool.ReleaseSource.Params,
	}

	return SettingsFromContainable(source, p.Config, p.Logger, p.FS)
}

// SettingsFromContainable adapts GTB config into typed repo settings. Runtime
// config remains a framework boundary; NewRepo itself does not depend on it.
func SettingsFromContainable(source release.ReleaseSourceConfig, cfg config.Containable, log diagnosticLogger, fs afero.Fs) Settings {
	settings := Settings{
		ReleaseSource: source,
		Logger:        log,
		FS:            fs,
	}

	if cfg == nil {
		return settings
	}

	settings.AuthEnabled = true

	if cfg.IsSet("vcs.provider") {
		settings.Forge = strings.ToLower(strings.TrimSpace(cfg.GetString("vcs.provider")))
	}

	forge := resolveForge(settings)
	settings.Auth = vcs.ConfigFromContainable(cfg.Sub(forge))

	if !cfg.Has(forge + ".ssh") {
		return settings
	}

	settings.SSH.Configured = true

	sshCfg := cfg.Sub(forge + ".ssh.key")
	if sshCfg == nil {
		return settings
	}

	settings.SSH.HasKey = true
	settings.SSH.Type = sshCfg.GetString("type")

	settings.SSH.Env = sshCfg.GetString("env")
	if sshCfg.Has("path") {
		settings.SSH.Path = sshCfg.GetString("path")
	}

	return settings
}

// NewRepoFromProps creates a Repo from GTB props.
func NewRepoFromProps(p *props.Props, ops ...RepoOpt) (*Repo, error) {
	return NewRepo(SettingsFromProps(p), ops...)
}

// NewThreadSafeRepoFromProps creates a ThreadSafeRepo from GTB props.
func NewThreadSafeRepoFromProps(p *props.Props, opts ...RepoOpt) (*ThreadSafeRepo, error) {
	return NewThreadSafeRepo(SettingsFromProps(p), opts...)
}
