package forge

import (
	"os"

	"gitlab.com/phpboyscout/go/config"

	forgeapi "gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Initialiser is the single, profile-parameterised [setup.Initialiser] shared
// by every forge setup wizard. The [Profile] it carries selects the
// single-token or dual-credential flow and supplies every provider-specific
// value; the injectable seams below let tests drive the flow without a real
// forge, keychain, or TTY.
type Initialiser struct {
	profile Profile

	// SkipLogin / SkipKey suppress the login and SSH stages of a single-token
	// profile. Ignored by dual-credential profiles.
	SkipLogin bool
	SkipKey   bool

	// providerFactory builds the registered forge provider from the resolved
	// config; the flow type-asserts it for the optional [forgeapi.Authenticator]
	// capability. Overridable for tests.
	providerFactory func(config.Reader) (forgeapi.Provider, error)
	// prompter surfaces the interactive device-code step of an Authenticator's
	// Login flow. Presentation lives here in the CLI, never in the forge module.
	prompter forgeapi.Prompter

	// authOpts / dualOpts inject deterministic form creators for the
	// single-token and dual-credential flows respectively.
	authOpts []AuthFormOption
	dualOpts []DualFormOption
}

// InitialiserOption configures an [Initialiser].
type InitialiserOption func(*Initialiser)

// WithProviderFactory overrides the forge provider constructor used for
// interactive login. Tests pass a factory returning a fake provider (optionally
// implementing [forgeapi.Authenticator]); production callers omit it to get the
// registered provider.
func WithProviderFactory(fn func(config.Reader) (forgeapi.Provider, error)) InitialiserOption {
	return func(i *Initialiser) { i.providerFactory = fn }
}

// WithPrompter overrides the [forgeapi.Prompter] that renders the device-code
// step. Tests pass a no-op prompter; production callers omit it to get the
// default CLI prompter.
func WithPrompter(p forgeapi.Prompter) InitialiserOption {
	return func(i *Initialiser) { i.prompter = p }
}

// WithAuthForms propagates [AuthFormOption]s into the single-token wizard.
// Tests use this to inject deterministic form creators via [WithAuthForm].
func WithAuthForms(opts ...AuthFormOption) InitialiserOption {
	return func(i *Initialiser) { i.authOpts = append(i.authOpts, opts...) }
}

// WithDualForms propagates [DualFormOption]s into the dual-credential wizard.
// Tests use this to inject deterministic form creators via [WithDualForm].
func WithDualForms(opts ...DualFormOption) InitialiserOption {
	return func(i *Initialiser) { i.dualOpts = append(i.dualOpts, opts...) }
}

// New constructs a profile-driven [Initialiser] with production defaults
// (the registered forge provider and the CLI prompter) and applies opts.
func New(_ *props.Props, profile Profile, opts ...InitialiserOption) *Initialiser {
	i := &Initialiser{
		profile:         profile,
		providerFactory: defaultForgeProvider(profile),
		prompter:        newCLIPrompter(),
	}

	for _, o := range opts {
		o(i)
	}

	return i
}

// NewGitHubInitialiser builds the single-token GitHub initialiser. Its asset
// bundle is registered from init() via [setup.RegisterAssets], applied for
// enabled features at root construction.
func NewGitHubInitialiser(p *props.Props, skipLogin, skipKey bool, opts ...InitialiserOption) *Initialiser {
	base := make([]InitialiserOption, 0, 1+len(opts))
	base = append(base, func(i *Initialiser) {
		i.SkipLogin = skipLogin
		i.SkipKey = skipKey
	})

	return New(p, gitHubProfile, append(base, opts...)...)
}

// NewBitbucketInitialiser builds the dual-credential Bitbucket initialiser.
func NewBitbucketInitialiser(p *props.Props, opts ...InitialiserOption) *Initialiser {
	return New(p, bitbucketProfile, opts...)
}

// Name returns the human-readable label for this initialiser.
func (i *Initialiser) Name() string { return i.profile.DisplayName }

// IsConfigured reports whether the profile's credential (and, for single-token
// profiles that offer it, SSH) is already present in the config.
func (i *Initialiser) IsConfigured(cfg config.Reader) bool {
	if i.profile.Credential == DualUserPass {
		return i.isDualConfigured(cfg)
	}

	return i.isSingleConfigured(cfg)
}

// Configure runs the interactive wizard for the profile's credential shape.
func (i *Initialiser) Configure(p *props.Props, cfg setup.Editor) error {
	if i.profile.Credential == DualUserPass {
		return i.configureDual(p, cfg)
	}

	return i.configureSingle(p, cfg)
}

func (i *Initialiser) isSingleConfigured(cfg config.Reader) bool {
	p := i.profile

	authEnv := cfg.GetString(p.authEnvKey())
	loginConfigured := i.SkipLogin ||
		cfg.GetString(p.authValueKey()) != "" ||
		cfg.GetString(p.authKeychainKey()) != "" ||
		(authEnv != "" && os.Getenv(authEnv) != "")

	sshConfigured := i.SkipKey || !p.OffersSSH ||
		cfg.GetString(p.sshKeyPathKey()) != "" ||
		cfg.GetString(p.sshKeyTypeKey()) == "agent"

	return loginConfigured && sshConfigured
}

func (i *Initialiser) isDualConfigured(cfg config.Reader) bool {
	p := i.profile

	return cfg.GetString(p.keychainKey()) != "" ||
		cfg.GetString(p.userEnvKey()) != "" ||
		cfg.GetString(p.passEnvKey()) != "" ||
		cfg.GetString(p.userKey()) != "" ||
		cfg.GetString(p.passKey()) != ""
}
