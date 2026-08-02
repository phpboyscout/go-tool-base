package forge

import (
	"embed"
	"os"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/config"

	forgeapi "gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
)

// PackagePath is this package's import path — the ConstPackage its feature
// descriptors carry, so generated source qualifies these constants against the
// package that actually declares them rather than assuming props.
const PackagePath = "gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"

// Feature identities for the forge integrations this package registers.
//
// They carry no "Cmd" suffix: unlike the framework's built-ins, a forge feature
// does not exist to gate a command. It gates an initialiser, a config section
// and an embedded asset bundle — the `init <forge>` subcommand it also
// contributes is incidental. See spec 0184 D10.
const (
	// GithubFeature gates the GitHub single-token setup wizard.
	GithubFeature = props.FeatureID("github")
	// BitbucketFeature gates the Bitbucket dual-credential setup wizard.
	BitbucketFeature = props.FeatureID("bitbucket")
)

// Registering the forge features makes them visible to everything that
// enumerates the feature set — most immediately the doctor support bundle,
// which ranges over props.AllFeatures and had been silently omitting both.
//
// Neither may be default-enabled: a blank import changes what is available,
// never what is on (props.ErrPluginDefaultOn).
//
//nolint:gochecknoinits // blank-import registration is the mechanism
func init() {
	props.RegisterFeature(props.FeatureDescriptor{
		ID:           GithubFeature,
		ConstName:    "GithubFeature",
		ConstPackage: PackagePath,
		Kind:         props.KindForge,
	})
	props.RegisterFeature(props.FeatureDescriptor{
		ID:           BitbucketFeature,
		ConstName:    "BitbucketFeature",
		ConstPackage: PackagePath,
		Kind:         props.KindForge,
	})
}

// CredentialShape discriminates the credential layout a forge setup wizard
// captures: a single API token, or a username + app-password pair.
type CredentialShape int

const (
	// SingleToken is a one-credential wizard (e.g. GitHub): three storage
	// modes over a single token, optional forge-driven login and SSH upload.
	SingleToken CredentialShape = iota
	// DualUserPass is a two-credential wizard (e.g. Bitbucket): three storage
	// modes over a username + app-password pair, no login, no SSH.
	DualUserPass
)

// Profile describes a forge's interactive setup shape. One generic
// [Initialiser] is parameterised by a Profile so GitHub and Bitbucket (and any
// future forge) share the storage-mode wizard, the single-credential-key
// exclusivity invariant, and the config-write plumbing — differing only in the
// per-provider values collected here.
type Profile struct {
	// Provider is the forge registry key passed to [forgeapi.Lookup]
	// (e.g. "github").
	Provider string
	// ConfigPrefix is the config-key namespace (e.g. "github" →
	// "github.auth.env"). It reproduces each provider's existing key layout
	// exactly so pre-existing configs keep resolving.
	ConfigPrefix string
	// Label is the human-facing provider name used in prompts and errors
	// (e.g. "GitHub").
	Label string
	// DisplayName is the initialiser's Name() label shown by the setup
	// runner (e.g. "GitHub integration").
	DisplayName string
	// Feature is the feature flag that gates this initialiser.
	Feature props.FeatureID
	// Host is the default web host: the manual-token URL host and the forge
	// provider's release-source host.
	Host string
	// KeychainAccount is the account portion of the "<service>/<account>"
	// keychain reference (e.g. "github.auth").
	KeychainAccount string
	// Credential selects the single-token or dual-credential flow.
	Credential CredentialShape

	// FallbackEnv is the well-known token env var used by the single-token
	// flow's already-configured detection and env-var-name default
	// (e.g. "GITHUB_TOKEN"). SingleToken only.
	FallbackEnv string
	// OffersSSH enables the SSH key discovery/generation/upload stage.
	// SingleToken only.
	OffersSSH bool
	// OffersLogin enables the forge [forgeapi.Authenticator] login attempt
	// before the manual-token fallback. SingleToken only.
	OffersLogin bool

	// TokenCreateURLTemplate is the provider's "create a personal access token"
	// URL, with the literal "{host}" standing in for Host (so an Enterprise host
	// is substituted). Empty degrades the manual-token wizard to a generic
	// "create a personal access token on <host>" message — forge-specific URL
	// paths and scope names must live here, not hard-coded in the wizard.
	// SingleToken only.
	TokenCreateURLTemplate string
	// TokenScopes is the human-readable scope list shown alongside the URL
	// (e.g. "repo, read:org, gist"). Empty omits the line. SingleToken only.
	TokenScopes string

	// UserFallbackEnv / PassFallbackEnv are the well-known env-var-name
	// defaults for the dual flow (e.g. "BITBUCKET_USERNAME" /
	// "BITBUCKET_APP_PASSWORD"). DualUserPass only.
	UserFallbackEnv string
	PassFallbackEnv string
}

// --- single-token config keys ---

func (p Profile) authEnvKey() string      { return p.ConfigPrefix + ".auth.env" }
func (p Profile) authValueKey() string    { return p.ConfigPrefix + ".auth.value" }
func (p Profile) authKeychainKey() string { return p.ConfigPrefix + ".auth.keychain" }
func (p Profile) sshKeyPathKey() string   { return p.ConfigPrefix + ".ssh.key.path" }
func (p Profile) sshKeyTypeKey() string   { return p.ConfigPrefix + ".ssh.key.type" }

