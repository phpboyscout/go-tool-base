package forge

import (
	"context"
	"fmt"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"

	"gitlab.com/phpboyscout/go/credentials"

	"gitlab.com/phpboyscout/go/config"

	forgeapi "gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
)

// AuthConfig captures the single-token wizard's output from each stage. All
// fields are populated incrementally so test-injected form creators can
// override any subset and the runner still produces a coherent config.
type AuthConfig struct {
	// StorageMode is set by the storage-mode selector. Defaults to
	// [credentials.ModeEnvVar] when the form presents the choice.
	StorageMode credentials.Mode

	// EnvVarName is the env var NAME recorded under <prefix>.auth.env in
	// env-var mode. Ignored in keychain/literal modes.
	EnvVarName string

	// FetchToken is true when the user wants the wizard to run OAuth (or the
	// manual fallback) on their behalf. Only relevant in env-var mode —
	// keychain/literal always need a token.
	FetchToken bool

	// Token is the captured token from OAuth / manual entry. Cleared after it
	// has been written (or displayed for env-var mode) so it does not linger in
	// the AuthConfig longer than necessary.
	Token string
}

// AuthFormOption configures form creators for the single-token auth wizard.
// Used by tests to inject deterministic form-answering creators without driving
// a real TTY.
type AuthFormOption func(*authFormConfig)

type authFormConfig struct {
	storageModeFormCreator func(*AuthConfig) *huh.Form
	envVarNameFormCreator  func(*AuthConfig) *huh.Form
	fetchTokenFormCreator  func(*AuthConfig) *huh.Form
	displayOnceFormCreator func(envVarName, token string) *huh.Form
}

// Form-slot indices used by [WithAuthForm] for the slice-returning creator. The
// display-once form takes a different signature and is not indexed here.
const (
	authFormSlotStorageMode = 0
	authFormSlotEnvVarName  = 1
	authFormSlotFetchToken  = 2
)

// WithAuthForm injects custom form creators into the single-token flow for
// testability. The creator returns forms in order:
//
//	[0] storage-mode selector
//	[1] env-var name input
//	[2] "fetch token now?" confirm
//	[3] display-once token view (takes envVarName, token)
//
// Returning fewer forms is allowed — the runner skips stages whose slot is nil
// or absent. The display-once creator has a different signature because it
// needs the captured token passed in.
func WithAuthForm(
	creator func(*AuthConfig) []*huh.Form,
	displayOnceCreator func(envVarName, token string) *huh.Form,
) AuthFormOption {
	return func(c *authFormConfig) {
		c.storageModeFormCreator = authFormAtIndex(creator, authFormSlotStorageMode)
		c.envVarNameFormCreator = authFormAtIndex(creator, authFormSlotEnvVarName)
		c.fetchTokenFormCreator = authFormAtIndex(creator, authFormSlotFetchToken)
		c.displayOnceFormCreator = displayOnceCreator
	}
}

func authFormAtIndex(creator func(*AuthConfig) []*huh.Form, i int) func(*AuthConfig) *huh.Form {
	return func(cfg *AuthConfig) *huh.Form {
		forms := creator(cfg)
		if i < len(forms) {
			return forms[i]
		}

		return nil
	}
}

// configureSingle runs the interactive login for a single-token profile.
//
// The SSH stage is not called here: it is shared with the dual-credential shape
// and runs from [Initialiser.Configure] once the credential is captured. Keeping
// one call site is what stops the two shapes drifting apart again.
func (i *Initialiser) configureSingle(ctx context.Context, p *props.Props, cfg setup.Editor) error {
	if !i.SkipLogin && !hasAnySingleCredential(i.profile, cfg.View()) {
		if err := i.configureAuth(ctx, p, cfg); err != nil {
			return err
		}
	}

	return nil
}

// hasAnySingleCredential reports whether the config already records a credential
// under any of the three storage modes. Used to short-circuit the auth wizard
// when the user re-runs `init`.
func hasAnySingleCredential(profile Profile, cfg config.Reader) bool {
	return cfg.GetString(profile.authValueKey()) != "" ||
		cfg.GetString(profile.authKeychainKey()) != "" ||
		cfg.GetString(profile.authEnvKey()) != ""
}

