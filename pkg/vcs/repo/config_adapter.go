// Package repo adapts GTB's runtime configuration into the typed settings used
// by the standalone gitlab.com/phpboyscout/go/repo module.
//
// The git operations themselves live in that module, which depends on no forge
// abstraction, no config container and no DI container. This package is the
// composition root that resolves those GTB concerns — release source, config
// subtree, credential chain, SSH key location — and hands the module plain
// data. Nothing here is re-exported: callers import the module directly for its
// own types.
package repo

import (
	"strings"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/config"
	gorepo "gitlab.com/phpboyscout/go/repo"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// SettingsFromProps adapts GTB props into the typed repo settings used by
// NewRepo. It preserves the existing config layout and precedence.
func SettingsFromProps(p *props.Props) gorepo.Settings {
	if p == nil {
		return gorepo.Settings{}
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

// resolveForge normalises a release source type into the forge name GTB uses
// for config lookup.
//
// The module applies the same normalisation internally to pick the
// git-over-HTTPS username. Because SettingsFromContainable stores the result in
// Settings.Forge, that internal pass is a no-op — the config subtree, the
// fallback environment variable and the auth convention are guaranteed to agree
// rather than agreeing by coincidence.
func resolveForge(forge string) string {
	forge = strings.ToLower(strings.TrimSpace(forge))

	// "direct" is a plain download source with no git remote, so it has no
	// forge convention of its own; fall back to GitHub's.
	if forge == "" || forge == gorepo.ForgeDirect {
		return gorepo.ForgeGitHub
	}

	return forge
}

// SettingsFromContainable adapts GTB config into typed repo settings. Runtime
// config remains a framework boundary; the module itself does not depend on it.
func SettingsFromContainable(
	source release.ReleaseSourceConfig,
	cfg config.Containable,
	log gorepo.Logger,
	fs afero.Fs,
) gorepo.Settings {
	settings := gorepo.Settings{
		// The release source names the forge unless config overrides it below.
		Forge:   resolveForge(source.Type),
		Private: source.Private,
		Logger:  log,
		FS:      fs,
	}

	if cfg == nil {
		return settings
	}

	settings.AuthEnabled = true

	if cfg.IsSet("vcs.provider") {
		settings.Forge = resolveForge(cfg.GetString("vcs.provider"))
	}

	forge := settings.Forge

	// Bind the config subtree now, but defer resolution: ResolveToken walks the
	// env → keychain → literal chain, and a repository authenticating over SSH
	// must never trigger a keychain lookup it does not need.
	authCfg := vcs.ConfigFromContainable(cfg.Sub(forge))
	fallbackEnv := strings.ToUpper(forge) + "_TOKEN"
	settings.Token = func() string { return vcs.ResolveToken(authCfg, fallbackEnv) }

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

	// Environment resolution happens here, in the composition root, rather than
	// inside the module: it reads no environment of its own so that every input
	// arrives through Settings. KeyPath applies the documented "explicit path,
	// else named env var" precedence.
	var path string
	if sshCfg.Has("path") {
		path = sshCfg.GetString("path")
	}

	settings.SSH.Path = gorepo.KeyPath(path, sshCfg.GetString("env"))

	return settings
}

// NewRepoFromProps creates a Repo from GTB props.
func NewRepoFromProps(p *props.Props, ops ...gorepo.RepoOpt) (*gorepo.Repo, error) {
	return gorepo.NewRepo(SettingsFromProps(p), ops...)
}

// NewThreadSafeRepoFromProps creates a ThreadSafeRepo from GTB props.
func NewThreadSafeRepoFromProps(p *props.Props, opts ...gorepo.RepoOpt) (*gorepo.ThreadSafeRepo, error) {
	return gorepo.NewThreadSafeRepo(SettingsFromProps(p), opts...)
}
