package repo

import (
	"github.com/spf13/afero"
)

// Forge identifiers understood by the git-authentication paths. They select the
// username git-over-HTTPS expects for token auth; any other value falls back to
// the GitHub convention.
//
// These are plain strings rather than an enum so a caller can name a forge this
// package has never heard of, and so the package needs no dependency on a forge
// or release abstraction merely to identify one.
const (
	ForgeGitHub    = "github"
	ForgeGitLab    = "gitlab"
	ForgeBitbucket = "bitbucket"
	ForgeGitea     = "gitea"
	ForgeCodeberg  = "codeberg"
	ForgeDirect    = "direct"
)

type diagnosticLogger interface {
	Debug(msg string, keyvals ...any)
	Warn(msg string, keyvals ...any)
}

// TokenSource returns an access token for git-over-HTTPS authentication.
//
// It is a function rather than a resolved string because resolution can be
// expensive or interactive — a token may live in the OS keychain, where an
// eager lookup could prompt the user to unlock it. The repository calls the
// source only on the code path that actually authenticates with a token: SSH
// authentication takes priority, and a repository configured for SSH never
// invokes it at all.
//
// Use [StaticToken] when the token is already in hand.
type TokenSource func() string

// StaticToken adapts an already-resolved token to a [TokenSource].
// An empty token is treated as "no token available".
func StaticToken(token string) TokenSource {
	return func() string { return token }
}

// resolve returns the token, tolerating a nil source.
func (t TokenSource) resolve() string {
	if t == nil {
		return ""
	}

	return t()
}

// Settings contains the typed configuration NewRepo needs to resolve git
// authentication without depending on a forge abstraction, a config container,
// or a DI container. Callers resolve those concerns and pass the results here.
type Settings struct {
	// Forge selects the git-over-HTTPS authentication convention — one of the
	// Forge* constants, or any other string (treated as GitHub). Empty defaults
	// to GitHub.
	Forge string

	// Private marks the target repository as private, which makes a missing
	// token a fast failure rather than an unauthenticated attempt.
	Private bool

	// AuthEnabled requests token authentication even when no token has been
	// supplied, so a missing credential is reported rather than silently skipped.
	AuthEnabled bool

	// Token supplies the access token, resolved lazily. See [TokenSource].
	Token TokenSource

	SSH    SSHSettings
	Logger diagnosticLogger
	FS     afero.Fs
}

// SSHSettings describes the forge SSH key configuration resolved by adapter
// code. Configured tracks whether the forge's SSH block exists at all; HasKey
// distinguishes a present-but-scalar SSH block from a structured ssh.key block.
type SSHSettings struct {
	Configured bool
	HasKey     bool
	Type       string
	Env        string
	Path       string
}
