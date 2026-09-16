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

// ErrProviderNotLinked is a provider chosen for init ai that this binary does
// not register: writing it to config would only fail at first use.
var ErrProviderNotLinked = errors.NewSentinel("gtb.setup.ai.provider_not_linked", "this tool does not link the chosen chat provider")

// linkedProviders builds the predicate from the registry. A binary that
// registers no provider at all cannot narrow, so every known provider is
// offered and doctor's Chat providers check is what reports the gap.
func linkedProviders(registered func() []gochat.Provider) func(gochat.Provider) bool {
	names := registered()
	if len(names) == 0 {
		return func(gochat.Provider) bool { return true }
	}

	return func(p gochat.Provider) bool { return slices.Contains(names, p) }
}

// providerLabel returns the framework's label for the provider, or the name
// itself for one the framework does not know.
func providerLabel(provider string) string {
	if d, ok := chat.DisplayFor(gochat.Provider(provider)); ok {
		return d.Label
	}

	return provider
}

// providerOptions offers the providers this binary links, labelled and
// glossed from the one display table (spec 0196 D7).
func providerOptions(linked func(gochat.Provider) bool) []huh.Option[string] {
	displays := chat.ProviderDisplays()
	opts := make([]huh.Option[string], 0, len(displays))

	for _, d := range displays {
		if !linked(d.ID) {
			continue
		}

		opts = append(opts, huh.NewOption(d.Label+"  ("+d.Gloss+")", string(d.ID)))
	}

	return opts
}

// envOverrideNote describes what AI_PROVIDER does when it is set: it is read
// only when ai.provider is unset, so the provider chosen here (which is
// written to config) wins over it. Empty when the variable is unset.
func envOverrideNote() string {
	envProvider := os.Getenv(chat.EnvAIProvider)
	if envProvider == "" {
		return ""
	}

	return fmt.Sprintf(
		"AI\\_PROVIDER is set to %q. It is used only when ai.provider is unset, so the provider chosen "+
			"below takes effect once written. To override per shell, set the tool's prefixed variable instead.",
		envProvider,
	)
}

// aiForm is the whole wizard as one form (spec 0198): the provider, then the
// storage mode, then the env var name or the key, each later page hidden
// when the answers before it make it moot. One form means one run, back
// navigation between pages, and a test that drives it from one stream.
func aiForm(ctx context.Context, p *props.Props, cfg *AIConfig, existing config.Reader, linked func(gochat.Provider) bool) *huh.Form {
	needsCredential := func() bool { return chat.NeedsCredential(gochat.Provider(cfg.Provider)) }

	return huh.NewForm(
		providerGroup(cfg, linked),
		setup.StorageModeGroup(ctx, p, &cfg.StorageMode, func() bool { return !needsCredential() }),
		envVarGroup(cfg, func() bool { return !needsCredential() || cfg.StorageMode != credentials.ModeEnvVar }),
		keyGroup(cfg, existing, func() bool { return !needsCredential() || cfg.StorageMode == credentials.ModeEnvVar }),
	)
}

// providerGroup offers the providers this binary links (spec 0196 D7), with
// a note when AI_PROVIDER is set saying what it does.
func providerGroup(cfg *AIConfig, linked func(gochat.Provider) bool) *huh.Group {
	// huh sizes an auto-height select to its options and then subtracts the
	// title and description lines, so the last options render off-screen
	// until the cursor reaches them (#43); the height is set explicitly.
	const titleAndDescriptionLines = 2

	options := providerOptions(linked)

	fields := []huh.Field{
		huh.NewSelect[string]().
			Key("provider").
			Title("Select AI Provider").
			Description("Choose the default AI provider for this tool").
			Options(options...).
			Height(len(options) + titleAndDescriptionLines).
			Value(&cfg.Provider).
			Validate(func(provider string) error {
				if !linked(gochat.Provider(provider)) {
					return errors.Wrapf(ErrProviderNotLinked, "%s", provider)
				}

				return nil
			}),
	}

	if note := envOverrideNote(); note != "" {
		fields = append([]huh.Field{huh.NewNote().Title("AI_PROVIDER is set").Description(note)}, fields...)
	}

	return huh.NewGroup(fields...).Title("AI Provider")
}

