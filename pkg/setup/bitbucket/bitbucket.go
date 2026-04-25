package bitbucket

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/credentials"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
)

// keychainOpTimeout bounds any single credentials-backend operation
// initiated by the Bitbucket wizard. Matches pkg/setup/ai's bound so
// a remote-store backend (Vault, SSM) that hangs can't stall the
// interactive flow.
const keychainOpTimeout = 5 * time.Second

// bitbucketKeychainAccount is the account portion of the
// "<service>/<account>" reference used by keychain mode. A single
// entry carries the JSON-serialised dual credentials.
const bitbucketKeychainAccount = "bitbucket.auth"

// envVarNameRe enforces `^[A-Z][A-Z0-9_]{0,63}$` for env var names —
// same shape as pkg/setup/ai so callers see a consistent validation
// error across wizards.
var envVarNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// skipBitbucket mirrors the --skip-login / --skip-ai pattern used by
// the other setup subsystems. Set by the feature flag below.
var skipBitbucket bool //nolint:gochecknoglobals // cobra flag binding

func init() {
	setup.Register(props.FeatureCmd("bitbucket"),
		[]setup.InitialiserProvider{
			func(p *props.Props) setup.Initialiser {
				if skipBitbucket {
					return nil
				}

				return NewInitialiser(p)
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

// BitbucketConfig captures the wizard's outputs. Fields unused by the
// selected storage mode are ignored.
type BitbucketConfig struct {
	StorageMode credentials.Mode

	// Username and AppPassword hold the collected credentials for
	// keychain and literal modes. Unused in env-var mode.
	Username    string
	AppPassword string

	// UsernameEnvName and AppPasswordEnvName hold env-var names for
	// env-var mode. Default to BITBUCKET_USERNAME /
	// BITBUCKET_APP_PASSWORD respectively.
	UsernameEnvName    string
	AppPasswordEnvName string
}

// FormOption configures the Bitbucket init form for testability.
type FormOption func(*formConfig)

type formConfig struct {
	storageModeFormCreator func(*BitbucketConfig) *huh.Form
	envVarNamesFormCreator func(*BitbucketConfig) *huh.Form
	credentialsFormCreator func(*BitbucketConfig) *huh.Form
}

// Form-slot indices used by [WithForm]. The creator callback returns
// a slice of forms; these constants identify which stage each slot
// feeds.
const (
	formSlotStorageMode = 0
	formSlotEnvVarNames = 1
	formSlotCredentials = 2
)

// WithForm injects custom form creators into the wizard for testing.
// The creator returns forms in order:
//
//	[0] storage-mode selector
//	[1] env-var names (env-var mode only)
//	[2] username + app_password inputs (keychain / literal modes)
//
// Returning fewer forms is allowed — the runner skips stages whose
// slot is nil or absent.
func WithForm(creator func(*BitbucketConfig) []*huh.Form) FormOption {
	return func(c *formConfig) {
		c.storageModeFormCreator = formAtIndex(creator, formSlotStorageMode)
		c.envVarNamesFormCreator = formAtIndex(creator, formSlotEnvVarNames)
		c.credentialsFormCreator = formAtIndex(creator, formSlotCredentials)
	}
}

func formAtIndex(creator func(*BitbucketConfig) []*huh.Form, i int) func(*BitbucketConfig) *huh.Form {
	return func(cfg *BitbucketConfig) *huh.Form {
		forms := creator(cfg)
		if i < len(forms) {
			return forms[i]
		}

		return nil
	}
}

// Initialiser implements setup.Initialiser for Bitbucket auth.
type Initialiser struct {
	formOpts []FormOption
}

// InitialiserOption configures the [Initialiser].
type InitialiserOption func(*Initialiser)

// WithFormOptions propagates [FormOption]s into the wizard. Tests use
// this to inject deterministic form creators.
func WithFormOptions(opts ...FormOption) InitialiserOption {
	return func(i *Initialiser) { i.formOpts = append(i.formOpts, opts...) }
}

// NewInitialiser constructs a new [Initialiser] with the supplied
// options applied.
func NewInitialiser(_ *props.Props, opts ...InitialiserOption) *Initialiser {
	i := &Initialiser{}
	for _, opt := range opts {
		opt(i)
	}

	return i
}

// Name returns the human-readable label for this initialiser.
func (i *Initialiser) Name() string {
	return "Bitbucket authentication"
}

// IsConfigured reports whether any of the three storage modes is
// already recorded in the config.
func (i *Initialiser) IsConfigured(cfg config.Containable) bool {
	return cfg.GetString("bitbucket.keychain") != "" ||
		cfg.GetString("bitbucket.username.env") != "" ||
		cfg.GetString("bitbucket.app_password.env") != "" ||
		cfg.GetString("bitbucket.username") != "" ||
		cfg.GetString("bitbucket.app_password") != ""
}

// Configure runs the interactive wizard and persists the captured
// credentials according to the selected storage mode.
func (i *Initialiser) Configure(p *props.Props, cfg config.Containable) error {
	ctx, cancel := context.WithTimeout(context.Background(), keychainOpTimeout)
	defer cancel()

	bbCfg, err := runForms(i.formOpts...)
	if err != nil {
		return err
	}

	if bbCfg.StorageMode == credentials.ModeLiteral && isCI() {
		return errors.WithHint(
			errors.New("literal credential storage is refused under CI"),
			"CI environments must use platform-injected secrets referenced via env-var mode.")
	}

	err = writeBitbucketCredentials(ctx, cfg, p.Tool.Name, bbCfg)

	// Best-effort clear of the struct's secret fields. Go strings are
	// immutable, so this only drops the references — see M-4 in
	// docs/development/security-decisions.md.
	bbCfg.AppPassword = ""

	return err
}

// defaultStorageModeForm presents the three-mode selector. Literal
// is hidden under CI; keychain is hidden unless [credentials.Probe]
// passes.
func defaultStorageModeForm(cfg *BitbucketConfig) *huh.Form {
	ctx, cancel := context.WithTimeout(context.Background(), keychainOpTimeout)
	defer cancel()

	options := storageModeOptions(isCI(), credentials.Probe(ctx))

	if cfg.StorageMode == "" {
		cfg.StorageMode = credentials.ModeEnvVar
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[credentials.Mode]().
				Title("Bitbucket Credential Storage").
				Description(storageModeDescription(isCI())).
				Options(options...).
				Value(&cfg.StorageMode),
		),
	)
}

func storageModeOptions(ci, keychainUsable bool) []huh.Option[credentials.Mode] {
	opts := []huh.Option[credentials.Mode]{
		huh.NewOption("Environment variable references (recommended)", credentials.ModeEnvVar),
	}

	if keychainUsable {
		opts = append(opts, huh.NewOption("OS keychain (single JSON blob)", credentials.ModeKeychain))
	}

	if !ci {
		opts = append(opts, huh.NewOption("Literal values in config file (plaintext)", credentials.ModeLiteral))
	}

	return opts
}

func storageModeDescription(ci bool) string {
	if ci {
		return "CI environment detected — only environment variable references are permitted."
	}

	return "Env-var references keep both credentials out of config; keychain stores one JSON blob; literal writes both to config as plaintext."
}

// defaultEnvVarNamesForm prompts for the username and app-password
// env var names, defaulting to the upstream-standard names.
func defaultEnvVarNamesForm(cfg *BitbucketConfig) *huh.Form {
	if cfg.UsernameEnvName == "" {
		cfg.UsernameEnvName = "BITBUCKET_USERNAME"
	}

	if cfg.AppPasswordEnvName == "" {
		cfg.AppPasswordEnvName = "BITBUCKET_APP_PASSWORD"
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Username env var name").
				Description("Name of the env var that holds your Bitbucket username.").
				Placeholder("BITBUCKET_USERNAME").
				Value(&cfg.UsernameEnvName).
				Validate(validateEnvVarName),
			huh.NewInput().
				Title("App password env var name").
				Description("Name of the env var that holds your Bitbucket app password.").
				Placeholder("BITBUCKET_APP_PASSWORD").
				Value(&cfg.AppPasswordEnvName).
				Validate(validateEnvVarName),
		),
	)
}

// defaultCredentialsForm collects the username and app password
// values for keychain and literal modes. The app password input
// uses [huh.EchoModePassword] so it is not echoed to the terminal.
func defaultCredentialsForm(cfg *BitbucketConfig) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Bitbucket username").
				Value(&cfg.Username).
				Validate(func(s string) error {
					if s == "" {
						return errors.New("username is required")
					}

					return nil
				}),
			huh.NewInput().
				Title("Bitbucket app password").
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

// runForms drives the wizard through storage-mode selection and then
// either env-var-names or credentials based on the selected mode.
func runForms(opts ...FormOption) (*BitbucketConfig, error) {
	fCfg := newFormConfig(opts...)
	cfg := &BitbucketConfig{}

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

func newFormConfig(opts ...FormOption) *formConfig {
	c := &formConfig{
		storageModeFormCreator: defaultStorageModeForm,
		envVarNamesFormCreator: defaultEnvVarNamesForm,
		credentialsFormCreator: defaultCredentialsForm,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

func runFormStage(creator func(*BitbucketConfig) *huh.Form, cfg *BitbucketConfig) error {
	if creator == nil {
		return nil
	}

	form := creator(cfg)
	if form == nil {
		return nil
	}

	if err := form.Run(); err != nil {
		return errors.Wrap(err, "Bitbucket auth form cancelled")
	}

	return nil
}

// writeBitbucketCredentials persists the captured BitbucketConfig
// according to the selected storage mode.
func writeBitbucketCredentials(ctx context.Context, cfg config.Containable, toolName string, bbCfg *BitbucketConfig) error {
	switch bbCfg.StorageMode {
	case credentials.ModeEnvVar:
		if bbCfg.UsernameEnvName != "" {
			cfg.Set("bitbucket.username.env", bbCfg.UsernameEnvName)
		}

		if bbCfg.AppPasswordEnvName != "" {
			cfg.Set("bitbucket.app_password.env", bbCfg.AppPasswordEnvName)
		}

		return nil

	case credentials.ModeKeychain:
		return writeKeychainBlob(ctx, cfg, toolName, bbCfg)

	case credentials.ModeLiteral, "":
		if bbCfg.Username != "" {
			cfg.Set("bitbucket.username", bbCfg.Username)
		}

		if bbCfg.AppPassword != "" {
			cfg.Set("bitbucket.app_password", bbCfg.AppPassword)
		}

		return nil

	default:
		return errors.Newf("unsupported Bitbucket credential storage mode %q", bbCfg.StorageMode)
	}
}

// writeKeychainBlob serialises the dual credentials into a JSON
// object and stores them under a single keychain entry, mirroring
// the format the resolver (pkg/vcs/bitbucket) expects. Extracted
// from the switch to keep [writeBitbucketCredentials] under the
// cyclomatic-complexity budget.
func writeKeychainBlob(ctx context.Context, cfg config.Containable, toolName string, bbCfg *BitbucketConfig) error {
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

	if err := credentials.Store(ctx, toolName, bitbucketKeychainAccount, string(blob)); err != nil {
		return errors.WithHint(
			errors.Wrap(err, "storing Bitbucket credentials in OS keychain"),
			"If the keychain is locked, unlock it and re-run; otherwise pick env-var or literal mode instead.")
	}

	cfg.Set("bitbucket.keychain", toolName+"/"+bitbucketKeychainAccount)

	return nil
}

// validateEnvVarName enforces `^[A-Z][A-Z0-9_]{0,63}$` so the name is
// a valid POSIX env var.
func validateEnvVarName(name string) error {
	if name == "" {
		return errors.New("env var name is required")
	}

	if !envVarNameRe.MatchString(name) {
		return errors.New("env var name must match ^[A-Z][A-Z0-9_]{0,63}$")
	}

	return nil
}

func isCI() bool {
	return os.Getenv("CI") == "true"
}

// RunBitbucketInit executes the wizard against an existing config
// container, typically invoked by [NewCmdInitBitbucket].
func RunBitbucketInit(p *props.Props, cfg config.Containable) error {
	i := NewInitialiser(p)

	return i.Configure(p, cfg)
}

// NewCmdInitBitbucket creates the `init bitbucket` subcommand.
func NewCmdInitBitbucket(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bitbucket",
		Short: "Configure Bitbucket authentication (username + app password)",
		Long: `Configures Bitbucket credentials via the three-mode selector: environment variable references (recommended default), OS keychain (single JSON blob), or literal values in the config file.

Bitbucket's dual-credential model (username + app_password) is handled natively — env-var mode records two env-var names, keychain mode stores a single JSON blob, literal mode writes both fields to config.`,
		Run: func(cmd *cobra.Command, _ []string) {
			dir, _ := cmd.Flags().GetString("dir")

			if err := RunInitCmd(p, dir); err != nil {
				p.Logger.Fatalf("Failed to configure Bitbucket: %s", err)
			}

			p.Logger.Info("Bitbucket configuration saved successfully")
		},
	}

	cmd.Flags().String("dir", setup.GetDefaultConfigDir(p.FS, p.Tool.Name), "directory containing the config file")

	return cmd
}

// RunInitCmd loads or creates the target config, runs the wizard,
// and writes the updated config back to disk with 0600 permissions.
func RunInitCmd(p *props.Props, dir string) error {
	targetFile := filepath.Join(dir, setup.DefaultConfigFilename)

	c, err := config.LoadFilesContainer(p.FS, config.WithConfigFiles(targetFile))
	if err != nil {
		v := viper.New()
		if rerr := v.ReadConfig(bytes.NewReader(setup.DefaultConfig)); rerr != nil {
			return errors.Wrap(rerr, "failed to read default config")
		}

		c = config.NewContainerFromViper(nil, v)
	}

	if err := RunBitbucketInit(p, c); err != nil {
		return err
	}

	const dirPerm = 0o755
	if err := p.FS.MkdirAll(dir, dirPerm); err != nil {
		return errors.Wrap(err, "failed to create config directory")
	}

	if err := c.WriteConfigAs(targetFile); err != nil {
		return err
	}

	const configFilePerm = 0o600

	return p.FS.Chmod(targetFile, configFilePerm)
}
