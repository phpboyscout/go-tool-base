package ai

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/phpboyscout/go-tool-base/pkg/chat"
	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/credentials"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
)

// keychainOpTimeout bounds any single credentials-backend operation
// initiated by the setup wizard (Probe, Store). The wizard is
// interactive and synchronous; a remote-store backend (Vault, SSM)
// that hangs would block the user indefinitely without this guard.
// OS-keychain backends complete well under this bound.
const keychainOpTimeout = 5 * time.Second

var skipAI bool

func init() {
	setup.Register(props.AiCmd,
		[]setup.InitialiserProvider{
			func(p *props.Props) setup.Initialiser {
				if skipAI {
					return nil
				}

				return NewAIInitialiser(p)
			},
		},
		[]setup.SubcommandProvider{
			func(p *props.Props) []*cobra.Command {
				return []*cobra.Command{NewCmdInitAI(p)}
			},
		},
		[]setup.FeatureFlag{
			func(cmd *cobra.Command) {
				is_ci := (os.Getenv("CI") == "true")
				cmd.Flags().BoolVarP(&skipAI, "skip-ai", "a", is_ci, "skip configuring AI tokens")
			},
		},
	)
}

//go:embed assets/*
var assets embed.FS

// AIConfig holds the AI provider configuration captured from the form.
type AIConfig struct {
	Provider    string
	APIKey      string
	ExistingKey string // populated from disk config; used to show masked hint in the form
	// StorageMode selects how the API key is persisted. Defaults to
	// [credentials.ModeEnvVar] when the wizard presents the choice.
	// [credentials.ModeLiteral] is refused when the process runs
	// under CI (CI=true); the only accepted CI credential path is a
	// platform-injected environment variable referenced via env-var
	// mode.
	StorageMode credentials.Mode
	// EnvVarName is the environment variable name recorded in
	// {provider}.api.env when [StorageMode] is [credentials.ModeEnvVar].
	// Ignored in literal mode.
	EnvVarName string
}

// FormOption configures the AI init form for testability.
type FormOption func(*formConfig)

type formConfig struct {
	providerFormCreator    func(*AIConfig) *huh.Form
	storageModeFormCreator func(*AIConfig) *huh.Form
	envVarFormCreator      func(*AIConfig) *huh.Form
	keyFormCreator         func(*AIConfig) *huh.Form
}

// Form-slot indices used by [WithAIForm]. The creator callback
// returns a slice of forms; these constants identify which stage
// each slot feeds.
const (
	formSlotProvider    = 0
	formSlotStorageMode = 1
	formSlotEnvVar      = 2
	formSlotKey         = 3
)

// WithAIForm allows injecting custom form creators for testing. The
// creator returns forms in order: [0] provider, [1] storage mode,
// [2] env-var name (or key input), [3] key input fallback.
//
// Returning fewer forms is allowed — the runner skips stages whose
// slot is nil or absent.
func WithAIForm(creator func(*AIConfig) []*huh.Form) FormOption {
	return func(c *formConfig) {
		c.providerFormCreator = formAtIndex(creator, formSlotProvider)
		c.storageModeFormCreator = formAtIndex(creator, formSlotStorageMode)
		c.envVarFormCreator = formAtIndex(creator, formSlotEnvVar)
		c.keyFormCreator = formAtIndex(creator, formSlotKey)
	}
}

func formAtIndex(creator func(*AIConfig) []*huh.Form, i int) func(*AIConfig) *huh.Form {
	return func(cfg *AIConfig) *huh.Form {
		forms := creator(cfg)
		if i < len(forms) {
			return forms[i]
		}

		return nil
	}
}

// providerLabel returns a human-friendly label for the provider.
func providerLabel(provider string) string {
	switch provider {
	case string(chat.ProviderClaude):
		return "Anthropic (Claude)"
	case string(chat.ProviderOpenAI):
		return "OpenAI"
	case string(chat.ProviderGemini):
		return "Google Gemini"
	default:
		return provider
	}
}

