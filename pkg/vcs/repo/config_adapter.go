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
	"context"
	"strings"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/config"
	gorepo "gitlab.com/phpboyscout/go/repo"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
)

// SettingsFromProps adapts GTB props into the typed repo settings used by
// NewRepo. It preserves the existing config layout and precedence.
func SettingsFromProps(p *props.Props) gorepo.Settings {
	if p == nil {
		return gorepo.Settings{}
	}

	// Props built without a store (tests, hand-constructed Props) resolve to
	// the same no-auth settings a nil reader does, not a panic on View.
	return SettingsFromReader(p.Tool.ReleaseSource, props.ViewOrNil(p), p.Logger, p.FS)
}

// resolveForge normalises a release source type into the forge name GTB uses
// for config lookup.
//
// The module applies the same normalisation internally to pick the
// git-over-HTTPS username. Because SettingsFromReader stores the result in
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

// SettingsFromReader adapts GTB config into typed repo settings. Runtime
// config remains a framework boundary; the module itself does not depend on it.
//
// It takes [props.ReleaseSource] rather than a forge type because it reads Type
// and Private, and forge.Endpoint carries no Private — that is connection
// identity only. The forge type this used to take existed, by its own doc
// comment, "to avoid a circular import between pkg/vcs/release and pkg/props";
// that cycle is long gone, and SettingsFromProps above already imports props.
func SettingsFromReader(
	source props.ReleaseSource,
	cfg config.Reader,
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

	forgeName := settings.Forge

	// Bind the config subtree now, but defer resolution: the chain walks
	// env → keychain → literal, and a repository authenticating over SSH must
	// never trigger a keychain lookup it does not need. repo.TokenSource is
	// called only on the path that actually authenticates with a token, so the
	// laziness survives the adaptation.
	authCfg := vcs.ConfigFromReader(cfg).Sub(forgeName)
	fallbackEnv := strings.ToUpper(forgeName) + "_TOKEN"
	credential := vcs.ForgeCredential(authCfg, fallbackEnv)

	settings.Token = func() string {
		// TokenSource is func() string by the module's contract, and is called
		// at git-authentication time rather than here — so there is no context
		// to thread. Capturing the construction-time one would be worse: it may
		// be cancelled or scoped to something unrelated by the time this runs.
		//
		// TokenSource also cannot report an error, so a resolution failure is
		// logged and reported as "no token" — the caller then fails at the
		// operation that needed it, with this line explaining why.
		token, err := credential(context.Background()) //nolint:contextcheck // repo.TokenSource takes no ctx and resolves after construction
		if err != nil {
			log.Warn("could not resolve forge credential", "forge", forgeName, "error", err)

			return ""
		}

		return token
	}

	if !cfg.Has(forgeName + ".ssh") {
		return settings
	}

	settings.SSH.Configured = true

	keyPrefix := forgeName + ".ssh.key"
	if !cfg.SectionExists(keyPrefix) {
		return settings
	}

	settings.SSH.HasKey = true
	settings.SSH.Type = cfg.GetString(keyPrefix + ".type")

	// Environment resolution happens here, in the composition root, rather than
	// inside the module: it reads no environment of its own so that every input
	// arrives through Settings. KeyPath applies the documented "explicit path,
	// else named env var" precedence.
	var path string
	if cfg.Has(keyPrefix + ".path") {
		path = cfg.GetString(keyPrefix + ".path")
	}

	settings.SSH.Path = gorepo.KeyPath(path, cfg.GetString(keyPrefix+".env"))

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
