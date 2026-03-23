package root

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/njayp/ophis"

	"github.com/phpboyscout/gtb/pkg/cmd/docs"
	"github.com/phpboyscout/gtb/pkg/cmd/initialise"
	"github.com/phpboyscout/gtb/pkg/cmd/update"
	"github.com/phpboyscout/gtb/pkg/cmd/version"
	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/phpboyscout/gtb/pkg/errorhandling"
	p "github.com/phpboyscout/gtb/pkg/props"
	"github.com/phpboyscout/gtb/pkg/setup"

	"github.com/charmbracelet/huh"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/gtb/pkg/logger"
)

// ErrUpdateComplete is returned by PersistentPreRunE when a self-update
// has completed successfully. The Execute wrapper handles this by exiting
// cleanly without logging an error.
var ErrUpdateComplete = errors.New("update complete — restart required")

// rootState holds per-command mutable state, avoiding package-level variables
// that would be shared across multiple NewCmdRoot calls in the same process.
type rootState struct {
	cfgPaths            []string
	redirectingToUpdate bool
	formCreator         func(*bool) *huh.Form
}

func newRootState() *rootState {
	return &rootState{
		formCreator: createUpdatePromptForm,
	}
}

// FlagValues holds the command-line flag values extracted from cobra command.
type FlagValues struct {
	CI    bool
	Debug bool
}

// ConfigLoadOptions holds the options needed for loading configuration.
type ConfigLoadOptions struct {
	CfgPaths    []string
	ConfigPaths []string
	Props       *p.Props
	AllowEmpty  bool
}

// extractFlags extracts and validates command-line flags from cobra command.
func extractFlags(cmd *cobra.Command) (*FlagValues, error) {
	ci, err := cmd.Flags().GetBool("ci")
	if err != nil {
		return nil, errors.Wrap(err, "failed to get ci flag")
	}

	debug, err := cmd.Flags().GetBool("debug")
	if err != nil {
		return nil, errors.Wrap(err, "failed to get debug flag")
	}

	return &FlagValues{
		CI:    ci,
		Debug: debug,
	}, nil
}

// loadAndMergeConfig loads the main configuration and merges it with embedded config if present.
func loadAndMergeConfig(opts ConfigLoadOptions) (config.Containable, error) {
	// Load main configuration
	cfg, err := config.Load(opts.CfgPaths, opts.Props.FS, opts.Props.Logger, opts.AllowEmpty)
	if err != nil {
		if errors.Is(err, config.ErrNoFilesFound) && opts.AllowEmpty {
			opts.Props.Logger.Debug("No config file found, loading default configuration")
			cfg = config.NewReaderContainer(opts.Props.Logger, "yaml", bytes.NewReader(setup.DefaultConfig))
		} else {
			return nil, errors.Wrap(err, "failed to load config")
		}
	} else if cfg.GetViper().ConfigFileUsed() == "" && len(setup.DefaultConfig) > 0 {
		opts.Props.Logger.Debug("No config file found (empty allowed), loading default configuration")
		cfg = config.NewReaderContainer(opts.Props.Logger, "yaml", bytes.NewReader(setup.DefaultConfig))
	}

	// If embedded config paths are provided, load and merge them
	mergedCfg, err := mergeEmbeddedConfigs(opts)
	if err != nil {
		return nil, err
	}

	if mergedCfg != nil {
		// Use MergeConfig with JSON for a deep merge (main config (cfg) overrides embedded (mergedCfg))
		err = mergedCfg.GetViper().MergeConfig(strings.NewReader(cfg.ToJSON()))
		if err != nil {
			return nil, errors.Wrap(err, "failed to merge embedded config")
		}

		return mergedCfg, nil
	}

	return cfg, nil
}

// mergeEmbeddedConfigs loads and merges all found embedded configurations.
// It leverages the Assets layer's built-in merging for structured config files.
func mergeEmbeddedConfigs(opts ConfigLoadOptions) (config.Containable, error) {
	if len(opts.ConfigPaths) == 0 || opts.Props.Assets == nil {
		return nil, nil
	}

	return config.LoadEmbed(opts.ConfigPaths, opts.Props.Assets, opts.Props.Logger)
}

// configureLogging sets up logging based on debug flag and config values.
func configureLogging(props *p.Props, flags *FlagValues, cfg config.Containable, mcpLogLevel *slog.LevelVar) {
	// Apply debug flag first
	if flags.Debug {
		props.Logger.SetLevel(logger.DebugLevel)
		mcpLogLevel.Set(slog.LevelDebug)
	} else if level, err := logger.ParseLevel(cfg.GetString("log.level")); err == nil {
		// Apply config-based log level if debug flag is not set
		props.Logger.SetLevel(level)
		mcpLogLevel.Set(mapLogLevel(level))
	}

	// Apply log format from config
	switch cfg.GetString("log.format") {
	case "json":
		props.Logger.SetFormatter(logger.JSONFormatter)
	case "logfmt":
		props.Logger.SetFormatter(logger.LogfmtFormatter)
	}
}