func defaultProviderForm(cfg *AIConfig) *huh.Form {
	// Build provider selection fields
	providerFields := []huh.Field{
		huh.NewSelect[string]().
			Title("Select AI Provider").
			Description("Choose the default AI provider for this tool").
			Options(
				huh.NewOption("Claude (Anthropic)", string(chat.ProviderClaude)),
				huh.NewOption("OpenAI", string(chat.ProviderOpenAI)),
				huh.NewOption("Gemini (Google)", string(chat.ProviderGemini)),
			).
			Value(&cfg.Provider),
	}

	// Warn if AI_PROVIDER env var is set — it takes precedence over the config file
	if envProvider := os.Getenv(chat.EnvAIProvider); envProvider != "" {
		providerFields = append([]huh.Field{
			huh.NewNote().
				Title("⚠ Environment Override Detected").
				Description(fmt.Sprintf(
					"AI\\_PROVIDER is set to %q. This environment variable takes precedence over the config file. "+
						"Changes to the provider below will only take effect when AI\\_PROVIDER is unset.",
					envProvider,
				)),
		}, providerFields...)
	}

	return huh.NewForm(
		huh.NewGroup(providerFields...),
	)
}

// defaultStorageModeForm offers the three-mode selector. Literal
// mode is hidden when the process runs under CI=true — the wizard
// refuses to write a plaintext credential to a config file that will
// almost certainly leak via CI artefacts or logs. Keychain is hidden
// unless the backend is both compiled in AND passes [credentials.Probe]
// (canary round-trip) so the user is never offered an option that
// will fail the moment they pick it.
func defaultStorageModeForm(cfg *AIConfig) *huh.Form {
	ctx, cancel := context.WithTimeout(context.Background(), keychainOpTimeout)
	defer cancel()

	options := storageModeOptions(isCI(), credentials.Probe(ctx))

	if cfg.StorageMode == "" {
		cfg.StorageMode = credentials.ModeEnvVar
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[credentials.Mode]().
				Title("Credential Storage").
				Description(storageModeDescription(isCI())).
				Options(options...).
				Value(&cfg.StorageMode),
		),
	)
}

// defaultEnvVarForm prompts for the name of the environment variable
// that will hold the API key. Only rendered when the user selects
// [credentials.ModeEnvVar].
func defaultEnvVarForm(cfg *AIConfig) *huh.Form {
	defaultName := providerEnvVar(cfg.Provider)

	if cfg.EnvVarName == "" {
		cfg.EnvVarName = defaultName
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Environment Variable Name").
				Description(fmt.Sprintf(
					"Name of the env var that will contain your %s API key. "+
						"Set the variable in your shell profile (or CI secret store) "+
						"after running this wizard.",
					providerLabel(cfg.Provider))).
				Placeholder(defaultName).
				Validate(validateEnvVarName).
				Value(&cfg.EnvVarName),
		),
	)
}

// storageModeOptions returns the huh.Option list for the storage
// mode selector, filtered by CI state, keychain build tag, and a
// live-backend probe. The keychain option only surfaces when the
// backend is compiled in AND reachable — a locked keychain or a
// headless Linux host without D-Bus must not leave the user stuck on
// a dead option during first-run setup.
func storageModeOptions(ci, keychainUsable bool) []huh.Option[credentials.Mode] {
	opts := []huh.Option[credentials.Mode]{
		huh.NewOption("Environment variable reference (recommended)", credentials.ModeEnvVar),
	}

	if keychainUsable {
		opts = append(opts, huh.NewOption("OS keychain", credentials.ModeKeychain))
	}

	if !ci {
		opts = append(opts, huh.NewOption("Literal value in config file (plaintext)", credentials.ModeLiteral))
	}

	return opts
}

func storageModeDescription(ci bool) string {
	if ci {
		return "CI environment detected — only environment variable references are permitted. Configure the env var via your CI platform's secret injection."
	}

	return "Environment variable references keep secrets out of the config file. Pick literal mode only for throwaway environments."
}

// validateEnvVarName enforces a conservative ^[A-Z][A-Z0-9_]{0,63}$
// shape so the name is a valid POSIX env var and fits downstream
// shell/YAML contexts without quoting.
func validateEnvVarName(name string) error {
	if name == "" {
		return errors.New("env var name is required")
	}

	if !envVarNameRe.MatchString(name) {
		return errors.New("env var name must match ^[A-Z][A-Z0-9_]{0,63}$")
	}

	return nil
}

var envVarNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// isCI reports whether the process appears to be running under a CI
// system. Mirrors the check used by the --skip-ai flag default.
func isCI() bool {
	return os.Getenv("CI") == "true"
}

