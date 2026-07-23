package forge

import (
	"context"
	"encoding/json"
	"fmt"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"

	"gitlab.com/phpboyscout/go/credentials"

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

// DualFormOption configures the dual-credential init form for testability.
type DualFormOption func(*dualFormConfig)

type dualFormConfig struct {
	storageModeFormCreator func(*DualConfig) *huh.Form
	envVarNamesFormCreator func(*DualConfig) *huh.Form
	credentialsFormCreator func(*DualConfig) *huh.Form
}

// Form-slot indices used by [WithDualForm]. The creator callback returns a slice
// of forms; these constants identify which stage each slot feeds.
const (
	formSlotStorageMode = 0
	formSlotEnvVarNames = 1
	formSlotCredentials = 2
)

// WithDualForm injects custom form creators into the dual-credential wizard for
// testing. The creator returns forms in order:
//
//	[0] storage-mode selector
//	[1] env-var names (env-var mode only)
//	[2] username + app_password inputs (keychain / literal modes)
//
// Returning fewer forms is allowed — the runner skips stages whose slot is nil
// or absent.
func WithDualForm(creator func(*DualConfig) []*huh.Form) DualFormOption {
	return func(c *dualFormConfig) {
		c.storageModeFormCreator = formAtIndex(creator, formSlotStorageMode)
		c.envVarNamesFormCreator = formAtIndex(creator, formSlotEnvVarNames)
		c.credentialsFormCreator = formAtIndex(creator, formSlotCredentials)
	}
}

func formAtIndex(creator func(*DualConfig) []*huh.Form, i int) func(*DualConfig) *huh.Form {
	return func(cfg *DualConfig) *huh.Form {
		forms := creator(cfg)
		if i < len(forms) {
			return forms[i]
		}

		return nil
	}
}

// configureDual runs the dual-credential wizard and persists the captured
// credentials according to the selected storage mode. ctx is the caller's context —
// it must not carry a stage-wide deadline (the forms are human-paced); the
// keychain write derives its own KeychainOpTimeout at the call site.
func (i *Initialiser) configureDual(ctx context.Context, p *props.Props, cfg setup.Editor) error {
	bbCfg, err := runForms(i.profile, i.dualOpts...) //nolint:contextcheck // form creators are ctx-free seams; the storage-mode form derives its own per-op KeychainOpTimeout ctx for the availability Probe by design
	if err != nil {
		return err
	}

	if err := credentials.RefuseLiteralUnderCI(bbCfg.StorageMode); err != nil {
		return err
	}

	err = writeDualCredentials(ctx, i.profile, cfg, p.Tool.Name, bbCfg)

	// Best-effort clear of the struct's secret fields. Go strings are immutable,
	// so this only drops the references — see M-4 in
	// docs/development/security-decisions.md.
	bbCfg.AppPassword = ""

	return err
}

// dualStorageModeForm presents the three-mode selector. Literal is hidden under
// CI; keychain is hidden unless [credentials.Probe] passes.
func dualStorageModeForm(profile Profile, cfg *DualConfig) *huh.Form {
	ctx, cancel := context.WithTimeout(context.Background(), credentials.KeychainOpTimeout)
	defer cancel()

	choices := credentials.ModeChoices(credentials.IsCI(), credentials.Probe(ctx),
		"Environment variable references (recommended)",
		"OS keychain (single JSON blob)",
		"Literal values in config file (plaintext)")

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
				Description(dualStorageModeDescription(credentials.IsCI())).
				Options(options...).
				Value(&cfg.StorageMode),
		),
	)
}

func dualStorageModeDescription(ci bool) string {
	if ci {
		return "CI environment detected — only environment variable references are permitted."
	}

	return "Env-var references keep both credentials out of config; keychain stores one JSON blob; literal writes both to config as plaintext."
}

// dualEnvVarNamesForm prompts for the username and app-password env var names,
// defaulting to the profile's fallback names.
func dualEnvVarNamesForm(profile Profile, cfg *DualConfig) *huh.Form {
	if cfg.UsernameEnvName == "" {
		cfg.UsernameEnvName = profile.UserFallbackEnv
	}

	if cfg.AppPasswordEnvName == "" {
		cfg.AppPasswordEnvName = profile.PassFallbackEnv
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Username env var name").
				Description(fmt.Sprintf("Name of the env var that holds your %s username.", profile.Label)).
				Placeholder(profile.UserFallbackEnv).
				Value(&cfg.UsernameEnvName).
				Validate(credentials.ValidateEnvVarName),
			huh.NewInput().
				Title("App password env var name").
				Description(fmt.Sprintf("Name of the env var that holds your %s app password.", profile.Label)).
				Placeholder(profile.PassFallbackEnv).
				Value(&cfg.AppPasswordEnvName).
				Validate(credentials.ValidateEnvVarName),
		),
	)
}

// dualCredentialsForm collects the username and app password values for keychain
// and literal modes. The app password input uses [huh.EchoModePassword] so it is
// not echoed to the terminal.
func dualCredentialsForm(profile Profile, cfg *DualConfig) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(profile.Label+" username").
				Value(&cfg.Username).
				Validate(func(s string) error {
					if s == "" {
						return errors.New("username is required")
					}

					return nil
				}),
			huh.NewInput().
				Title(profile.Label+" app password").
				Description("Input is hidden. Create one at bitbucket.org → Personal settings → App passwords.").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.AppPassword).
				Validate(func(s string) error {
					if s == "" {
						return errors.New("app password is required")
					}

					return nil
				}),
		),
	)
}

// runForms drives the wizard through storage-mode selection and then either
// env-var-names or credentials based on the selected mode.
func runForms(profile Profile, opts ...DualFormOption) (*DualConfig, error) {
	fCfg := newFormConfig(profile, opts...)
	cfg := &DualConfig{}

	if err := runFormStage(fCfg.storageModeFormCreator, cfg); err != nil {
		return nil, err
	}

	switch cfg.StorageMode {
	case credentials.ModeEnvVar:
		if err := runFormStage(fCfg.envVarNamesFormCreator, cfg); err != nil {
			return nil, err
		}
	case credentials.ModeKeychain, credentials.ModeLiteral, "":
		if err := runFormStage(fCfg.credentialsFormCreator, cfg); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func newFormConfig(profile Profile, opts ...DualFormOption) *dualFormConfig {
	c := &dualFormConfig{
		storageModeFormCreator: func(cfg *DualConfig) *huh.Form { return dualStorageModeForm(profile, cfg) },
		envVarNamesFormCreator: func(cfg *DualConfig) *huh.Form { return dualEnvVarNamesForm(profile, cfg) },
		credentialsFormCreator: func(cfg *DualConfig) *huh.Form { return dualCredentialsForm(profile, cfg) },
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

func runFormStage(creator func(*DualConfig) *huh.Form, cfg *DualConfig) error {
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