// singleCredentialKeys is the full set of config keys that can carry the
// single token across the three storage modes. Every writer commits its mode's
// keys through [setup.WriteExclusive] over this set so switching modes never
// leaves a stale token or reference behind.
func (p Profile) singleCredentialKeys() []string {
	return []string{p.authEnvKey(), p.authValueKey(), p.authKeychainKey()}
}

// --- dual-credential config keys ---

func (p Profile) userEnvKey() string  { return p.ConfigPrefix + ".username.env" }
func (p Profile) passEnvKey() string  { return p.ConfigPrefix + ".app_password.env" }
func (p Profile) userKey() string     { return p.ConfigPrefix + ".username" }
func (p Profile) passKey() string     { return p.ConfigPrefix + ".app_password" }
func (p Profile) keychainKey() string { return p.ConfigPrefix + ".keychain" }

// dualCredentialKeys is the full set of config keys that can carry the dual
// credentials across the three storage modes — same exclusivity invariant as
// [Profile.singleCredentialKeys]. Order is load-bearing: it fixes the
// transactional change ordering [setup.ExclusiveChanges] emits.
func (p Profile) dualCredentialKeys() []string {
	return []string{p.userEnvKey(), p.passEnvKey(), p.userKey(), p.passKey(), p.keychainKey()}
}

// defaultForgeProvider builds the registered forge provider from the resolved
// config. Auth and SSH-key upload target the profile's Host (or the Enterprise
// host carried in the config's url.api); the release-source owner/repo are
// irrelevant to account-level capabilities, so only Host is set.
func defaultForgeProvider(profile Profile) func(config.Reader) (forgeapi.Provider, error) {
	return func(cfg config.Reader) (forgeapi.Provider, error) {
		factory, err := forgeapi.Lookup(profile.Provider)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		return factory(forgeapi.ReleaseSourceConfig{Host: profile.Host}, vcs.ConfigFromReader(cfg))
	}
}

// gitHubProfile drives the single-token GitHub wizard: forge-driven OAuth login
// with a manual-PAT fallback, optional SSH key generation and upload.
var gitHubProfile = Profile{ //nolint:gosec // G101: TokenCreateURLTemplate is a PAT-creation URL, not a credential
	Provider:               "github",
	ConfigPrefix:           "github",
	Label:                  "GitHub",
	DisplayName:            "GitHub integration",
	Feature:                GithubFeature,
	Host:                   "github.com",
	KeychainAccount:        "github.auth",
	Credential:             SingleToken,
	FallbackEnv:            "GITHUB_TOKEN",
	OffersSSH:              true,
	OffersLogin:            true,
	TokenCreateURLTemplate: "https://{host}/settings/tokens/new?scopes=repo,read:org,gist&description=gtb-cli",
	TokenScopes:            "repo, read:org, gist",
}

// bitbucketProfile drives the dual-credential Bitbucket wizard: username +
// app-password, no login, no SSH.
var bitbucketProfile = Profile{ //nolint:gosec // G101: PassFallbackEnv is the env-var NAME to read, not a credential value
	Provider:        "bitbucket",
	ConfigPrefix:    "bitbucket",
	Label:           "Bitbucket",
	DisplayName:     "Bitbucket authentication",
	Feature:         BitbucketFeature,
	KeychainAccount: "bitbucket.auth",
	Credential:      DualUserPass,
	UserFallbackEnv: "BITBUCKET_USERNAME",
	PassFallbackEnv: "BITBUCKET_APP_PASSWORD",
}

//go:embed assets/*
var assets embed.FS

var (
	skipLogin     bool
	skipKey       bool
	skipBitbucket bool
)

func init() {
	registerGitHub()
	registerBitbucket()
}

// registerGitHub wires the GitHub initialiser, its `init github` subcommand,
// the --skip-login / --skip-key flags, and its embedded asset bundle.
func registerGitHub() {
	setup.RegisterAssets(gitHubProfile.Feature, "github", &assets)
	setup.Register(gitHubProfile.Feature,
		[]setup.InitialiserProvider{
			func(p *props.Props) setup.Initialiser {
				if skipLogin && skipKey {
					return nil
				}

				return NewGitHubInitialiser(p, skipLogin, skipKey)
			},
		},
		[]setup.SubcommandProvider{
			func(p *props.Props) []*cobra.Command {
				return []*cobra.Command{NewCmdInitGitHub(p)}
			},
		},
		[]setup.FeatureFlag{
			func(cmd *cobra.Command) {
				isCI := (os.Getenv("CI") == "true")
				cmd.Flags().BoolVarP(&skipLogin, "skip-login", "l", isCI, "skip the login to github")
				cmd.Flags().BoolVarP(&skipKey, "skip-key", "k", isCI, "skip configuring ssh key")
			},
		},
	)
}

// registerBitbucket wires the Bitbucket initialiser, its `init bitbucket`
// subcommand, and the --skip-bitbucket flag.
func registerBitbucket() {
	setup.Register(bitbucketProfile.Feature,
		[]setup.InitialiserProvider{
			func(p *props.Props) setup.Initialiser {
				if skipBitbucket {
					return nil
				}

				return NewBitbucketInitialiser(p)
			},
		},
		[]setup.SubcommandProvider{
			func(p *props.Props) []*cobra.Command {
				return []*cobra.Command{NewCmdInitBitbucket(p)}
			},
		},
		[]setup.FeatureFlag{
			func(cmd *cobra.Command) {
				isCI := (os.Getenv("CI") == "true")
				cmd.Flags().BoolVar(&skipBitbucket, "skip-bitbucket", isCI, "skip configuring Bitbucket credentials")
			},
		},
	)
}
