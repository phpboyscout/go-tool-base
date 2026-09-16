package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/huh/v2"

	"gitlab.com/phpboyscout/go/credentials"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// DualConfig captures the dual-credential wizard's outputs. Fields unused by the
// selected storage mode are ignored.
type DualConfig struct {
	StorageMode credentials.Mode

	// Username and AppPassword hold the collected credentials for keychain and
	// literal modes. Unused in env-var mode.
	Username    string
	AppPassword string

	// UsernameEnvName and AppPasswordEnvName hold env-var names for env-var
	// mode. Default to the profile's fallback env-var names.
	UsernameEnvName    string
	AppPasswordEnvName string
}

// configureDual runs the dual-credential wizard and persists the captured
// credentials according to the selected storage mode. ctx is the caller's context —
// it must not carry a stage-wide deadline (the forms are human-paced); the
// keychain write derives its own KeychainOpTimeout at the call site.
func (i *Initialiser) configureDual(ctx context.Context, p *props.Props, cfg setup.Editor) error {
	bbCfg := &DualConfig{}

	if err := setup.RunForm(ctx, p, dualForm(ctx, p, i.profile, bbCfg)); err != nil {
		return errors.Wrap(err, "auth form cancelled")
	}

	if err := finaliseDualConfig(i.profile, bbCfg); err != nil {
		return err
	}

	err := writeDualCredentials(ctx, i.profile, cfg, p.Tool.Name, bbCfg)

	// Best-effort clear of the struct's secret fields. Go strings are immutable,
	// so this only drops the references — see M-4 in
	// docs/development/security-decisions.md.
	bbCfg.AppPassword = ""

	return err
}

// dualForm is the dual-credential wizard's one form (spec 0198): the storage
// mode, then the page that mode needs. Env-var mode names the two variables
// (blank keeps the profile's fallback); keychain and literal modes take the
// username and app password themselves.
func dualForm(ctx context.Context, p *props.Props, profile Profile, cfg *DualConfig) *huh.Form {
	notEnvVar := func() bool { return cfg.StorageMode != credentials.ModeEnvVar }
	envVar := func() bool { return cfg.StorageMode == credentials.ModeEnvVar }

	return huh.NewForm(
		setup.StorageModeGroup(ctx, p, &cfg.StorageMode, func() bool { return false }).
			Title(profile.Label+" Credential Storage"),
		huh.NewGroup(
			huh.NewInput().
				Key("username-env").
				Title("Username env var name").
				Description(fmt.Sprintf("Name of the env var that holds your %s username. Blank means `%s`.",
					profile.Label, profile.UserFallbackEnv)).
				Placeholder(profile.UserFallbackEnv).
				Value(&cfg.UsernameEnvName).
				Validate(optionalEnvVarName),
			huh.NewInput().
				Key("app-password-env").
				Title("App password env var name").
				Description(fmt.Sprintf("Name of the env var that holds your %s app password. Blank means `%s`.",
					profile.Label, profile.PassFallbackEnv)).
				Placeholder(profile.PassFallbackEnv).
				Value(&cfg.AppPasswordEnvName).
				Validate(optionalEnvVarName),
		).WithHideFunc(notEnvVar),
		huh.NewGroup(
			huh.NewInput().
				Key("username").
				Title(profile.Label+" username").
				Value(&cfg.Username).
				Validate(required("username")),
			huh.NewInput().
				Key("app-password").
				Title(profile.Label+" app password").
				Description("Input is hidden. Create one at bitbucket.org → Personal settings → App passwords.").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.AppPassword).
				Validate(required("app password")),
		).WithHideFunc(envVar),
	)
}

// optionalEnvVarName accepts blank (the fallback stands in) or a valid name.
func optionalEnvVarName(s string) error {
	if s == "" {
		return nil
	}

	return credentials.ValidateEnvVarName(s)
}

func required(what string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.Newf("%s is required", what)
		}

		return nil
	}
}