func (i *Initialiser) configureAuth(ctx context.Context, p *props.Props, cfg setup.Editor) error {
	profile := i.profile

	// CI defence: literal-mode writes in a CI environment almost certainly leak
	// the token to build artefacts or logs. The --skip-login default already
	// suppresses this path under CI, but belt-and-braces the invariant here.
	if credentials.IsCI() {
		return errors.WithHint(
			errors.Newf("%s literal-token storage is refused under CI", profile.Label),
			fmt.Sprintf("Set %s via your CI platform's secret injection and add `%s: %s` to the tool's config.",
				profile.FallbackEnv, profile.authEnvKey(), profile.FallbackEnv))
	}

	// If the user already has any credential configured — env-var reference,
	// literal config value (the store's env layer surfaces prefixed env), or
	// the unprefixed ecosystem fallback — don't overwrite with a fresh token.
	// The KeychainOpTimeout deadline is scoped to this single resolve only:
	// reusing it for the stage that follows was the 2026-07-23 review's
	// CRITICAL finding (a 5s clock spanning the OAuth device flow killed every
	// interactive login; spec 2026-07-23-setup-credential-stage-context-scoping).
	resolveCtx, cancel := context.WithTimeout(ctx, credentials.KeychainOpTimeout)
	defer cancel()

	view := cfg.View()

	credential := vcs.ForgeCredential(
		vcs.ConfigFromReader(view).Sub(profile.Provider), profile.FallbackEnv)

	// A resolution error here is not fatal: this only asks "is one already
	// configured?", and the answer on failure is "assume not" — the wizard then
	// captures one, which is the recovery anyway.
	if token, _ := credential(resolveCtx); token != "" {
		p.Logger.Info("credential already configured; skipping OAuth token capture",
			"provider", profile.Label, "env_ref", view.GetString(profile.authEnvKey()))

		return nil
	}

	authCfg := &AuthConfig{}
	fCfg := newAuthFormConfig(profile, i.authOpts...) //nolint:contextcheck // form creators are ctx-free seams; the storage-mode form derives its own per-op KeychainOpTimeout ctx for the availability Probe by design

	if err := runAuthFormStage(fCfg.storageModeFormCreator, authCfg); err != nil {
		return err
	}

	// CI belt-and-braces: refuse literal even if a test-injected form creator
	// bypassed the selector.
	if err := credentials.RefuseLiteralUnderCI(authCfg.StorageMode); err != nil {
		return err
	}

	return i.runAuthCredentialStage(ctx, p, cfg, fCfg, authCfg)
}

// runAuthCredentialStage dispatches to the per-mode branch and writes the
// resulting config. Split from [Initialiser.configureAuth] so the outer
// function stays under the cyclomatic-complexity budget.
func (i *Initialiser) runAuthCredentialStage(
	ctx context.Context,
	p *props.Props,
	cfg setup.Editor,
	fCfg *authFormConfig,
	authCfg *AuthConfig,
) error {
	var err error

	switch authCfg.StorageMode {
	case credentials.ModeEnvVar:
		err = i.runEnvVarAuth(ctx, p, cfg.View(), fCfg, authCfg)
	case credentials.ModeKeychain, credentials.ModeLiteral, "":
		authCfg.Token, err = i.captureToken(ctx, p, cfg.View())
	default:
		err = errors.Newf("unsupported credential storage mode %q", authCfg.StorageMode)
	}

	if err != nil {
		return err
	}

	if writeErr := writeSingleCredential(ctx, i.profile, cfg, p.Tool.Name, authCfg); writeErr != nil {
		return writeErr
	}

	// Best-effort zero. Go strings are immutable so this only clears the last
	// reference in the AuthConfig — the GC reclaims the backing memory when it's
	// ready. See docs/development/security-decisions.md § M-4.
	authCfg.Token = ""

	return nil
}

