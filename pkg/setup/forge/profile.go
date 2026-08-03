package forge

import (
	"context"
	"embed"
	"io/fs"
	"net/url"
	"os"
	"strings"

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
	// GitlabFeature gates the GitLab single-token setup wizard.
	GitlabFeature = props.FeatureID("gitlab")
	// GiteaFeature gates the Gitea single-token setup wizard.
	GiteaFeature = props.FeatureID("gitea")
	// BitbucketFeature gates the Bitbucket dual-credential setup wizard.
	BitbucketFeature = props.FeatureID("bitbucket")
)

// gitLabOAuthClientID is the client ID of the OAuth application GTB owns on
// gitlab.com, registered to the phpboyscout group rather than an individual so
// a shipped identifier is not tied to one person's account.
//
// A client ID is a public identifier for a public client — shipping it is the
// point of registering the application — so it is a constant, not a secret. The
// client secret GitLab also issued is unused and is recorded nowhere.
//
// It is applied only when the target really is gitlab.com; see
// [Profile.LoginClientID] for why that condition is load-bearing.
// Being 64 hex characters it trips gitleaks' generic-api-key entropy rule, so
// the literal is allowlisted by value in .gitleaks.toml. An inline
// //gitleaks:allow would not do: CI scans the commit range, and the commit that
// introduced this line would still carry the un-annotated version.
const gitLabOAuthClientID = "78d6c73fc80d2ad37bc3ec34a53aaed8dc517abbb824d9035d5735a977429376"

