package ai

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/phpboyscout/gtb/pkg/chat"
	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/phpboyscout/gtb/pkg/setup"
)

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
}

// FormOption configures the AI init form for testability.
type FormOption func(*formConfig)

type formConfig struct {
	providerFormCreator func(*AIConfig) *huh.Form
	keyFormCreator      func(*AIConfig) *huh.Form
}

// WithAIForm allows injecting custom form creators for testing.
func WithAIForm(creator func(*AIConfig) []*huh.Form) FormOption {
	return func(c *formConfig) {
		c.providerFormCreator = func(cfg *AIConfig) *huh.Form {
			forms := creator(cfg)
			if len(forms) > 0 {
				return forms[0]
			}

			return nil
		}
		c.keyFormCreator = func(cfg *AIConfig) *huh.Form {
			forms := creator(cfg)
			if len(forms) > 1 {
				return forms[1]
			}

			return nil
		}
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

	keyPath := providerConfigKey(aiCfg.Provider)
	if keyPath != "" && aiCfg.APIKey != "" {
		cfg.Set(keyPath, aiCfg.APIKey)
	}

	return nil
}

// RunAIInit executes the AI configuration form and writes the results to the config file.
func RunAIInit(p *props.Props, dir string, opts ...FormOption) error {
	targetFile := filepath.Join(dir, setup.DefaultConfigFilename)

	existingCfg, _ := config.LoadFilesContainer(nil, p.FS, targetFile)
	if existingCfg == nil {
		existingCfg = config.NewContainerFromViper(nil, viper.New())
	}

	aiCfg, err := runAIForms(existingCfg, opts...)
	if err != nil {
		return err
	}

	return writeAIConfig(p, dir, aiCfg)
}

// runAIForms runs the two-stage AI configuration forms and returns the result.
func runAIForms(existingCfg config.Containable, opts ...FormOption) (*AIConfig, error) {
	fCfg := &formConfig{
		providerFormCreator: defaultProviderForm,
		keyFormCreator:      defaultKeyForm,
	}

	for _, opt := range opts {
		opt(fCfg)
	}

	aiCfg := &AIConfig{}

	// Pre-populate provider from existing config
	provider := existingCfg.GetString(chat.ConfigKeyAIProvider)
	if isValidProvider(provider) {
		aiCfg.Provider = provider
	}

	// Stage 1: Provider selection
	if providerForm := fCfg.providerFormCreator(aiCfg); providerForm != nil {
		if err := providerForm.Run(); err != nil {
			return nil, errors.Newf("AI configuration form cancelled: %w", err)
		}
	}

	// Pre-populate existing key for the selected provider so the key form can show a hint
	aiCfg.ExistingKey = existingCfg.GetString(providerConfigKey(aiCfg.Provider))

	// Stage 2: API key input (built lazily so it can check provider-specific env vars)
	if keyForm := fCfg.keyFormCreator(aiCfg); keyForm != nil {
		if err := keyForm.Run(); err != nil {
			return nil, errors.Newf("AI configuration form cancelled: %w", err)
		}
	}

	// If user submitted blank, keep the existing key
	if aiCfg.APIKey == "" && aiCfg.ExistingKey != "" {
		aiCfg.APIKey = aiCfg.ExistingKey
	}

	return aiCfg, nil
}

// providerConfigKey returns the viper config key for the provider's API key.
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

func writeAIConfig(p *props.Props, dir string, aiCfg *AIConfig) error {
	targetFile := filepath.Join(dir, setup.DefaultConfigFilename)

	cfg := viper.New()
	cfg.SetFs(p.FS)
	cfg.SetConfigType("yaml")

	// Load existing config if present
	if data, aferoErr := afero.ReadFile(p.FS, targetFile); aferoErr == nil {
		if readErr := cfg.ReadConfig(bytes.NewReader(data)); readErr != nil {
			return errors.Newf("failed to read existing config: %w", readErr)
		}
	}

	// Build the config map for only the selected provider
	configMap := map[string]any{
		"ai": map[string]any{
			"provider": aiCfg.Provider,
		},
	}

	// Set the API key under the correct provider path
	keyPath := providerConfigKey(aiCfg.Provider)
	if keyPath != "" && aiCfg.APIKey != "" {
		cfg.Set(keyPath, aiCfg.APIKey)
	}

	if err := cfg.MergeConfigMap(configMap); err != nil {
		return errors.Newf("failed to merge AI config: %w", err)
	}

	// Ensure directory exists
	const defaultDirPerm = 0o755

	if err := p.FS.MkdirAll(dir, defaultDirPerm); err != nil {
		return errors.Newf("failed to create config directory: %w", err)
	}

	return cfg.WriteConfigAs(targetFile)
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