// envVarGroup asks the name of the variable that will hold the key. Blank
// takes the provider's well-known name, which the description shows.
func envVarGroup(cfg *AIConfig, hide func() bool) *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Key("env-var").
			Title("Environment Variable Name").
			DescriptionFunc(func() string {
				return fmt.Sprintf(
					"Name of the env var that will contain your %s API key; blank takes %s. "+
						"Set the variable in your shell profile (or CI secret store) after running this wizard.",
					providerLabel(cfg.Provider), providerEnvVar(cfg.Provider))
			}, &cfg.Provider).
			PlaceholderFunc(func() string { return providerEnvVar(cfg.Provider) }, &cfg.Provider).
			Validate(func(s string) error {
				if s == "" {
					return nil
				}

				return credentials.ValidateEnvVarName(s)
			}).
			Value(&cfg.EnvVarName),
	).WithHideFunc(hide)
}

// keyGroup asks the key itself, masked; blank keeps an existing one, which
// the description says (masked) when there is one.
func keyGroup(cfg *AIConfig, existing config.Reader, hide func() bool) *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Key("api-key").
			TitleFunc(func() string { return fmt.Sprintf("%s API Key", providerLabel(cfg.Provider)) }, &cfg.Provider).
			DescriptionFunc(func() string {
				var parts []string

				if current := existing.GetString(providerConfigKey(cfg.Provider)); current != "" {
					parts = append(parts, fmt.Sprintf("Current key: %s; leave blank to keep it.", maskKey(current)))
				} else {
					parts = append(parts, fmt.Sprintf("Enter your %s API key.", providerLabel(cfg.Provider)))
				}

				if envName := providerEnvVar(cfg.Provider); envName != "" && os.Getenv(envName) != "" {
					parts = append(parts, fmt.Sprintf("%s is set and takes precedence over the config file until it is unset.", envName))
				}

				return strings.Join(parts, " ")
			}, &cfg.Provider).
			Placeholder("paste new key or press enter to keep existing").
			EchoMode(huh.EchoModePassword).
			Value(&cfg.APIKey),
	).WithHideFunc(hide)
}

