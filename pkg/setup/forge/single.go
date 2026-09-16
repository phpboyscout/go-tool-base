package forge

import (
	"context"
	"fmt"
	"strings"

	"charm.land/huh/v2"

	"gitlab.com/phpboyscout/go/credentials"
	"gitlab.com/phpboyscout/go/errors"

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

	authCfg := &AuthConfig{FetchToken: true}

	if err := setup.RunForm(ctx, p, authForm(ctx, profile, authCfg)); err != nil {
		return errors.Wrap(err, "auth form cancelled")
	}

	// CI belt-and-braces: refuse literal even if the selector somehow offered it.
	if err := credentials.RefuseLiteralUnderCI(authCfg.StorageMode); err != nil {
		return err
	}

	if authCfg.StorageMode == credentials.ModeEnvVar && authCfg.EnvVarName == "" {
		authCfg.EnvVarName = profile.FallbackEnv
	}

	return i.runAuthCredentialStage(ctx, p, cfg, authCfg)
}

// runAuthCredentialStage dispatches to the per-mode branch and writes the
// resulting config. Split from [Initialiser.configureAuth] so the outer
// function stays under the cyclomatic-complexity budget.
func (i *Initialiser) runAuthCredentialStage(
	ctx context.Context,
	p *props.Props,
	cfg setup.Editor,
	authCfg *AuthConfig,
) error {
	var err error

	switch authCfg.StorageMode {
	case credentials.ModeEnvVar:
		err = i.runEnvVarAuth(ctx, p, cfg.View(), authCfg)
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

// runEnvVarAuth drives the env-var branch after the form: optionally runs
// OAuth and displays the token once for the user to copy into their shell
// profile. The env-var name and the fetch decision are the form's.
func (i *Initialiser) runEnvVarAuth(
	ctx context.Context,
	p *props.Props,
	cfg config.Reader,
	authCfg *AuthConfig,
) error {
	if !authCfg.FetchToken {
		// User already has a token via other means (shell profile, 1Password
		// CLI, etc.). We write only the env-var reference.
		return nil
	}

	token, err := i.captureToken(ctx, p, cfg)
	if err != nil {
		return err
	}

	if err := setup.RunForm(ctx, p, singleDisplayOnceForm(authCfg.EnvVarName, token)); err != nil {
		return errors.Wrap(err, "token display cancelled")
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
func (i *Initialiser) captureToken(ctx context.Context, p *props.Props, cfg config.Reader) (string, error) {
	log := p.GetLogger()

	auth, err := i.authenticator(ctx, cfg)
	if err != nil {
		log.Warn("interactive login unavailable, falling back to manual token entry",
			"provider", i.profile.Label, "reason", err)

		return promptManualToken(ctx, p, i.profile)
	}

	log.Info("Logging in", "provider", i.profile.Label, "host", i.profile.Host)

	token, err := auth.Login(ctx, i.prompter)
	if err == nil {
		return token, nil
	}

	log.Warn("OAuth flow unavailable, falling back to manual token entry",
		"provider", i.profile.Label, "reason", err)

	return promptManualToken(ctx, p, i.profile)
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

// authForm is the single-token wizard's one form (spec 0198): the storage
// mode, then, for env-var mode, the variable's name and whether to fetch a
// token now. The OAuth capture and the display-once page follow it, because
// a token has to exist before it can be shown.
func authForm(ctx context.Context, profile Profile, cfg *AuthConfig) *huh.Form {
	notEnvVar := func() bool { return cfg.StorageMode != credentials.ModeEnvVar }

	return huh.NewForm(
		setup.StorageModeGroup(ctx, &cfg.StorageMode, func() bool { return false }).
			Title(profile.Label+" Credential Storage"),
		huh.NewGroup(
			huh.NewInput().
				Key("env-var").
				Title("Environment Variable Name").
				Description(fmt.Sprintf("Name of the env var that will hold your %s token. "+
					"`%s` is the upstream standard and is what blank means; override if you run "+
					"multiple tools with conflicting tokens.", profile.Label, profile.FallbackEnv)).
				Placeholder(profile.FallbackEnv).
				Value(&cfg.EnvVarName).
				Validate(func(s string) error {
					if s == "" {
						return nil
					}

					return credentials.ValidateEnvVarName(s)
				}),
			huh.NewConfirm().
				Key("fetch-token").
				Title("Fetch a token now?").
				Description("Select Yes to run OAuth and have the wizard display the token once for you to copy into your shell profile. "+
					"Select No if you already have a token (e.g. from 1Password, a password manager, or manual PAT creation).").
				Affirmative("Yes, run OAuth").
				Negative("No, I already have one").
				Value(&cfg.FetchToken),
		).WithHideFunc(notEnvVar),
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

// ErrNoTokenEntered is returned when the manual token prompt closes without a
// token.
var ErrNoTokenEntered = errors.NewSentinel("gtb.setup.forge.no_token_entered", "no token was entered")

// promptManualToken is the fallback authentication path used when the OAuth
// device flow cannot complete — typically on headless servers where no web
// browser is available to launch.
//
// It prints the profile's token-creation guidance (see [manualTokenInstructions]),
// then prompts for the token via a password input that does not echo to the
// terminal. The resulting token is indistinguishable from one issued by OAuth
// and is returned to the caller for mode-specific persistence.
func promptManualToken(ctx context.Context, p *props.Props, profile Profile) (string, error) {
	out := p.GetIO().Err()
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, manualTokenInstructions(profile))
	_, _ = fmt.Fprintln(out)

	var token string

	err := setup.RunForm(ctx, p, huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(profile.Label+" Personal Access Token").
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
	))
	if err != nil {
		return "", errors.Wrap(err, "manual token prompt cancelled")
	}

	// huh's accessible mode swallows a field's error (a password prompt with
	// no terminal behind it), so an empty answer is the only trace of it.
	if strings.TrimSpace(token) == "" {
		return "", ErrNoTokenEntered
	}

	return strings.TrimSpace(token), nil
}