func defaultKeyForm(cfg *AIConfig) *huh.Form {
	keyFields := []huh.Field{
		huh.NewInput().
			Title(fmt.Sprintf("%s API Key", providerLabel(cfg.Provider))).
			DescriptionFunc(func() string {
				if cfg.ExistingKey != "" {
					masked := maskKey(cfg.ExistingKey)

					return fmt.Sprintf("Current key: %s — leave blank to keep existing", masked)
				}

				return fmt.Sprintf("Enter your %s API key", providerLabel(cfg.Provider))
			}, &cfg.ExistingKey).
			Placeholder("paste new key or press enter to keep existing").
			EchoMode(huh.EchoModePassword).
			Value(&cfg.APIKey),
	}

	// Warn if the provider's token env var is set
	envName := providerEnvVar(cfg.Provider)
	if envName != "" {
		if envVal := os.Getenv(envName); envVal != "" {
			escapedName := strings.ReplaceAll(envName, "_", "\\_")
			keyFields = append([]huh.Field{
				huh.NewNote().
					Title("⚠ Environment Override Detected").
					Description(fmt.Sprintf(
						"%s is set. This environment variable takes precedence over the config file. "+
							"Changes to the API key below will only take effect when %s is unset.",
						escapedName, escapedName,
					)),
			}, keyFields...)
		}
	}

	return huh.NewForm(
		huh.NewGroup(keyFields...),
	)
}

// providerEnvVar returns the environment variable name for the provider's API key.
func providerEnvVar(provider string) string {
	switch provider {
	case string(chat.ProviderClaude):
		return chat.EnvClaudeKey
	case string(chat.ProviderOpenAI):
		return chat.EnvOpenAIKey
	case string(chat.ProviderGemini):
		return chat.EnvGeminiKey
	default:
		return ""
	}
}

// maskKey returns a masked version of the key showing only the last 4 characters.
func maskKey(key string) string {
	const visibleChars = 4

	if len(key) <= visibleChars {
		return "****"
	}

	return "****" + key[len(key)-visibleChars:]
}

// AIInitialiser implements setup.Initialiser for AI provider configuration.
type AIInitialiser struct {
	formOpts []FormOption
}

// NewAIInitialiser creates a new AIInitialiser and mounts its assets.
func NewAIInitialiser(p *props.Props, opts ...FormOption) *AIInitialiser {
	if p.Assets != nil {
		p.Assets.Mount(assets, "pkg/setup/ai")
	}

	return &AIInitialiser{formOpts: opts}
}

// Name returns the human-readable name for this initialiser.
func (a *AIInitialiser) Name() string {
	return "AI integration"
}

// IsConfigured checks if a valid AI provider is set and its corresponding
// API key is present.
func (a *AIInitialiser) IsConfigured(cfg config.Containable) bool {
	provider := cfg.GetString(chat.ConfigKeyAIProvider)
	if !isValidProvider(provider) {
		return false
	}

	keyPath := providerConfigKey(provider)

	return keyPath != "" && cfg.GetString(keyPath) != ""
}

// Configure runs the interactive AI configuration forms and populates the shared config.
func (a *AIInitialiser) Configure(p *props.Props, cfg config.Containable) error {
	aiCfg, err := runAIForms(cfg, a.formOpts...)
	if err != nil {
		return err
	}

	// Write results directly into the shared configuration container
	cfg.Set(chat.ConfigKeyAIProvider, aiCfg.Provider)

	return writeAICredentialKeys(cfg, p.Tool.Name, aiCfg)
}

// writeAICredentialKeys writes the provider's credential keys based
// on the selected storage mode. Only one of the literal / env-var /
// keychain keys is set — this ensures config.GetString returns the
// intended value without stale entries from a prior mode. toolName
// names the service used by the keychain write; see
// [providerKeychainAccount] for the account shape.
func writeAICredentialKeys(cfg config.Containable, toolName string, aiCfg *AIConfig) error {
	keys, ok := providerConfigKeys(aiCfg.Provider)
	if !ok {
		return nil
	}

	return applyStorageModeWrite(cfg, toolName, keys, aiCfg)
}

// providerConfigKeyTriple groups the three provider-specific config
// key paths so storage-mode dispatch can be written as a single
// switch rather than a switch-per-key. ok=false means the provider
// is unknown and the caller should no-op.
type providerConfigKeyTriple struct {
	env, literal, keychain string
}