// runEnvVarAuth drives the env-var branch: prompts for the env-var name, then
// (optionally) runs OAuth and displays the token once for the user to copy into
// their shell profile.
func (i *Initialiser) runEnvVarAuth(
	ctx context.Context,
	p *props.Props,
	cfg config.Reader,
	fCfg *authFormConfig,
	authCfg *AuthConfig,
) error {
	if err := runAuthFormStage(fCfg.envVarNameFormCreator, authCfg); err != nil {
		return err
	}

	if err := runAuthFormStage(fCfg.fetchTokenFormCreator, authCfg); err != nil {
		return err
	}

	if !authCfg.FetchToken {
		// User already has a token via other means (shell profile, 1Password
		// CLI, etc.). We write only the env-var reference.
		return nil
	}

	token, err := i.captureToken(ctx, p, cfg)
	if err != nil {
		return err
	}

	displayForm := fCfg.displayOnceFormCreator(authCfg.EnvVarName, token)
	if displayForm != nil {
		if err := displayForm.Run(); err != nil {
			return errors.Wrap(err, "token display cancelled")
		}
	}

	// Token is not written to config in env-var mode. The user has seen it and
	// acknowledged saving it. We clear our local copy best-effort (string
	// immutability; see M-4).
	token = ""
	_ = token

	return nil
}

// captureToken drives the forge provider's [forgeapi.Authenticator] login (an
// OAuth device flow), falling back to manual token entry when the provider does
// not support interactive login ([forgeapi.ErrNotSupported]), cannot be built,
// or the flow fails (e.g. a headless server with no browser).
func (i *Initialiser) captureToken(ctx context.Context, p props.LoggerProvider, cfg config.Reader) (string, error) {
	log := p.GetLogger()

	auth, err := i.authenticator(ctx, cfg)
	if err != nil {
		log.Warn("interactive login unavailable, falling back to manual token entry",
			"provider", i.profile.Label, "reason", err)

		return promptManualToken(i.profile)
	}

	log.Info("Logging in", "provider", i.profile.Label, "host", i.profile.Host)

	token, err := auth.Login(ctx, i.prompter)
	if err == nil {
		return token, nil
	}

	log.Warn("OAuth flow unavailable, falling back to manual token entry",
		"provider", i.profile.Label, "reason", err)

	return promptManualToken(i.profile)
}

// authenticator builds the forge provider and type-asserts it for the optional
// [forgeapi.Authenticator] capability. A provider that does not implement it (or
// is not configured for interactive login) yields [forgeapi.ErrNotSupported],
// which the caller treats as "fall back to manual entry".
func (i *Initialiser) authenticator(ctx context.Context, cfg config.Reader) (forgeapi.Authenticator, error) {
	provider, err := i.providerFactory(ctx, cfg)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// forgeapi.As walks any Provider decorator chain, so a forwarding wrapper
	// cannot silently strip the Authenticator capability.
	var auth forgeapi.Authenticator
	if !forgeapi.As(provider, &auth) {
		return nil, errors.Wrapf(forgeapi.ErrNotSupported,
			"%s provider does not support interactive login", i.profile.Provider)
	}

	return auth, nil
}

// writeSingleCredential routes to the correct config-write path for the
// captured storage mode. Token in authCfg is the captured OAuth or manual value
// (populated for keychain / literal modes; cleared for env-var mode after the
// display-once flow).
func writeSingleCredential(ctx context.Context, profile Profile, cfg setup.Editor, toolName string, authCfg *AuthConfig) error {
	switch authCfg.StorageMode {
	case credentials.ModeEnvVar:
		return writeSingleEnvRef(profile, cfg, authCfg)
	case credentials.ModeKeychain:
		return writeSingleKeychainRef(ctx, profile, cfg, toolName, authCfg)
	case credentials.ModeLiteral, "":
		return writeSingleLiteral(profile, cfg, authCfg)
	default:
		return errors.Newf("unsupported credential storage mode %q", authCfg.StorageMode)
	}
}

func writeSingleEnvRef(profile Profile, cfg setup.Editor, authCfg *AuthConfig) error {
	if authCfg.EnvVarName == "" {
		return nil
	}

	return setup.WriteExclusive(cfg,
		map[string]any{profile.authEnvKey(): authCfg.EnvVarName}, profile.singleCredentialKeys())
}