func mapLogLevel(level logger.Level) slog.Level {
	switch level {
	case logger.DebugLevel:
		return slog.LevelDebug
	case logger.InfoLevel:
		return slog.LevelInfo
	case logger.WarnLevel:
		return slog.LevelWarn
	case logger.FatalLevel, logger.ErrorLevel:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// UpdateCheckResult holds the result of checking for updates.
type UpdateCheckResult struct {
	HasUpdated bool
	ShouldExit bool
	Error      error
}

// checkForUpdates handles the version checking and update prompting logic.
func checkForUpdates(ctx context.Context, cmd *cobra.Command, props *p.Props, flags *FlagValues, state *rootState) *UpdateCheckResult {
	result := &UpdateCheckResult{}

	if shouldSkipUpdateCheck(props, cmd, flags, state) {
		return result
	}

	props.Logger.Debug("time since last update check", "duration", setup.GetTimeSinceLast(props.FS, props.Tool.Name, setup.CheckedKey))

	selfUpdater, err := setup.NewUpdater(props, "", false)
	if err != nil {
		props.Logger.Error("failed to create updater", "error", err)

		return result
	}

	props.Logger.Info("Checking for latest version")

	isLatestVersion, message, err := selfUpdater.IsLatestVersion(ctx)
	if err != nil {
		props.Logger.Error("failed to check for latest version", "error", err)

		return result
	}

	props.Logger.Debug("Version check results", "version", props.Version.GetVersion(), "latest", isLatestVersion, "message", message)

	if !isLatestVersion {
		handleOutdatedVersion(ctx, props, message, result, state)
	} else {
		props.Logger.Info(message)
	}

	// Set last checked time
	if err = setup.SetTimeSinceLast(props.FS, props.Tool.Name, setup.CheckedKey); err != nil {
		props.Logger.Warn("unable to set last checked time", "error", err)
	}

	return result
}

func shouldSkipUpdateCheck(props *p.Props, cmd *cobra.Command, flags *FlagValues, state *rootState) bool {
	// Skip update checks in various conditions
	if props.Tool.IsDisabled(p.UpdateCmd) ||
		(props.Version != nil && props.Version.IsDevelopment()) ||
		state.redirectingToUpdate ||
		flags.CI ||
		props.Config.GetBool("ci") {
		return true
	}

	return setup.SkipUpdateCheck(props.FS, props.Tool.Name, cmd)
}

func createUpdatePromptForm(runUpdate *bool) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Do you want to run the update now?").
				Description("using an out of date version may result in incorrect functionality or configuration").
				Affirmative("Yes!").
				Negative("No.").
				Value(runUpdate),
		))
}

// OutdatedVersionOption configures handleOutdatedVersion behavior.
type OutdatedVersionOption func(*outdatedVersionConfig)

type outdatedVersionConfig struct {
	formCreator func(*bool) *huh.Form
}

// WithForm allows providing a custom form creator for testing.
func WithForm(formCreator func(*bool) *huh.Form) OutdatedVersionOption {
	return func(cfg *outdatedVersionConfig) {
		cfg.formCreator = formCreator
	}
}

func handleOutdatedVersion(ctx context.Context, props *p.Props, message string, result *UpdateCheckResult, state *rootState, opts ...OutdatedVersionOption) {
	props.Logger.Warn(message)

	// Apply options
	cfg := &outdatedVersionConfig{
		formCreator: state.formCreator,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	var runUpdate = true

	form := cfg.formCreator(&runUpdate)
	// Allow nil form for testing (form creator can set the value and return nil)
	if form != nil {
		_ = form.Run()
	}

	if runUpdate {
		state.redirectingToUpdate = true

		if err := update.Update(ctx, props, "", false); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				result.Error = errors.WithHint(
					errors.New("update timed out"),
					"Check your internet connection or try again later.")

				return
			}

			result.Error = err

			return
		}

		props.Logger.Warnf("update complete please run command again to use the updated version")

		result.HasUpdated = true
		result.ShouldExit = true
	} else {
		props.Logger.Warnf("Continuing with an out of date version, please run '%s update' ASAP", props.Tool.Name)
	}
}

func NewCmdRoot(props *p.Props, subcommands ...*cobra.Command) *cobra.Command {
	return NewCmdRootWithConfig(props, []string{}, subcommands...)
}