// providerConfigKeys returns the env/literal/keychain config key
// triple for a provider, with ok=false for unknown providers.
func providerConfigKeys(provider string) (providerConfigKeyTriple, bool) {
	envKey := providerEnvConfigKey(provider)
	litKey := providerConfigKey(provider)
	kcKey := providerKeychainConfigKey(provider)

	if envKey == "" || litKey == "" || kcKey == "" {
		return providerConfigKeyTriple{}, false
	}

	return providerConfigKeyTriple{env: envKey, literal: litKey, keychain: kcKey}, true
}

// applyStorageModeWrite performs the single Set call corresponding
// to the selected storage mode. Extracted so the cyclomatic cost of
// the four-arm switch doesn't hit the outer function's budget.
func applyStorageModeWrite(cfg config.Containable, toolName string, keys providerConfigKeyTriple, aiCfg *AIConfig) error {
	switch aiCfg.StorageMode {
	case credentials.ModeEnvVar:
		if aiCfg.EnvVarName != "" {
			cfg.Set(keys.env, aiCfg.EnvVarName)
		}

		return nil

	case credentials.ModeLiteral, "":
		// "" preserves prior behaviour for callers (tests) that
		// bypass the wizard and set APIKey directly.
		if aiCfg.APIKey != "" {
			cfg.Set(keys.literal, aiCfg.APIKey)
		}

		return nil

	case credentials.ModeKeychain:
		ref, err := storeAIKeyInKeychain(toolName, aiCfg)
		if err != nil {
			return err
		}

		if ref != "" {
			cfg.Set(keys.keychain, ref)
		}

		return nil

	default:
		return errors.Newf("unknown credential storage mode %q", aiCfg.StorageMode)
	}
}

// storeAIKeyInKeychain writes the API key into the OS keychain under
// "<toolName>/<account>" and returns the reference string recorded in
// the config file. A blank APIKey is a no-op so running the wizard
// with a placeholder form in tests doesn't touch real credentials.
func storeAIKeyInKeychain(toolName string, aiCfg *AIConfig) (string, error) {
	if aiCfg.APIKey == "" {
		return "", nil
	}

	account := providerKeychainAccount(aiCfg.Provider)
	if toolName == "" || account == "" {
		return "", errors.New("cannot write keychain entry without both tool name and provider account")
	}

	ctx, cancel := context.WithTimeout(context.Background(), keychainOpTimeout)
	defer cancel()

	if err := credentials.Store(ctx, toolName, account, aiCfg.APIKey); err != nil {
		return "", errors.WithHint(
			errors.Wrap(err, "storing AI API key in OS keychain"),
			"If the keychain is locked, unlock it and re-run; otherwise pick env-var or literal mode instead.")
	}

	return toolName + "/" + account, nil
}

// RunAIInit executes the AI configuration form and writes the results to the config file.
func RunAIInit(p *props.Props, dir string, opts ...FormOption) error {
	targetFile := filepath.Join(dir, setup.DefaultConfigFilename)

	existingCfg, _ := config.LoadFilesContainer(p.FS, config.WithConfigFiles(targetFile))
	if existingCfg == nil {
		existingCfg = config.NewContainerFromViper(nil, viper.New())
	}

	aiCfg, err := runAIForms(existingCfg, opts...)
	if err != nil {
		return err
	}

	return writeAIConfig(p, dir, aiCfg)
}

// runAIForms runs the multi-stage AI configuration forms and returns the result.
//
// Stages: provider selection → storage-mode selection → either
// env-var name input (env-var mode) or secret input (literal/keychain
// mode). Split across helpers to keep each stage under the
// cyclomatic-complexity budget.
func runAIForms(existingCfg config.Containable, opts ...FormOption) (*AIConfig, error) {
	fCfg := newAIFormConfig(opts...)

	aiCfg := &AIConfig{}

	// Pre-populate provider from existing config.
	if provider := existingCfg.GetString(chat.ConfigKeyAIProvider); isValidProvider(provider) {
		aiCfg.Provider = provider
	}

	if err := runFormStage(fCfg.providerFormCreator, aiCfg); err != nil {
		return nil, err
	}

	aiCfg.ExistingKey = existingCfg.GetString(providerConfigKey(aiCfg.Provider))

	if err := runFormStage(fCfg.storageModeFormCreator, aiCfg); err != nil {
		return nil, err
	}

	// CI defence-in-depth: refuse literal even if a test-injected
	// creator bypassed the storage-mode form.
	if aiCfg.StorageMode == credentials.ModeLiteral && isCI() {
		return nil, errors.WithHint(
			errors.New("literal credential storage is refused under CI"),
			"CI environments must use platform-injected secrets referenced via env-var mode.")
	}

	return runAICredentialStage(fCfg, aiCfg)
}