func writeSingleKeychainRef(ctx context.Context, profile Profile, cfg setup.Editor, toolName string, authCfg *AuthConfig) error {
	if authCfg.Token == "" {
		return errors.Newf("no %s token captured for keychain mode", profile.Label)
	}

	if toolName == "" {
		return errors.New("cannot write keychain entry without a tool name")
	}

	// Fresh per-operation deadline at the store call site — the documented
	// KeychainOpTimeout contract. The caller's ctx has been through the
	// human-paced form/OAuth stages, so any deadline derived earlier would
	// already be spent.
	storeCtx, cancel := context.WithTimeout(ctx, credentials.KeychainOpTimeout)
	defer cancel()

	if err := credentials.Store(storeCtx, toolName, profile.KeychainAccount, authCfg.Token); err != nil {
		return errors.WithHint(
			errors.Wrapf(err, "storing %s token in OS keychain", profile.Label),
			"If the keychain is locked, unlock it and re-run; otherwise pick env-var or literal mode instead.")
	}

	return setup.WriteExclusive(cfg,
		map[string]any{profile.authKeychainKey(): toolName + "/" + profile.KeychainAccount},
		profile.singleCredentialKeys())
}

func writeSingleLiteral(profile Profile, cfg setup.Editor, authCfg *AuthConfig) error {
	if authCfg.Token == "" {
		return nil
	}

	return setup.WriteExclusive(cfg,
		map[string]any{profile.authValueKey(): authCfg.Token}, profile.singleCredentialKeys())
}

// singleStorageModeForm presents the three-mode selector. Literal mode is
// hidden when the process runs under CI=true; keychain is hidden unless
// [credentials.Probe] succeeds against the registered backend.
func singleStorageModeForm(profile Profile, cfg *AuthConfig) *huh.Form {
	ctx, cancel := context.WithTimeout(context.Background(), credentials.KeychainOpTimeout)
	defer cancel()

	choices := credentials.ModeChoices(credentials.IsCI(), credentials.Probe(ctx),
		"Environment variable reference (recommended)",
		"OS keychain",
		"Literal value in config file (plaintext)")

	options := make([]huh.Option[credentials.Mode], len(choices))
	for i, c := range choices {
		options[i] = huh.NewOption(c.Label, c.Mode)
	}

	if cfg.StorageMode == "" {
		cfg.StorageMode = credentials.ModeEnvVar
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[credentials.Mode]().
				Title(profile.Label + " Credential Storage").
				Description(singleStorageModeDescription(credentials.IsCI())).
				Options(options...).
				Value(&cfg.StorageMode),
		),
	)
}

func singleStorageModeDescription(ci bool) string {
	if ci {
		return "CI environment detected — only environment variable references are permitted."
	}

	return "Env-var reference is recommended; keychain keeps the token out of config; literal writes the token to config as plaintext."
}

// singleEnvVarNameForm prompts for the env var name, defaulting to the
// profile's well-known token env var (the ecosystem standard).
func singleEnvVarNameForm(profile Profile, cfg *AuthConfig) *huh.Form {
	if cfg.EnvVarName == "" {
		cfg.EnvVarName = profile.FallbackEnv
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Environment Variable Name").
				Description(fmt.Sprintf("Name of the env var that will hold your %s token. "+
					"`%s` is the upstream standard; override if you run "+
					"multiple tools with conflicting tokens.", profile.Label, profile.FallbackEnv)).
				Placeholder(profile.FallbackEnv).
				Value(&cfg.EnvVarName).
				Validate(credentials.ValidateEnvVarName),
		),
	)
}

// singleFetchTokenForm asks whether the user wants the wizard to run OAuth and
// display the token once. Default is yes.
func singleFetchTokenForm(cfg *AuthConfig) *huh.Form {
	cfg.FetchToken = true

	return huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Fetch a token now?").
				Description("Select Yes to run OAuth and have the wizard display the token once for you to copy into your shell profile. " +
					"Select No if you already have a token (e.g. from 1Password, a password manager, or manual PAT creation).").
				Affirmative("Yes, run OAuth").
				Negative("No, I already have one").
				Value(&cfg.FetchToken),
		),
	)
}

// singleDisplayOnceForm shows the captured token inside a non-editable input,
// requires the user to submit the form (the acknowledgement), and then the
// caller is responsible for zeroing the token. The token is exposed briefly on
// screen by design — the user explicitly opted in, and the prompt instructs
// them to copy the value before confirming.
func singleDisplayOnceForm(envVarName, token string) *huh.Form {
	title := fmt.Sprintf("Save the token to %s", envVarName)
	description := fmt.Sprintf(
		"Copy the token below, add it to your shell profile as:\n\n"+
			"    export %s=%s\n\n"+
			"This prompt will not be shown again. The token is NOT written to the config file.",
		envVarName, token)

	var acknowledged bool

	return huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title(title).
				Description(description),
			huh.NewConfirm().
				Title("Have you saved the token?").
				Affirmative("Yes, continue").
				Negative("No, cancel").
				Value(&acknowledged),
		),
	)
}