// finaliseDualConfig settles what the form left to the caller: literal mode
// is refused under CI, blank env-var names take the profile's fallbacks, and
// a credential mode with either value missing is an error rather than an
// empty write (huh's accessible mode cannot read a password without a
// terminal and swallows the failure).
func finaliseDualConfig(profile Profile, cfg *DualConfig) error {
	if err := credentials.RefuseLiteralUnderCI(cfg.StorageMode); err != nil {
		return err
	}

	if cfg.StorageMode == credentials.ModeEnvVar {
		if cfg.UsernameEnvName == "" {
			cfg.UsernameEnvName = profile.UserFallbackEnv
		}

		if cfg.AppPasswordEnvName == "" {
			cfg.AppPasswordEnvName = profile.PassFallbackEnv
		}

		return nil
	}

	if strings.TrimSpace(cfg.Username) == "" || strings.TrimSpace(cfg.AppPassword) == "" {
		return ErrCredentialsIncomplete
	}

	return nil
}

// ErrCredentialsIncomplete is returned when a credential storage mode was
// chosen but the username or app password was not entered.
var ErrCredentialsIncomplete = errors.NewSentinel("gtb.setup.forge.credentials_incomplete",
	"a username and an app password are both required")

// writeDualCredentials persists the captured DualConfig according to the
// selected storage mode.
func writeDualCredentials(ctx context.Context, profile Profile, cfg setup.Editor, toolName string, bbCfg *DualConfig) error {
	switch bbCfg.StorageMode {
	case credentials.ModeEnvVar:
		return writeDualEnvRefs(profile, cfg, bbCfg)
	case credentials.ModeKeychain:
		return writeKeychainBlob(ctx, profile, cfg, toolName, bbCfg)
	case credentials.ModeLiteral, "":
		return writeDualLiterals(profile, cfg, bbCfg)
	default:
		return errors.Newf("unsupported %s credential storage mode %q", profile.Label, bbCfg.StorageMode)
	}
}

func writeDualEnvRefs(profile Profile, cfg setup.Editor, bbCfg *DualConfig) error {
	winning := map[string]any{}
	if bbCfg.UsernameEnvName != "" {
		winning[profile.userEnvKey()] = bbCfg.UsernameEnvName
	}

	if bbCfg.AppPasswordEnvName != "" {
		winning[profile.passEnvKey()] = bbCfg.AppPasswordEnvName
	}

	if len(winning) == 0 {
		return nil
	}

	return setup.WriteExclusive(cfg, winning, profile.dualCredentialKeys())
}

func writeDualLiterals(profile Profile, cfg setup.Editor, bbCfg *DualConfig) error {
	winning := map[string]any{}
	if bbCfg.Username != "" {
		winning[profile.userKey()] = bbCfg.Username
	}

	if bbCfg.AppPassword != "" {
		winning[profile.passKey()] = bbCfg.AppPassword
	}

	if len(winning) == 0 {
		return nil
	}

	return setup.WriteExclusive(cfg, winning, profile.dualCredentialKeys())
}

// writeKeychainBlob serialises the dual credentials into a JSON object and
// stores them under a single keychain entry, mirroring the format the resolver
// expects. Extracted from the switch to keep [writeDualCredentials] under the
// cyclomatic-complexity budget.
func writeKeychainBlob(ctx context.Context, profile Profile, cfg setup.Editor, toolName string, bbCfg *DualConfig) error {
	if toolName == "" {
		return errors.New("cannot write keychain entry without a tool name")
	}

	if bbCfg.Username == "" || bbCfg.AppPassword == "" {
		return errors.New("keychain mode requires both username and app password")
	}

	blob, err := json.Marshal(map[string]string{
		"username":     bbCfg.Username,
		"app_password": bbCfg.AppPassword,
	})
	if err != nil {
		return errors.Wrap(err, "marshal bitbucket keychain blob")
	}

	// Fresh per-operation deadline at the store call site (the documented
	// KeychainOpTimeout contract); the caller's ctx spans the human-paced forms.
	storeCtx, cancel := context.WithTimeout(ctx, credentials.KeychainOpTimeout)
	defer cancel()

	if err := credentials.Store(storeCtx, toolName, profile.KeychainAccount, string(blob)); err != nil {
		return errors.WithHint(
			errors.Wrapf(err, "storing %s credentials in OS keychain", profile.Label),
			"If the keychain is locked, unlock it and re-run; otherwise pick env-var or literal mode instead.")
	}

	return setup.WriteExclusive(cfg,
		map[string]any{profile.keychainKey(): toolName + "/" + profile.KeychainAccount},
		profile.dualCredentialKeys())
}