// gitLabAPIHost is the API host the shipped client ID is valid against. A
// client ID registered on gitlab.com means nothing to a self-hosted instance.
const gitLabAPIHost = "gitlab.com"

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
		ID:           GitlabFeature,
		ConstName:    "GitlabFeature",
		ConstPackage: PackagePath,
		Kind:         props.KindForge,
	})
	props.RegisterFeature(props.FeatureDescriptor{
		ID:           GiteaFeature,
		ConstName:    "GiteaFeature",
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
	//
	// It is a capability claim — "this forge can accept an SSH key" — not a
	// statement about the credential shape. The stage takes a Profile and
	// nothing else, and runs from Configure for either shape.
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

	// LoginClientID is an OAuth application client ID this framework ships for
	// the forge's device-flow login, applied only when the resolved API host
	// matches LoginClientIDHost and the user's config names no client ID of
	// its own.
	//
	// It is deliberately NOT shipped in the forge's embedded config bundle,
	// which would be simpler. An embedded default always populates the key, so
	// the adapter's Settings.ClientID would never be empty — which would (a)
	// dead-letter the provider's own well-known env-var fallback, and (b) send
	// a gitlab.com client ID to a self-hosted instance, where login fails as an
	// invalid client instead of degrading to the manual-token path. Gating on
	// the host keeps both behaviours intact. See spec 0185 D8.
	LoginClientID string
	// LoginClientIDHost is the API host LoginClientID is registered against.
	// Empty means "never apply it".
	LoginClientIDHost string

	// RepoDescription and RepoPlaceholder are the repository-field help text
	// and placeholder the project generator shows for this forge. They live
	// here, beside the forge's other definitional data, so adding a forge does
	// not also mean editing a table in internal/cmd/generate — see spec 0185
	// D5. They reach the generator through [DisplayFor], not through Profile
	// itself, which stays unexported.
	RepoDescription string
	RepoPlaceholder string
	// NestedNamespaces marks a forge whose owner segment may itself contain
	// slashes (GitLab subgroups). It selects the namespace-validation rules
	// and is the one structural difference between forges' repository paths.
	NestedNamespaces bool
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
func defaultForgeProvider(profile Profile) func(context.Context, config.Reader) (forgeapi.Provider, error) {
	return func(ctx context.Context, cfg config.Reader) (forgeapi.Provider, error) {
		factory, err := forgeapi.Lookup(profile.Provider)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		return factory(
			ctx,
			forgeapi.ReleaseSourceConfig{Host: profile.Host},
			withShippedClientID(vcs.ConfigFromReader(cfg), profile),
		)
	}
}

// withShippedClientID layers the profile's shipped OAuth client ID over the
// resolved config, so the adapter sees it exactly where it would see an
// operator-supplied one. A profile that ships none is returned untouched.
func withShippedClientID(cfg forgeapi.Config, profile Profile) forgeapi.Config {
	if cfg == nil || profile.LoginClientID == "" || profile.LoginClientIDHost == "" {
		return cfg
	}

	return clientIDDefaulter{Config: cfg, profile: profile}
}

// clientIDDefaulter intercepts the forge's own config section on the way past.
// Everything else delegates untouched.
type clientIDDefaulter struct {
	forgeapi.Config

	profile Profile
}

func (d clientIDDefaulter) Sub(key string) forgeapi.Config {
	sub := d.Config.Sub(key)
	if sub == nil || key != d.profile.ConfigPrefix {
		return sub
	}

	return clientIDSection{Config: sub, profile: d.profile}
}

// clientIDSection supplies the shipped client ID for auth.client_id when the
// user named none and the section points at the host the ID is registered
// against.
type clientIDSection struct {
	forgeapi.Config

	profile Profile
}

func (s clientIDSection) GetString(key string) string {
	value := s.Config.GetString(key)
	if key != "auth.client_id" || value != "" {
		return value
	}

	if !s.targetsRegisteredHost() {
		return ""
	}

	return s.profile.LoginClientID
}

// targetsRegisteredHost reports whether this section resolves to the host the
// shipped client ID was registered against. An empty url.api means the adapter
// will fall back to its own public default, which is that host.
func (s clientIDSection) targetsRegisteredHost() bool {
	apiURL := strings.TrimSpace(s.Config.GetString("url.api"))
	if apiURL == "" {
		return true
	}

	parsed, err := url.Parse(apiURL)
	if err != nil {
		return false
	}

	return strings.EqualFold(parsed.Hostname(), s.profile.LoginClientIDHost)
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
	RepoDescription:        "The repository path in org/repo format.",
	RepoPlaceholder:        "org/repo",
}

// gitLabProfile drives the single-token GitLab wizard. Like GitHub it offers
// device-flow login and SSH upload — forge-gitlab implements both capabilities
// — backed by the OAuth application GTB owns on gitlab.com.
//
// Scopes are `api` plus `write_repository`, and `api` was forced rather than
// chosen: SSH-key upload is POST /user/keys, a user-level API write, and GitLab
// has read_user but no write_user, while write_repository is Git-over-HTTP
// rather than API access. Nothing below `api` can register a key.
// write_repository is requested as well because a device-flow token is neither
// a personal access token nor a project token, and only the former documents
// `api` as covering Git-over-HTTP push — which `--push` needs. See spec 0185 D9.
var gitLabProfile = Profile{ //nolint:gosec // G101: TokenCreateURLTemplate is a PAT-creation URL, not a credential
	Provider:               "gitlab",
	ConfigPrefix:           "gitlab",
	Label:                  "GitLab",
	DisplayName:            "GitLab integration",
	Feature:                GitlabFeature,
	Host:                   "gitlab.com",
	KeychainAccount:        "gitlab.auth",
	Credential:             SingleToken,
	FallbackEnv:            "GITLAB_TOKEN",
	OffersSSH:              true,
	OffersLogin:            true,
	TokenCreateURLTemplate: "https://{host}/-/user_settings/personal_access_tokens?scopes=api,write_repository",
	TokenScopes:            "api, write_repository",
	RepoDescription:        "The repository path. GitLab supports nested groups — use the full path and the last segment will be treated as the repository name (e.g. group/subgroup/repo).",
	RepoPlaceholder:        "group/subgroup/repo",
	NestedNamespaces:       true,
	LoginClientID:          gitLabOAuthClientID,
	LoginClientIDHost:      gitLabAPIHost,
}

// giteaProfile drives the single-token Gitea wizard.
//
// It offers no login: forge-gitea deliberately does not implement
// forge.Authenticator — its own docs state Gitea is personal-access-token only
// — so the wizard goes straight to manual token entry. captureToken already
// degrades that way on ErrNotSupported, so OffersLogin: false is configuration
// rather than a separate code path.
//
// It carries no Host. There is no "gitea.com" the way there is a github.com;
// every instance is somebody's. That makes it the first hostless single-token
// profile, which is why manualTokenInstructions has to divert rather than
// interpolate an empty string. Gitea's token page also takes no scope query
// parameter, so TokenScopes is guidance the user applies by hand rather than a
// link that pre-fills the form. See spec 0185 D1 and D3.
var giteaProfile = Profile{ //nolint:gosec // G101: TokenCreateURLTemplate is a PAT-creation URL, not a credential
	Provider:               "gitea",
	ConfigPrefix:           "gitea",
	Label:                  "Gitea",
	DisplayName:            "Gitea integration",
	Feature:                GiteaFeature,
	KeychainAccount:        "gitea.auth",
	Credential:             SingleToken,
	FallbackEnv:            "GITEA_TOKEN",
	OffersSSH:              true,
	OffersLogin:            false,
	TokenCreateURLTemplate: "https://{host}/user/settings/applications",
	TokenScopes:            "write:repository, read:user",
	RepoDescription:        "The repository path in owner/repo format.",
	RepoPlaceholder:        "owner/repo",
}

// bitbucketProfile drives the dual-credential Bitbucket wizard: username +
// app-password, no login, no SSH.
var bitbucketProfile = Profile{ //nolint:gosec // G101: PassFallbackEnv is the env-var NAME to read, not a credential value
	Provider:     "bitbucket",
	ConfigPrefix: "bitbucket",
	Label:        "Bitbucket",
	DisplayName:  "Bitbucket authentication",
	Feature:      BitbucketFeature,
	// Host is required by defaultKeyManager, which builds the provider with
	// forgeapi.ReleaseSourceConfig{Host: profile.Host}. Its absence here was
	// latent rather than harmless.
	Host:            "bitbucket.org",
	KeychainAccount: "bitbucket.auth",
	Credential:      DualUserPass,
	// forge-bitbucket implements forge.KeyManager: UploadKey posts an
	// OpenSSH-format public key, authorised by the configured username and app
	// password. GTB could not reach it while the stage was single-token only.
	OffersSSH:       true,
	UserFallbackEnv: "BITBUCKET_USERNAME",
	PassFallbackEnv: "BITBUCKET_APP_PASSWORD",
	RepoDescription: "The repository path in workspace/repo format.",
	RepoPlaceholder: "workspace/repo",
}

// bundles holds one embedded config bundle per forge, each rooted at
// bundles/<forge>/assets/… so [bundleFor] can hand [setup.RegisterAssets] an
// fs.FS in which the framework's fixed asset paths — assets/config.yaml and
// assets/init/config.yaml — resolve exactly as they do for a single-bundle
// package.
//
// Every forge ships one, not just GitHub. vcs.ConfigFromReader(cfg).Sub(name)
// returns nil when the section is absent, and GTB was insulated from that only
// because the GitHub bundle guaranteed a `github:` key existed. A forge without
// a bundle would be the first real exposure of that hazard — see spec 0185 D4.
//
//go:embed bundles
var bundles embed.FS

// bundleFor returns the named forge's bundle re-rooted so that its
// assets/config.yaml sits at the path the framework looks for.
//
// The sub-FS cannot fail for a name that has a directory under bundles/, and
// every caller is a compile-time constant paired with a checked-in directory —
// so a failure here is a build-time mistake, not a runtime condition, and the
// registration guard test asserts every forge resolves.
func bundleFor(name string) fs.FS {
	sub, err := fs.Sub(bundles, "bundles/"+name)
	if err != nil {
		panic("forge: missing embedded config bundle for " + name + ": " + err.Error())
	}

	return sub
}

var (
	skipLogin     bool
	skipKey       bool
	skipBitbucket bool
	skipGitlab    bool
	skipGitea     bool
)

func init() {
	registerGitHub()
	registerSingleTokenForge(gitLabProfile, &skipGitlab, "skip-gitlab", "skip configuring GitLab credentials")
	registerSingleTokenForge(giteaProfile, &skipGitea, "skip-gitea", "skip configuring Gitea credentials")
	registerBitbucket()
}

// registerSingleTokenForge wires a single-token forge's initialiser, its
// `init <forge>` subcommand, its skip flag and its embedded config bundle.
//
// GitHub keeps its own registration function rather than routing through here:
// it carries two flags of its own (--skip-login / --skip-key) whose names and
// shorthands are established CLI surface, and spec 0185 D7 holds its behaviour
// fixed.
func registerSingleTokenForge(profile Profile, skip *bool, flagName, flagUsage string) {
	setup.RegisterAssets(profile.Feature, profile.ConfigPrefix, bundleFor(profile.ConfigPrefix))
	setup.Register(profile.Feature,
		[]setup.InitialiserProvider{
			func(p *props.Props) setup.Initialiser {
				if *skip {
					return nil
				}

				init := New(p, profile)
				// --skip-key is a global init flag, so it must reach every
				// profile that offers SSH — not just GitHub, which was the
				// only one wired to it while the stage was single-token only.
				init.SkipKey = skipKey

				return init
			},
		},
		[]setup.SubcommandProvider{
			func(p *props.Props) []*cobra.Command {
				return []*cobra.Command{NewCmdInitForge(p, profile)}
			},
		},
		[]setup.FeatureFlag{
			func(cmd *cobra.Command) {
				cmd.Flags().BoolVar(skip, flagName, os.Getenv("CI") == "true", flagUsage)
			},
		},
	)
}

// registerGitHub wires the GitHub initialiser, its `init github` subcommand,
// the --skip-login / --skip-key flags, and its embedded asset bundle.
func registerGitHub() {
	setup.RegisterAssets(gitHubProfile.Feature, "github", bundleFor("github"))
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
	setup.RegisterAssets(bitbucketProfile.Feature, "bitbucket", bundleFor("bitbucket"))
	setup.Register(bitbucketProfile.Feature,
		[]setup.InitialiserProvider{
			func(p *props.Props) setup.Initialiser {
				if skipBitbucket {
					return nil
				}

				i := NewBitbucketInitialiser(p)
				// Bitbucket reaches the SSH stage now (0186 D1), so it honours
				// --skip-key like every other profile that offers SSH.
				i.SkipKey = skipKey

				return i
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
