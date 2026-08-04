package ai

import (
	"context"
	"embed"
	"fmt"
	"os"
	"slices"
	"strings"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	gochat "gitlab.com/phpboyscout/go/chat"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go/credentials"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

var skipAI bool

func init() {
	setup.RegisterAssets(props.AiCmd, "ai", &assets)
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
	case string(gochat.ProviderClaude):
		return "Anthropic (Claude)"
	case string(gochat.ProviderOpenAI):
		return "OpenAI"
	case string(gochat.ProviderGemini):
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
				huh.NewOption("Claude (Anthropic)", string(gochat.ProviderClaude)),
				huh.NewOption("OpenAI", string(gochat.ProviderOpenAI)),
				huh.NewOption("Gemini (Google)", string(gochat.ProviderGemini)),
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
				Title("Credential Storage").
				Description(storageModeDescription(credentials.IsCI())).
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
				Validate(credentials.ValidateEnvVarName).
				Value(&cfg.EnvVarName),
		),
	)
}

func storageModeDescription(ci bool) string {
	if ci {
		return "CI environment detected — only environment variable references are permitted. Configure the env var via your CI platform's secret injection."
	}

	return "Environment variable references keep secrets out of the config file. Pick literal mode only for throwaway environments."
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
	case string(gochat.ProviderClaude):
		return chat.EnvClaudeKey
	case string(gochat.ProviderOpenAI):
		return chat.EnvOpenAIKey
	case string(gochat.ProviderGemini):
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

// NewAIInitialiser creates a new AIInitialiser. Its asset bundle is
// registered from init() via setup.RegisterAssets, applied for enabled
// features at root construction.
func NewAIInitialiser(_ *props.Props, opts ...FormOption) *AIInitialiser {
	return &AIInitialiser{formOpts: opts}
}

// Name returns the human-readable name for this initialiser.
func (a *AIInitialiser) Name() string {
	return "AI integration"
}

// IsConfigured checks if a valid AI provider is set and its corresponding
// API key is present.
func (a *AIInitialiser) IsConfigured(cfg config.Reader) bool {
	provider := cfg.GetString(chat.ConfigKeyAIProvider)
	if !isValidProvider(provider) {
		return false
	}

	keyPath := providerConfigKey(provider)

	return keyPath != "" && cfg.GetString(keyPath) != ""
}

// Configure runs the interactive AI configuration forms and writes the
// results through the editor.
func (a *AIInitialiser) Configure(ctx context.Context, p *props.Props, cfg setup.Editor) error {
	aiCfg, err := runAIForms(cfg.View(), a.formOpts...)
	if err != nil {
		return err
	}

	return writeAIConfig(ctx, cfg, p.Tool.Name, aiCfg)
}

// writeAIConfig commits the provider selection and its credential as one
// transactional write. The credential changes are computed first — including
// the fallible OS-keychain store in keychain mode — so a failure there returns
// before anything touches the config file. Persisting the provider in the same
// Apply is what prevents an orphaned `ai.provider` with no credential when the
// keychain is locked (the pre-Store code wrote the whole document at once for
// the same reason).
func writeAIConfig(ctx context.Context, cfg setup.Editor, toolName string, aiCfg *AIConfig) error {
	credChanges, err := credentialChanges(ctx, toolName, aiCfg)
	if err != nil {
		return err
	}

	changes := append(
		[]config.Change{config.Set(chat.ConfigKeyAIProvider, aiCfg.Provider)},
		credChanges...,
	)

	return cfg.Apply(changes...)
}

// credentialChanges builds the config changes for the provider's credential in
// the selected storage mode — exactly one of the literal / env-var / keychain
// keys is set and the other two removed (via [setup.ExclusiveChanges]), so a
// stale entry from a prior mode cannot mask the new value. In keychain mode the
// external keychain store runs here, before any change is returned, so its
// failure aborts the whole write. toolName names the service used by the
// keychain write; see [providerKeychainAccount] for the account shape.
func credentialChanges(ctx context.Context, toolName string, aiCfg *AIConfig) ([]config.Change, error) {
	keys, ok := providerConfigKeys(aiCfg.Provider)
	if !ok {
		return nil, nil
	}

	return storageModeChanges(ctx, toolName, keys, aiCfg)
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

// all returns the triple as the full key set a [setup.WriteExclusive] call
// enforces the single-credential-key invariant over.
func (k providerConfigKeyTriple) all() []string {
	return []string{k.env, k.literal, k.keychain}
}

// storageModeChanges returns the config changes for the selected storage mode,
// dispatching to a per-mode helper so each arm stays readable. A blank
// credential field yields no changes (the wizard was bypassed in tests, or the
// user left the field empty), never an error.
func storageModeChanges(ctx context.Context, toolName string, keys providerConfigKeyTriple, aiCfg *AIConfig) ([]config.Change, error) {
	switch aiCfg.StorageMode {
	case credentials.ModeEnvVar:
		return envVarChanges(keys, aiCfg), nil
	case credentials.ModeLiteral, "":
		// "" preserves prior behaviour for callers (tests) that
		// bypass the wizard and set APIKey directly.
		return literalChanges(keys, aiCfg), nil
	case credentials.ModeKeychain:
		return keychainChanges(ctx, toolName, keys, aiCfg)
	default:
		return nil, errors.Newf("unknown credential storage mode %q", aiCfg.StorageMode)
	}
}

func envVarChanges(keys providerConfigKeyTriple, aiCfg *AIConfig) []config.Change {
	if aiCfg.EnvVarName == "" {
		return nil
	}

	return setup.ExclusiveChanges(map[string]any{keys.env: aiCfg.EnvVarName}, keys.all())
}

func literalChanges(keys providerConfigKeyTriple, aiCfg *AIConfig) []config.Change {
	if aiCfg.APIKey == "" {
		return nil
	}

	return setup.ExclusiveChanges(map[string]any{keys.literal: aiCfg.APIKey}, keys.all())
}

func keychainChanges(ctx context.Context, toolName string, keys providerConfigKeyTriple, aiCfg *AIConfig) ([]config.Change, error) {
	ref, err := storeAIKeyInKeychain(ctx, toolName, aiCfg)
	if err != nil {
		return nil, err
	}

	if ref == "" {
		return nil, nil
	}

	return setup.ExclusiveChanges(map[string]any{keys.keychain: ref}, keys.all()), nil
}

// storeAIKeyInKeychain writes the API key into the OS keychain under
// "<toolName>/<account>" and returns the reference string recorded in
// the config file. A blank APIKey is a no-op so running the wizard
// with a placeholder form in tests doesn't touch real credentials.
func storeAIKeyInKeychain(ctx context.Context, toolName string, aiCfg *AIConfig) (string, error) {
	if aiCfg.APIKey == "" {
		return "", nil
	}

	account := providerKeychainAccount(aiCfg.Provider)
	if toolName == "" || account == "" {
		return "", errors.New("cannot write keychain entry without both tool name and provider account")
	}

	// Per-operation deadline derived from the caller's ctx at the store call
	// site (the documented KeychainOpTimeout contract).
	storeCtx, cancel := context.WithTimeout(ctx, credentials.KeychainOpTimeout)
	defer cancel()

	if err := credentials.Store(storeCtx, toolName, account, aiCfg.APIKey); err != nil {
		return "", errors.WithHint(
			errors.Wrap(err, "storing AI API key in OS keychain"),
			"If the keychain is locked, unlock it and re-run; otherwise pick env-var or literal mode instead.")
	}

	return toolName + "/" + account, nil
}

// RunAIInit executes the AI configuration form and writes the results to the
// config file, which is seeded from the merged init template when absent.
func RunAIInit(ctx context.Context, p *props.Props, dir string, opts ...FormOption) error {
	editor, _, err := setup.OpenConfigEditor(ctx, p, dir, false)
	if err != nil {
		return err
	}

	aiCfg, err := runAIForms(editor.View(), opts...)
	if err != nil {
		return err
	}

	return writeAIConfig(ctx, editor, p.Tool.Name, aiCfg)
}

// runAIForms runs the multi-stage AI configuration forms and returns the result.
//
// Stages: provider selection → storage-mode selection → either
// env-var name input (env-var mode) or secret input (literal/keychain
// mode). Split across helpers to keep each stage under the
// cyclomatic-complexity budget.
func runAIForms(existingCfg config.Reader, opts ...FormOption) (*AIConfig, error) {
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
	if err := credentials.RefuseLiteralUnderCI(aiCfg.StorageMode); err != nil {
		return nil, err
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

// providerConfigKey returns the config key for the provider's literal API key.
func providerConfigKey(provider string) string {
	switch provider {
	case string(gochat.ProviderClaude):
		return chat.ConfigKeyClaudeKey
	case string(gochat.ProviderOpenAI):
		return chat.ConfigKeyOpenAIKey
	case string(gochat.ProviderGemini):
		return chat.ConfigKeyGeminiKey
	default:
		return ""
	}
}

// providerEnvConfigKey returns the config key that records the
// env var NAME (not value) for the provider's API key when stored
// in [credentials.ModeEnvVar].
func providerEnvConfigKey(provider string) string {
	switch provider {
	case string(gochat.ProviderClaude):
		return chat.ConfigKeyClaudeEnv
	case string(gochat.ProviderOpenAI):
		return chat.ConfigKeyOpenAIEnv
	case string(gochat.ProviderGemini):
		return chat.ConfigKeyGeminiEnv
	default:
		return ""
	}
}

// providerKeychainConfigKey returns the config key that records
// the "<service>/<account>" reference for the provider's API key
// when stored in [credentials.ModeKeychain].
func providerKeychainConfigKey(provider string) string {
	switch provider {
	case string(gochat.ProviderClaude):
		return chat.ConfigKeyClaudeKeychain
	case string(gochat.ProviderOpenAI):
		return chat.ConfigKeyOpenAIKeychain
	case string(gochat.ProviderGemini):
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
	case string(gochat.ProviderClaude):
		return "anthropic.api"
	case string(gochat.ProviderOpenAI):
		return "openai.api"
	case string(gochat.ProviderGemini):
		return "gemini.api"
	default:
		return ""
	}
}

// validProviders is the set of permitted AI provider identifiers.
var validProviders = []string{
	string(gochat.ProviderClaude),
	string(gochat.ProviderOpenAI),
	string(gochat.ProviderGemini),
}

// isValidProvider returns true if the provider is one of the permitted values.
func isValidProvider(provider string) bool {
	return slices.Contains(validProviders, provider)
}

// IsAIConfigured checks if the AI provider and its corresponding key are configured.
func IsAIConfigured(p props.ConfigProvider) bool {
	store := p.GetConfig()
	if store == nil {
		return false
	}

	cfg := store.View()

	provider := cfg.GetString(chat.ConfigKeyAIProvider)
	if !isValidProvider(provider) {
		return false
	}

	keyPath := providerConfigKey(provider)

	return keyPath != "" && cfg.GetString(keyPath) != ""
}

// NewCmdInitAI creates the `init ai` subcommand.
func NewCmdInitAI(p *props.Props, opts ...FormOption) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ai",
		Short: "Configure AI provider integration",
		Long: `Configure the AI provider and API key used for AI-powered features such
as documentation Q&A and code analysis. The key is stored via the
three-mode selector: an environment variable reference (recommended
default), the OS keychain, or a literal value in the config file. Literal
mode is refused when running under CI.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, _ := cmd.Flags().GetString("dir")

			if err := RunAIInit(cmd.Context(), p, dir, opts...); err != nil {
				return errors.Wrap(err, "failed to configure AI")
			}

			p.Logger.Info("AI configuration saved successfully")

			return nil
		},
	}

	cmd.Flags().String("dir", setup.GetDefaultConfigDir(p.FS, p.Tool.Name), "directory containing the config file")

	return cmd
}