// NewCmdRootWithConfig creates the root command for the CLI application.
// It accepts additional configuration file paths to be considered during initialization.
func NewCmdRootWithConfig(props *p.Props, configPaths []string, subcommands ...*cobra.Command) *cobra.Command {
	// Set the helper and logger for the error handling package
	if props.ErrorHandler == nil {
		props.ErrorHandler = errorhandling.New(props.Logger, props.Tool.Help)
	}

	state := newRootState()

	// mcpLogLevel is used to control the log level of the MCP server dynamically
	mcpLogLevel := &slog.LevelVar{}

	var rootCmd = &cobra.Command{
		Use:               props.Tool.Name,
		Short:             props.Tool.Summary,
		Long:              props.Tool.Description,
		PersistentPreRunE: newRootPreRunE(props, configPaths, mcpLogLevel, state),
	}

	setupRootFlags(rootCmd, props, state)
	registerFeatureCommands(rootCmd, props, mcpLogLevel)

	for _, subcommand := range subcommands {
		rootCmd.AddCommand(subcommand)
	}

	return rootCmd
}

func newRootPreRunE(props *p.Props, configPaths []string, mcpLogLevel *slog.LevelVar, state *rootState) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		// Extract and validate flags
		flags, err := extractFlags(cmd)
		if err != nil {
			return errors.Wrap(err, "failed to read command flags")
		}

		// Skip config loading for init command but still configure logging
		if cmd.Use == "init" {
			if flags.Debug {
				props.Logger.SetLevel(logger.DebugLevel)
				mcpLogLevel.Set(slog.LevelDebug)
			}

			return nil
		}

		// Load and merge configuration
		allowEmpty := props.Tool.IsDisabled(p.InitCmd)
		if allowEmpty {
			configPaths = append(configPaths, "assets/init/config.yaml")
		}

		cfg, err := loadAndMergeConfig(ConfigLoadOptions{
			CfgPaths:    state.cfgPaths,
			ConfigPaths: configPaths,
			Props:       props,
			AllowEmpty:  allowEmpty,
		})
		if err != nil {
			return errors.Wrap(err, "failed to load configuration")
		}

		// Set config in props
		props.Config = cfg

		// Validate config for common misconfigurations
		validateConfig(cfg, props.Logger)

		// Configure logging based on flags and config
		configureLogging(props, flags, cfg, mcpLogLevel)

		// Check for updates
		if props.Tool.IsDisabled(p.UpdateCmd) {
			return nil
		}

		updateResult := checkForUpdates(cmd.Context(), cmd, props, flags, state)
		if updateResult.Error != nil {
			return updateResult.Error
		}

		if updateResult.ShouldExit {
			return ErrUpdateComplete
		}

		return nil
	}
}

func setupRootFlags(rootCmd *cobra.Command, props *p.Props, state *rootState) {
	defaultConfigPaths := []string{
		filepath.Join(setup.GetDefaultConfigDir(props.FS, props.Tool.Name), setup.DefaultConfigFilename),
		fmt.Sprintf("%s%s", string(os.PathSeparator), filepath.Join("etc", props.Tool.Name, setup.DefaultConfigFilename)),
	}

	rootCmd.PersistentFlags().StringArrayVar(&state.cfgPaths, "config", defaultConfigPaths, "config files to use")
	rootCmd.PersistentFlags().Bool("debug", false, "forces debug log output")

	rootCmd.PersistentFlags().Bool("ci", false, "flag to indicate the tools is running in a CI environment")
}

func registerFeatureCommands(rootCmd *cobra.Command, props *p.Props, mcpLogLevel *slog.LevelVar) {
	rootCmd.AddCommand(version.NewCmdVersion(props))

	if props.Tool.IsEnabled(p.UpdateCmd) {
		rootCmd.AddCommand(update.NewCmdUpdate(props))
	}

	if props.Tool.IsEnabled(p.InitCmd) {
		rootCmd.AddCommand(initialise.NewCmdInit(props))
	}

	if props.Tool.IsEnabled(p.McpCmd) {
		rootCmd.AddCommand(ophis.Command(&ophis.Config{
			SloggerOptions: &slog.HandlerOptions{
				Level: mcpLogLevel,
			},
		}))
	}

	if props.Tool.IsEnabled(p.DocsCmd) {
		if docsCmd := docs.NewCmdDocs(props); docsCmd != nil {
			rootCmd.AddCommand(docsCmd)
		}
	}
}

// validateConfig warns about common misconfigurations.
func validateConfig(cfg config.Containable, l logger.Logger) {
	emptySetKeys := []string{
		"github.token",
		"anthropic.api.key",
		"openai.api.key",
		"gemini.api.key",
	}

	for _, key := range emptySetKeys {
		if cfg.IsSet(key) && cfg.GetString(key) == "" {
			l.Warn(key + " is set but empty — operations using this key will fail")
		}
	}
}