// newAuthFormConfig constructs the default form creators (bound to the profile)
// and applies caller-supplied overrides (typically from tests).
func newAuthFormConfig(profile Profile, opts ...AuthFormOption) *authFormConfig {
	c := &authFormConfig{
		storageModeFormCreator: func(cfg *AuthConfig) *huh.Form { return singleStorageModeForm(profile, cfg) },
		envVarNameFormCreator:  func(cfg *AuthConfig) *huh.Form { return singleEnvVarNameForm(profile, cfg) },
		fetchTokenFormCreator:  singleFetchTokenForm,
		displayOnceFormCreator: singleDisplayOnceForm,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// runAuthFormStage runs a single form stage, wrapping any error in a
// cancellation message.
func runAuthFormStage(creator func(*AuthConfig) *huh.Form, cfg *AuthConfig) error {
	if creator == nil {
		return nil
	}

	form := creator(cfg)
	if form == nil {
		return nil
	}

	if err := form.Run(); err != nil {
		return errors.Wrap(err, "auth form cancelled")
	}

	return nil
}

// manualTokenInstructions returns the stderr guidance shown before prompting
// for a manual token. It has three shapes, selected by what the profile knows:
//
//   - host and URL template — the resolved URL and (if set) the scope list;
//   - host, no template — a generic message naming the host;
//   - no host — see [hostlessTokenInstructions].
//
// No forge-specific URL or scope literal lives in the wizard itself; all of it
// comes from the profile.
func manualTokenInstructions(profile Profile) string {
	host := strings.TrimSpace(profile.Host)

	// Both branches below interpolate the host, so a profile that has none must
	// divert before either runs. Not every forge has a default host the way
	// GitHub has github.com — Gitea instances are all self-hosted — and an empty
	// host renders "https:///user/settings/applications" or a sentence ending in
	// a blank. That is broken output, not degraded output.
	if host == "" {
		return hostlessTokenInstructions(profile)
	}

	if profile.TokenCreateURLTemplate == "" {
		return fmt.Sprintf("Create a personal access token on %s, then paste it below.", host)
	}

	url := strings.ReplaceAll(profile.TokenCreateURLTemplate, "{host}", host)

	lines := []string{
		"Open this URL on any device to create a personal access token:",
		"",
		"  " + url,
	}

	if profile.TokenScopes != "" {
		lines = append(lines, "", "Required scopes: "+profile.TokenScopes)
	}

	return strings.Join(lines, "\n")
}

// hostlessTokenInstructions is the guidance for a profile carrying no host.
//
// The URL template is deliberately left unrendered: without a host it can only
// produce a URL nobody can visit, and a broken link is worse than no link. The
// scope list is still worth stating — the user must select the same scopes by
// hand wherever their instance lives.
func hostlessTokenInstructions(profile Profile) string {
	subject := strings.TrimSpace(profile.Label)
	if subject == "" {
		subject = "your forge"
	}

	lines := []string{
		fmt.Sprintf("Create a personal access token for %s, then paste it below.", subject),
	}

	if profile.TokenScopes != "" {
		lines = append(lines, "", "Required scopes: "+profile.TokenScopes)
	}

	return strings.Join(lines, "\n")
}

// promptManualToken is the fallback authentication path used when the OAuth
// device flow cannot complete — typically on headless servers where no web
// browser is available to launch.
//
// It prints the profile's token-creation guidance (see [manualTokenInstructions]),
// then prompts for the token via a password input that does not echo to the
// terminal. The resulting token is indistinguishable from one issued by OAuth
// and is returned to the caller for mode-specific persistence.
func promptManualToken(profile Profile) (string, error) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, manualTokenInstructions(profile))
	fmt.Fprintln(os.Stderr)

	var token string

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(profile.Label + " Personal Access Token").
				Description("Paste the token you just generated. Input is hidden.").
				EchoMode(huh.EchoModePassword).
				Value(&token).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("token is required")
					}

					return nil
				}),
		),
	).Run()
	if err != nil {
		return "", errors.Wrap(err, "manual token prompt cancelled")
	}

	return strings.TrimSpace(token), nil
}