// newAIFormConfig constructs the default formConfig and applies options.
func newAIFormConfig(opts ...FormOption) *formConfig {
	c := &formConfig{
		providerFormCreator:    defaultProviderForm,
		storageModeFormCreator: defaultStorageModeForm,
		envVarFormCreator:      defaultEnvVarForm,
		keyFormCreator:         defaultKeyForm,
	}
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// runFormStage runs a single form-creator stage, wrapping the form
// error in a form-cancelled message.
func runFormStage(creator func(*AIConfig) *huh.Form, aiCfg *AIConfig) error {
	form := creator(aiCfg)
	if form == nil {
		return nil
	}

	if err := form.Run(); err != nil {
		return errors.Newf("AI configuration form cancelled: %w", err)
	}

	return nil
}

// runAICredentialStage runs either the env-var name form (env-var
// mode) or the API key input form (literal / keychain mode).
func runAICredentialStage(fCfg *formConfig, aiCfg *AIConfig) (*AIConfig, error) {
	if aiCfg.StorageMode == credentials.ModeEnvVar {
		if err := runFormStage(fCfg.envVarFormCreator, aiCfg); err != nil {
			return nil, err
		}

		return aiCfg, nil
	}

	if err := runFormStage(fCfg.keyFormCreator, aiCfg); err != nil {
		return nil, err
	}

	// Blank submission in literal mode preserves the existing key.
	if aiCfg.APIKey == "" && aiCfg.ExistingKey != "" {
		aiCfg.APIKey = aiCfg.ExistingKey
	}

	return aiCfg, nil
}

// providerConfigKey returns the viper config key for the provider's literal API key.
func providerConfigKey(provider string) string {
	switch provider {
	case string(chat.ProviderClaude):
		return chat.ConfigKeyClaudeKey
	case string(chat.ProviderOpenAI):
		return chat.ConfigKeyOpenAIKey
	case string(chat.ProviderGemini):
		return chat.ConfigKeyGeminiKey
	default:
		return ""
	}
}

// providerEnvConfigKey returns the viper config key that records the
// env var NAME (not value) for the provider's API key when stored
// in [credentials.ModeEnvVar].
func providerEnvConfigKey(provider string) string {
	switch provider {
	case string(chat.ProviderClaude):
		return chat.ConfigKeyClaudeEnv
	case string(chat.ProviderOpenAI):
		return chat.ConfigKeyOpenAIEnv
	case string(chat.ProviderGemini):
		return chat.ConfigKeyGeminiEnv
	default:
		return ""
	}
}

// providerKeychainConfigKey returns the viper config key that records
// the "<service>/<account>" reference for the provider's API key
// when stored in [credentials.ModeKeychain].
func providerKeychainConfigKey(provider string) string {
	switch provider {
	case string(chat.ProviderClaude):
		return chat.ConfigKeyClaudeKeychain
	case string(chat.ProviderOpenAI):
		return chat.ConfigKeyOpenAIKeychain
	case string(chat.ProviderGemini):
		return chat.ConfigKeyGeminiKeychain
	default:
		return ""
	}
}

// providerKeychainAccount returns the keychain account name under
// which the provider's API key is stored. The service portion is the
// tool name so the keychain UI labels entries clearly
// ("<tool>/anthropic.api", "<tool>/openai.api", …). Changing these
// values would strand existing keychain entries on user machines;
// evolve with care.
func providerKeychainAccount(provider string) string {
	switch provider {
	case string(chat.ProviderClaude):
		return "anthropic.api"
	case string(chat.ProviderOpenAI):
		return "openai.api"
	case string(chat.ProviderGemini):
		return "gemini.api"
	default:
		return ""
	}
}

func writeAIConfig(p *props.Props, dir string, aiCfg *AIConfig) error {
	targetFile := filepath.Join(dir, setup.DefaultConfigFilename)

	cfg := viper.New()
	cfg.SetFs(p.FS)
	cfg.SetConfigType("yaml")

	if err := loadExistingAIConfig(cfg, p.FS, targetFile); err != nil {
		return err
	}

	configMap := map[string]any{
		"ai": map[string]any{
			"provider": aiCfg.Provider,
		},
	}

	if err := setAICredentialOnViper(cfg, p.Tool.Name, aiCfg); err != nil {
		return err
	}

	if err := cfg.MergeConfigMap(configMap); err != nil {
		return errors.Newf("failed to merge AI config: %w", err)
	}

	// Ensure directory exists
	const defaultDirPerm = 0o755

	if err := p.FS.MkdirAll(dir, defaultDirPerm); err != nil {
		return errors.Newf("failed to create config directory: %w", err)
	}

	if err := cfg.WriteConfigAs(targetFile); err != nil {
		return err
	}

	// Restrict config file permissions — the file may contain API keys.
	const configFilePerm = 0o600

	return p.FS.Chmod(targetFile, configFilePerm)
}

// loadExistingAIConfig reads the target file (if present) into the
// viper instance so we merge rather than clobber. A missing file is
// treated as "no existing config" — only genuine read errors surface
// downstream.
func loadExistingAIConfig(cfg *viper.Viper, fs afero.Fs, targetFile string) error {
	exists, _ := afero.Exists(fs, targetFile)
	if !exists {
		return nil
	}

	data, err := afero.ReadFile(fs, targetFile)
	if err != nil {
		return errors.Newf("failed to read existing config: %w", err)
	}

	if readErr := cfg.ReadConfig(bytes.NewReader(data)); readErr != nil {
		return errors.Newf("failed to parse existing config: %w", readErr)
	}

	return nil
}

// setAICredentialOnViper writes exactly one of the credential keys
// (env-var reference, literal, or keychain reference) based on the
// selected storage mode, so a prior mode cannot mask the new one at
// resolve time. toolName is the keychain service used for
// [credentials.ModeKeychain] writes.
func setAICredentialOnViper(cfg *viper.Viper, toolName string, aiCfg *AIConfig) error {
	keys, _ := providerConfigKeys(aiCfg.Provider)

	switch aiCfg.StorageMode {
	case credentials.ModeEnvVar:
		if keys.env != "" && aiCfg.EnvVarName != "" {
			cfg.Set(keys.env, aiCfg.EnvVarName)
		}
	case credentials.ModeLiteral, "":
		if keys.literal != "" && aiCfg.APIKey != "" {
			cfg.Set(keys.literal, aiCfg.APIKey)
		}
	case credentials.ModeKeychain:
		return writeKeychainRefToViper(cfg, toolName, keys.keychain, aiCfg)
	default:
		return errors.Newf("unknown credential storage mode %q", aiCfg.StorageMode)
	}

	return nil
}

// writeKeychainRefToViper stores the API key in the backend and
// records the reference on the viper instance. Split out of
// [setAICredentialOnViper] to keep that function under the
// cyclomatic-complexity budget.
func writeKeychainRefToViper(cfg *viper.Viper, toolName, kcKey string, aiCfg *AIConfig) error {
	ref, err := storeAIKeyInKeychain(toolName, aiCfg)
	if err != nil {
		return err
	}

	if kcKey != "" && ref != "" {
		cfg.Set(kcKey, ref)
	}

	return nil
}

// validProviders is the set of permitted AI provider identifiers.
var validProviders = []string{
	string(chat.ProviderClaude),
	string(chat.ProviderOpenAI),
	string(chat.ProviderGemini),
}

// isValidProvider returns true if the provider is one of the permitted values.
func isValidProvider(provider string) bool {
	return slices.Contains(validProviders, provider)
}

// IsAIConfigured checks if the AI provider and its corresponding key are configured.
func IsAIConfigured(p *props.Props) bool {
	if p.Config == nil {
		return false
	}

	provider := p.Config.GetString(chat.ConfigKeyAIProvider)
	if !isValidProvider(provider) {
		return false
	}

	keyPath := providerConfigKey(provider)

	return keyPath != "" && p.Config.GetString(keyPath) != ""
}

// NewCmdInitAI creates the `init ai` subcommand.
func NewCmdInitAI(p *props.Props, opts ...FormOption) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ai",
		Short: "Configure AI provider integration",
		Long:  `Configures the AI provider and API keys for AI-powered features such as documentation Q&A and code analysis.`,
		Run: func(cmd *cobra.Command, _ []string) {
			dir, _ := cmd.Flags().GetString("dir")

			if err := RunAIInit(p, dir, opts...); err != nil {
				p.Logger.Fatalf("Failed to configure AI: %s", err)
			}

			p.Logger.Info("AI configuration saved successfully")
		},
	}

	cmd.Flags().String("dir", setup.GetDefaultConfigDir(p.FS, p.Tool.Name), "directory containing the config file")

	return cmd
}