// providerEnvVar returns the environment variable name for the provider's API key.
func providerEnvVar(provider string) string {
	keys, _ := chat.CredentialKeysFor(gochat.Provider(provider))

	return keys.FallbackEnv
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
type AIInitialiser struct{}

// NewAIInitialiser creates a new AIInitialiser. Its asset bundle is
// registered from init() via setup.RegisterAssets, applied for enabled
// features at root construction.
func NewAIInitialiser(_ *props.Props) *AIInitialiser {
	return &AIInitialiser{}
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

	keys, needs := chat.CredentialKeysFor(gochat.Provider(provider))
	if !needs {
		return true
	}

	for _, key := range []string{keys.Env, keys.Keychain, keys.Literal} {
		if cfg.GetString(key) != "" {
			return true
		}
	}

	return false
}

// Configure runs the interactive AI configuration forms and writes the
// results through the editor.
func (a *AIInitialiser) Configure(ctx context.Context, p *props.Props, cfg setup.Editor) error {
	aiCfg, err := runAIForms(ctx, p, cfg.View(), linkedProviders(gochat.RegisteredProviders))
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
func RunAIInit(ctx context.Context, p *props.Props, dir string) error {
	editor, _, err := setup.OpenConfigEditor(ctx, p, dir, false)
	if err != nil {
		return err
	}

	aiCfg, err := runAIForms(ctx, p, editor.View(), linkedProviders(gochat.RegisteredProviders))
	if err != nil {
		return err
	}

	return writeAIConfig(ctx, editor, p.Tool.Name, aiCfg)
}

// runAIForms runs the wizard on the invocation's streams and settles what the
// form could not: a blank env var name takes the provider's well-known one, a
// blank key keeps the existing one, and a literal is refused under CI even if
// the selector somehow offered it.
func runAIForms(ctx context.Context, p *props.Props, existing config.Reader, linked func(gochat.Provider) bool) (*AIConfig, error) {
	aiCfg := &AIConfig{}

	// Pre-populate provider from existing config.
	if provider := existing.GetString(chat.ConfigKeyAIProvider); isValidProvider(provider) {
		aiCfg.Provider = provider
	}

	if err := setup.RunForm(ctx, p, aiForm(ctx, p, aiCfg, existing, linked)); err != nil {
		return nil, errors.Newf("AI configuration form cancelled: %w", err)
	}

	return finaliseAIConfig(aiCfg, existing, linked)
}

// finaliseAIConfig applies the rules that hold whatever the form did.
func finaliseAIConfig(aiCfg *AIConfig, existing config.Reader, linked func(gochat.Provider) bool) (*AIConfig, error) {
	provider := gochat.Provider(aiCfg.Provider)

	if !linked(provider) {
		err := errors.Wrapf(ErrProviderNotLinked, "%s", provider)
		if module, ok := chat.ProviderModule(provider); ok {
			err = errors.WithHintf(err, "Add to the tool's main package:\n\nimport _ %q", module)
		}

		return nil, err
	}

	// A local CLI or bedrock authenticates on its own: the provider is the
	// whole answer.
	if !chat.NeedsCredential(provider) {
		aiCfg.StorageMode, aiCfg.EnvVarName, aiCfg.APIKey = "", "", ""

		return aiCfg, nil
	}

	if err := credentials.RefuseLiteralUnderCI(aiCfg.StorageMode); err != nil {
		return nil, err
	}

	aiCfg.ExistingKey = existing.GetString(providerConfigKey(aiCfg.Provider))

	if aiCfg.StorageMode == credentials.ModeEnvVar {
		if aiCfg.EnvVarName == "" {
			aiCfg.EnvVarName = providerEnvVar(aiCfg.Provider)
		}

		aiCfg.APIKey = ""

		return aiCfg, nil
	}

	// Blank submission in literal or keychain mode preserves the existing key.
	if aiCfg.APIKey == "" && aiCfg.ExistingKey != "" {
		aiCfg.APIKey = aiCfg.ExistingKey
	}

	return aiCfg, nil
}

// providerConfigKey returns the config key for the provider's literal API
// key, empty for a provider that carries no GTB credential.
func providerConfigKey(provider string) string {
	keys, _ := chat.CredentialKeysFor(gochat.Provider(provider))

	return keys.Literal
}

// providerEnvConfigKey returns the config key that records the env var NAME
// (not value) for the provider's API key when stored in
// [credentials.ModeEnvVar].
func providerEnvConfigKey(provider string) string {
	keys, _ := chat.CredentialKeysFor(gochat.Provider(provider))

	return keys.Env
}

// providerKeychainConfigKey returns the config key that records the
// "<service>/<account>" reference for the provider's API key when stored in
// [credentials.ModeKeychain].
func providerKeychainConfigKey(provider string) string {
	keys, _ := chat.CredentialKeysFor(gochat.Provider(provider))

	return keys.Keychain
}

// providerKeychainAccount returns the keychain account name under which the
// provider's API key is stored: the credential root, so the keychain UI
// labels entries "<tool>/anthropic.api". Changing it would strand existing
// keychain entries on user machines.
func providerKeychainAccount(provider string) string {
	keys, _ := chat.CredentialKeysFor(gochat.Provider(provider))

	return keys.Root
}

// isValidProvider reports whether a known module registers the provider.
func isValidProvider(provider string) bool {
	_, ok := chat.ProviderModule(gochat.Provider(provider))

	return ok
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

	keys, needs := chat.CredentialKeysFor(gochat.Provider(provider))
	if !needs {
		return true
	}

	for _, key := range []string{keys.Env, keys.Keychain, keys.Literal} {
		if cfg.GetString(key) != "" {
			return true
		}
	}

	return false
}

// NewCmdInitAI creates the `init ai` subcommand.
func NewCmdInitAI(p *props.Props) *cobra.Command {
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

			if err := RunAIInit(cmd.Context(), p, dir); err != nil {
				return errors.Wrap(err, "failed to configure AI")
			}

			p.Logger.Info("AI configuration saved successfully")

			return nil
		},
	}

	cmd.Flags().String("dir", setup.GetDefaultConfigDir(p.FS, p.Tool.Name), "directory containing the config file")

	return cmd
}
