package forge

import (
	"context"
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

	// SkipLogin suppresses the login stage of a single-token profile; it is
	// ignored by dual-credential profiles, which have no login step.
	//
	// SkipKey suppresses the SSH stage for any profile that offers it,
	// whatever its credential shape.
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
	// single-token and dual-credential flows respectively. sshOpts does the
	// same for the SSH stage, which both shapes now reach.
	authOpts []AuthFormOption
	dualOpts []DualFormOption
	sshOpts  []ConfigureSSHKeyOption
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

// WithSSHForms propagates [ConfigureSSHKeyOption]s into the SSH stage. The
// stage's form creators and key-manager factory were already injectable, but
// unreachable from Configure — so the stage could only be driven directly, not
// as part of the wizard it actually runs in.
func WithSSHForms(opts ...ConfigureSSHKeyOption) InitialiserOption {
	return func(i *Initialiser) { i.sshOpts = append(i.sshOpts, opts...) }
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

// IsConfigured reports whether the profile's credential — and, for any profile
// that offers it, SSH — is already present in the config.
func (i *Initialiser) IsConfigured(cfg config.Reader) bool {
	if i.profile.Credential == DualUserPass {
		return i.isDualConfigured(cfg)
	}

	return i.isSingleConfigured(cfg)
}

// Configure runs the interactive wizard for the profile's credential shape and
// then, for a profile that offers SSH, the key stage.
//
// The SSH stage runs here rather than inside a per-shape branch: whether a
// forge is offered a key is a property of the profile, not a consequence of how
// many fields its credential has. It runs after credential capture because an
// upload is authorised by the credential just captured — Bitbucket's UploadKey
// needs the username and app password, so the order is a requirement rather
// than an accident of where the call sat.
//
// ctx is the caller's context — it deliberately carries no stage-wide deadline
// (see setup.Initialiser); keychain operations derive their own
// KeychainOpTimeout bounds at each call site.
func (i *Initialiser) Configure(ctx context.Context, p *props.Props, cfg setup.Editor) error {
	if err := i.configureCredential(ctx, p, cfg); err != nil {
		return err
	}

	// configureSSH stays ctx-free for now: its upload path bounds itself and is
	// outside the credential-stage scoping fix (see the forge-repo-setup
	// follow-ups spec).
	return i.maybeConfigureSSH(p, cfg) //nolint:contextcheck // SSH stage deliberately ctx-free; upload bounds itself
}

// configureCredential dispatches to the wizard for the profile's credential
// shape. It is the only place that branches on Credential.
func (i *Initialiser) configureCredential(ctx context.Context, p *props.Props, cfg setup.Editor) error {
	if i.profile.Credential == DualUserPass {
		return i.configureDual(ctx, p, cfg)
	}

	return i.configureSingle(ctx, p, cfg)
}

// maybeConfigureSSH runs the key stage when the profile offers SSH, the run has
// not been told to skip keys, and no key is recorded yet.
//
// The config is re-read here rather than reusing an earlier view: the credential
// stage may have written keys this must observe.
func (i *Initialiser) maybeConfigureSSH(p *props.Props, cfg setup.Editor) error {
	if !i.profile.OffersSSH || i.SkipKey {
		return nil
	}

	view := cfg.View()
	if view.GetString(i.profile.sshKeyPathKey()) != "" ||
		view.GetString(i.profile.sshKeyTypeKey()) == "agent" {
		return nil
	}

	return i.configureSSH(p, cfg)
}

func (i *Initialiser) isSingleConfigured(cfg config.Reader) bool {
	p := i.profile

	authEnv := cfg.GetString(p.authEnvKey())
	loginConfigured := i.SkipLogin ||
		cfg.GetString(p.authValueKey()) != "" ||
		cfg.GetString(p.authKeychainKey()) != "" ||
		(authEnv != "" && os.Getenv(authEnv) != "")

	return loginConfigured && i.sshConfigured(cfg)
}

func (i *Initialiser) isDualConfigured(cfg config.Reader) bool {
	p := i.profile

	credentialConfigured := cfg.GetString(p.keychainKey()) != "" ||
		cfg.GetString(p.userEnvKey()) != "" ||
		cfg.GetString(p.passEnvKey()) != "" ||
		cfg.GetString(p.userKey()) != "" ||
		cfg.GetString(p.passKey()) != ""

	return credentialConfigured && i.sshConfigured(cfg)
}

// sshConfigured reports whether the SSH stage has nothing left to do — either
// because it was skipped, the profile does not offer SSH, or a key is already
// recorded. Shared by both credential shapes so re-running init does not
// re-prompt a dual-credential forge for a key it already has.
func (i *Initialiser) sshConfigured(cfg config.Reader) bool {
	p := i.profile

	return i.SkipKey || !p.OffersSSH ||
		cfg.GetString(p.sshKeyPathKey()) != "" ||
		cfg.GetString(p.sshKeyTypeKey()) == "agent"
}
