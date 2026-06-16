package root

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/njayp/ophis"

	cmdchangelog "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/changelog"
	cmdconfig "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/docs"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/doctor"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/initialise"
	cmdtelemetry "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/telemetry"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/update"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/version"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/errorhandling"
	"gitlab.com/phpboyscout/go-tool-base/pkg/output"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// Compile-time check: *telemetry.Collector implements props.TelemetryCollector.
var _ p.TelemetryCollector = (*telemetry.Collector)(nil)

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

// configOpts builds the common ContainerOption slice from Props.
func configOpts(props *p.Props) []config.ContainerOption {
	opts := []config.ContainerOption{config.WithLogger(props.Logger)}
	if props.Tool.EnvPrefix != "" {
		opts = append(opts, config.WithEnvPrefix(props.Tool.EnvPrefix))
	}

	return opts
}

// loadAndMergeConfig loads the main configuration and merges it with embedded config if present.
func loadAndMergeConfig(opts ConfigLoadOptions) (config.Containable, error) {
	cfgOpts := configOpts(opts.Props)

	// Load main configuration
	cfg, err := config.Load(opts.CfgPaths, opts.Props.FS, opts.AllowEmpty, cfgOpts...)
	if err != nil {
		if errors.Is(err, config.ErrNoFilesFound) && opts.AllowEmpty {
			opts.Props.Logger.Debug("No config file found, loading default configuration")
			cfg = config.NewReaderContainer(opts.Props.FS, append(cfgOpts,
				config.WithConfigFormat("yaml"),
				config.WithConfigReaders(bytes.NewReader(setup.DefaultConfig)),
			)...)
		} else {
			return nil, errors.Wrap(err, "failed to load config")
		}
	} else if cfg.GetViper().ConfigFileUsed() == "" && len(setup.DefaultConfig) > 0 {
		opts.Props.Logger.Debug("No config file found (empty allowed), loading default configuration")
		cfg = config.NewReaderContainer(opts.Props.FS, append(cfgOpts,
			config.WithConfigFormat("yaml"),
			config.WithConfigReaders(bytes.NewReader(setup.DefaultConfig)),
		)...)
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

	return config.LoadEmbed(opts.ConfigPaths, opts.Props.Assets, configOpts(opts.Props)...)
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

	policy := p.ResolveUpdatePolicy(props.Tool.UpdatePolicy, props.Config.GetString("update.policy"))

	// Persistent out-of-date reminder from the cached latest version: emitted
	// every invocation (even when the network check is throttled below), so a
	// user who declined an update — or runs a disabled-policy tool — keeps
	// being reminded. Suppressed under --ci.
	if !flags.CI && !props.Config.GetBool("ci") {
		warnIfBehindCached(props)
	}

	if shouldSkipUpdateCheck(props, cmd, flags, state) {
		return result
	}

	props.Logger.Debug("time since last update check", "duration", setup.GetTimeSinceLast(props.FS, props.Tool.Name, setup.CheckedKey))

	selfUpdater, err := setup.NewUpdater(ctx, props, "", false)
	if err != nil {
		props.Logger.Error("failed to create updater", "error", err)

		return result
	}

	var (
		isLatestVersion bool
		message         string
	)

	spinErr := output.Spin(ctx, "Checking for latest version", func(ctx context.Context) error {
		var versionErr error

		isLatestVersion, message, versionErr = selfUpdater.IsLatestVersion(ctx)

		return versionErr
	})
	if spinErr != nil {
		props.Logger.Error("failed to check for latest version", "error", spinErr)

		return result
	}

	props.Logger.Debug("Version check results", "version", props.Version.GetVersion(), "latest", isLatestVersion, "message", message)

	// Record the check time. When behind, the latest version is stored in the
	// last_checked marker body so warnIfBehindCached can remind on later runs
	// without a network call; when up to date, the body is cleared.
	recordCheckedVersion(ctx, props, selfUpdater, isLatestVersion)

	if !isLatestVersion {
		handleOutdatedVersion(ctx, props, message, result, state, policy)
	} else {
		props.Logger.Info(message)
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

	interval := setup.ResolveCheckInterval(props.Tool.UpdateCheckInterval, props.Config.GetString("update.check_interval"))

	return setup.SkipUpdateCheck(props.FS, props.Tool.Name, cmd, interval)
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

func handleOutdatedVersion(ctx context.Context, props *p.Props, message string, result *UpdateCheckResult, state *rootState, policy p.UpdatePolicy, opts ...OutdatedVersionOption) {
	props.Logger.Warn(message)

	// disabled: log that an update is available and carry on — no prompt, no
	// block. The persistent cached-version WARN keeps reminding on later runs.
	if policy == p.UpdatePolicyDisabled {
		props.Logger.Warnf("a newer version is available — run '%s update' to upgrade when ready", props.Tool.Name)

		return
	}

	// Apply options
	cfg := &outdatedVersionConfig{
		formCreator: state.formCreator,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Default to declining: without a usable TTY (cron, CI, piped stdin) or
	// on an aborted/timed-out prompt, form.Run returns an error and runUpdate
	// must stay false. Defaulting to true here would silently self-update
	// without consent.
	var runUpdate = false

	form := cfg.formCreator(&runUpdate)
	// Allow nil form for testing (form creator can set the value and return nil)
	if form != nil {
		if err := form.Run(); err != nil {
			runUpdate = false

			props.Logger.Debugf("update prompt unavailable (%v); declining update", err)
		}
	}

	if runUpdate {
		performUpdate(ctx, props, result, state)

		return
	}

	// Declined, or no usable prompt. enabled blocks: a required update that was
	// not performed is a hard, non-zero failure (never a masked continue).
	// prompt continues with a warning.
	if policy == p.UpdatePolicyEnabled {
		result.Error = errors.WithHintf(
			errors.Newf("an update to %s is required before continuing", props.Tool.Name),
			"Run '%s update' to upgrade.", props.Tool.Name)

		return
	}

	props.Logger.Warnf("Continuing with an out of date version, please run '%s update' ASAP", props.Tool.Name)
}

// performUpdate runs the self-update and records the outcome on result: a
// successful update sets HasUpdated/ShouldExit (the caller exits 0 and asks the
// user to re-run); a failed update sets result.Error so the process exits
// non-zero rather than masking the failure.
func performUpdate(ctx context.Context, props *p.Props, result *UpdateCheckResult, state *rootState) {
	state.redirectingToUpdate = true

	if _, err := update.Update(ctx, props, "", false); err != nil {
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
}

// warnIfBehindCached emits a single out-of-date warning when the cached latest
// version (from a prior check) is newer than the running binary. It is the
// persistent reminder for users who declined an update or run a disabled-policy
// tool — and costs no network call. Skipped for development builds.
func warnIfBehindCached(props *p.Props) {
	if props.Version == nil || props.Version.IsDevelopment() {
		return
	}

	cached := setup.GetCheckedVersion(props.FS, props.Tool.Name)
	if cached == "" {
		return
	}

	if ver.CompareVersions(props.Version.GetVersion(), cached) < 0 {
		props.Logger.Warnf("a newer %s is available: %s — run '%s update' to upgrade",
			props.Tool.Name, cached, props.Tool.Name)
	}
}

// recordCheckedVersion stamps the last-checked marker. When the binary is out
// of date it stores the latest release version in the marker body (so
// warnIfBehindCached can remind on later runs without a network round-trip);
// when up to date it clears the body. Best-effort: failures are non-fatal.
func recordCheckedVersion(ctx context.Context, props *p.Props, updater *setup.SelfUpdater, isLatest bool) {
	version := ""

	if !isLatest {
		if latest, err := updater.GetLatestVersionString(ctx); err == nil {
			version = latest
		}
	}

	if err := setup.SetCheckedVersion(props.FS, props.Tool.Name, version); err != nil {
		props.Logger.Warn("unable to set last checked time", "error", err)
	}
}

// NewCmdRoot creates the root command with Props wiring and optional subcommands.
func NewCmdRoot(props *p.Props, subcommands ...*setup.Command) *setup.Command {
	return NewCmdRootWithOptions(props, WithSubcommands(subcommands...))
}

// NewCmdRootWithConfig creates the root command for the CLI application.
// It accepts additional configuration file paths to be considered during initialization.
func NewCmdRootWithConfig(props *p.Props, configPaths []string, subcommands ...*setup.Command) *setup.Command {
	return NewCmdRootWithOptions(props,
		WithConfigPaths(configPaths...),
		WithSubcommands(subcommands...),
	)
}

// NewCmdRootWithOptions creates the root command, configured by the supplied
// [RootOption]s. This is the extensible constructor; [NewCmdRoot] and
// [NewCmdRootWithConfig] are thin wrappers over it. Use [WithBoundFlags] or
// [WithConventionBoundFlags] to wire CLI flags into the configuration
// precedence (flags > env > file > embedded > defaults).
func NewCmdRootWithOptions(props *p.Props, opts ...RootOption) *setup.Command {
	o := applyRootOptions(opts)

	// Run every parent PersistentPreRunE from root to leaf rather than only the
	// closest one. Without this, a downstream subcommand that defines its own
	// PersistentPreRunE silently shadows the root bootstrap (config load,
	// telemetry, update check) for that subtree. With it set, the framework
	// bootstrap always runs first and a child hook runs after it. Cobra exposes
	// this as a package-global; only the root command defines a persistent
	// pre-run hook in GTB's own tree, so root→leaf traversal is otherwise a
	// no-op for the framework. See spec 2026-06-12-bootstrap-prerun-traversal.
	cobra.EnableTraverseRunHooks = true

	// Set the helper and logger for the error handling package
	if props.ErrorHandler == nil {
		props.ErrorHandler = errorhandling.New(props.Logger, props.Tool.Help)
	}

	// Uphold the documented Props.Collector invariant ("always non-nil"). The
	// real *telemetry.Collector is resolved later in the root PersistentPreRunE,
	// but the init and help paths return before that — and Props built as a
	// struct literal (tests, cmd/e2e) never set it. Default to a noop here so
	// every consumer can call props.Collector unconditionally.
	if props.Collector == nil {
		props.Collector = p.NoopCollector{}
	}

	state := newRootState()

	// mcpLogLevel is used to control the log level of the MCP server dynamically
	mcpLogLevel := &slog.LevelVar{}

	var rootCmd = &cobra.Command{
		Use:               props.Tool.Name,
		Short:             props.Tool.Summary,
		Long:              props.Tool.Description,
		PersistentPreRunE: newRootPreRunE(props, o.configPaths, mcpLogLevel, state, o.boundFlags),
	}

	setupRootFlags(rootCmd, props, state)

	// Register author-supplied bound flags on the root's persistent flag set so
	// cobra parses them; they are then bound onto the config container during
	// the pre-run (filtered by flag.Changed).
	for _, flag := range o.boundFlags {
		if flag != nil && rootCmd.PersistentFlags().Lookup(flag.Name) == nil {
			rootCmd.PersistentFlags().AddFlag(flag)
		}
	}

	wrapped := setup.Wrap("", rootCmd)
	registerFeatureCommands(wrapped, props, mcpLogLevel)

	wrapped.Register(o.subcommands...)

	// Once the full tree is assembled, warn (once, at debug) if any downstream
	// command defines its own PersistentPreRunE so authors understand it runs
	// AFTER the framework bootstrap rather than instead of it.
	logShadowingPreRunHooks(rootCmd, props.Logger)

	return wrapped
}

// logShadowingPreRunHooks emits a single debug log if any non-root command in
// the tree defines a PersistentPreRunE. EnableTraverseRunHooks guarantees the
// framework bootstrap (the root hook) still runs first, so this is purely an
// ordering note for authors rather than a warning about lost behaviour.
func logShadowingPreRunHooks(root *cobra.Command, l logger.Logger) {
	for _, cmd := range root.Commands() {
		if commandTreeHasPersistentPreRun(cmd) {
			l.Debug("a downstream command defines its own PersistentPreRunE; " +
				"it runs AFTER the framework bootstrap (config load, telemetry, update check), not instead of it")

			return
		}
	}
}

// commandTreeHasPersistentPreRun reports whether cmd or any of its descendants
// defines a PersistentPreRunE hook.
func commandTreeHasPersistentPreRun(cmd *cobra.Command) bool {
	if cmd.PersistentPreRunE != nil || cmd.PersistentPreRun != nil {
		return true
	}

	for _, child := range cmd.Commands() {
		if commandTreeHasPersistentPreRun(child) {
			return true
		}
	}

	return false
}

func newRootPreRunE(props *p.Props, configPaths []string, mcpLogLevel *slog.LevelVar, state *rootState, boundFlags map[string]*pflag.Flag) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		// Extract and validate flags
		flags, err := extractFlags(cmd)
		if err != nil {
			return errors.Wrap(err, "failed to read command flags")
		}

		// Skip config loading for the init command but still configure logging.
		// Identify it by the typed feature annotation (stamped by setup.Wrap),
		// not the fragile Use string: a string match misfires for any unrelated
		// command literally named "init" and breaks if Use carries an arg suffix.
		if setup.FeatureOf(cmd) == p.InitCmd {
			if flags.Debug {
				props.Logger.SetLevel(logger.DebugLevel)
				mcpLogLevel.Set(slog.LevelDebug)
			}

			return nil
		}

		// Load and merge configuration. Clone the captured slice per
		// invocation so the append below never accumulates across repeated
		// PersistentPreRunE runs and never mutates the caller's backing array.
		allowEmpty := props.Tool.IsDisabled(p.InitCmd)
		paths := slices.Clone(configPaths)

		if allowEmpty {
			paths = append(paths, "assets/init/config.yaml")
		}

		cfg, err := loadAndMergeConfig(ConfigLoadOptions{
			CfgPaths:    state.cfgPaths,
			ConfigPaths: paths,
			Props:       props,
			AllowEmpty:  allowEmpty,
		})
		if err != nil {
			return errors.Wrap(err, "failed to load configuration")
		}

		// Set config in props
		props.Config = cfg

		// Bind CLI flags into the configuration so the documented precedence
		// (flags > env > file > embedded > defaults) holds. Binding happens
		// after the file and env layers are established; viper's BindPFlag sits
		// above AutomaticEnv, so no custom precedence is needed. Only flags the
		// user explicitly changed are bound (handled in bindCommandFlags).
		bindCommandFlags(cmd, cfg, props.Logger, boundFlags)

		// Validate config for common misconfigurations
		validateConfig(cfg, props.Logger)

		// Configure logging based on flags and config
		configureLogging(props, flags, cfg, mcpLogLevel)

		// Prompt for telemetry consent if the feature is enabled but not yet configured
		promptTelemetryConsent(props, flags)

		// Build and wire telemetry collector
		props.Collector = buildTelemetryCollector(cmd.Context(), props)

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

// bindCommandFlags wires CLI flags into the configuration container so the
// documented precedence (flags > env > file > embedded > defaults) holds. It
// binds three sources, all filtered by flag.Changed:
//
//  1. the explicit author-supplied boundFlags map (config key → flag),
//  2. the special-cased built-ins --ci and --debug (folded through the same
//     binding path so config visibility is consistent — D3), and
//  3. the dispatched command's own local flags, mapped by the hyphen-to-dot
//     convention (--server-port → server.port) into the command-scoped config
//     view (D5).
//
// Only flags the user explicitly set are bound; a flag at its default never
// overrides config (viper's default-clobber footgun).
func bindCommandFlags(cmd *cobra.Command, cfg config.Containable, log logger.Logger, boundFlags map[string]*pflag.Flag) {
	// 1. Explicit author-supplied bindings (persistent/root flags).
	bindChangedFlags(cfg, boundFlags, log)

	// 2. Built-ins --ci / --debug, folded through the same binding path so
	//    Config.GetBool("ci"/"debug") reflects the flag at the documented
	//    precedence. --debug's immediate log-level effect is still applied
	//    separately in configureLogging.
	builtins := map[string]*pflag.Flag{}

	for key := range builtinBoundFlags {
		if f := cmd.Flags().Lookup(key); f != nil {
			builtins[key] = f
		}
	}

	bindChangedFlags(cfg, builtins, log)

	// 3. The dispatched command's own local flags, by convention. Inherited
	//    persistent flags are skipped here (the root binds its own); only the
	//    command's local flag set participates so e.g. `serve --port` overrides
	//    server.port for the serve command.
	convention := map[string]*pflag.Flag{}

	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		// Skip flags already explicitly bound or handled as built-ins to avoid
		// surprising convention mappings overriding an author's explicit choice.
		if _, ok := builtinBoundFlags[f.Name]; ok {
			return
		}

		convention[ConventionKey(f.Name)] = f
	})

	bindChangedFlags(cfg, convention, log)
}

// builtinBoundFlags is the set of built-in persistent flags folded through the
// binding path (D3). The map value is unused; only the key set matters.
var builtinBoundFlags = map[string]struct{}{
	"ci":    {},
	"debug": {},
}

func setupRootFlags(rootCmd *cobra.Command, props *p.Props, state *rootState) {
	defaultConfigPaths := []string{
		filepath.Join(setup.GetDefaultConfigDir(props.FS, props.Tool.Name), setup.DefaultConfigFilename),
		fmt.Sprintf("%s%s", string(os.PathSeparator), filepath.Join("etc", props.Tool.Name, setup.DefaultConfigFilename)),
	}

	rootCmd.PersistentFlags().StringArrayVar(&state.cfgPaths, "config", defaultConfigPaths, "config files to use")
	rootCmd.PersistentFlags().Bool("debug", false, "forces debug log output")

	rootCmd.PersistentFlags().Bool("ci", false, "flag to indicate the tools is running in a CI environment")
	rootCmd.PersistentFlags().String("output", "text", "output format (text, json)")
}

// registerGlobalMiddlewareOnce registers the built-in global middleware and
// seals the registry, but only on the first call per process. The middleware
// registry is process-global, so a second NewCmdRoot reuses the already-sealed
// registry rather than panicking on re-registration after seal.
func registerGlobalMiddlewareOnce(props *p.Props) {
	if setup.IsSealed() {
		return
	}

	setup.RegisterGlobalMiddleware(
		setup.WithRecovery(props.Logger),
		setup.WithTiming(props.Logger),
		setup.WithTelemetry(props),
	)

	setup.Seal()
}

func registerFeatureCommands(rootCmd *setup.Command, props *p.Props, mcpLogLevel *slog.LevelVar) {
	registerGlobalMiddlewareOnce(props)

	rootCmd.Register(version.NewCmdVersion(props))

	if props.Tool.IsEnabled(p.UpdateCmd) {
		rootCmd.Register(update.NewCmdUpdate(props))
	}

	if props.Tool.IsEnabled(p.InitCmd) {
		rootCmd.Register(initialise.NewCmdInit(props))
	}

	if props.Tool.IsEnabled(p.McpCmd) {
		mcpCmd := ophis.Command(&ophis.Config{
			SloggerOptions: &slog.HandlerOptions{
				Level: mcpLogLevel,
			},
		})
		rootCmd.Register(setup.Wrap(p.McpCmd, mcpCmd))
	}

	if props.Tool.IsEnabled(p.DocsCmd) {
		if docsCmd := docs.NewCmdDocs(props); docsCmd != nil {
			rootCmd.Register(docsCmd)
		}
	}

	if props.Tool.IsEnabled(p.DoctorCmd) {
		rootCmd.Register(doctor.NewCmdDoctor(props))
	}

	if props.Tool.IsEnabled(p.ConfigCmd) {
		rootCmd.Register(cmdconfig.NewCmdConfig(props))
	}

	if props.Tool.IsEnabled(p.TelemetryCmd) {
		rootCmd.Register(cmdtelemetry.NewCmdTelemetry(props))
	}

	if props.Tool.IsEnabled(p.ChangelogCmd) {
		rootCmd.Register(cmdchangelog.NewCmdChangelog(props))
	}
}

const telemetryFlushTimeout = 2 * time.Second

// promptTelemetryConsent shows a one-time opt-in prompt when TelemetryCmd is
// enabled but the user hasn't made a choice yet. Skips prompting in CI mode,
// when the TELEMETRY_ENABLED env var is set, or when telemetry.enabled is
// already present in config.
func promptTelemetryConsent(props *p.Props, flags *FlagValues) {
	if props.Tool.IsDisabled(p.TelemetryCmd) {
		return
	}

	// Tool author has force-enabled telemetry — no prompt, always on
	if props.Tool.Telemetry.ForceEnabled {
		return
	}

	// Already configured — no prompt needed
	if _, ok := os.LookupEnv("TELEMETRY_ENABLED"); ok {
		return
	}

	if props.Config.IsSet("telemetry.enabled") {
		return
	}

	// Non-interactive — skip silently
	if flags.CI || props.Config.GetBool("ci") {
		return
	}

	var optIn bool

	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Anonymous usage telemetry").
			Description(
				"Help improve " + props.Tool.Name + " by sending anonymous usage statistics.\n" +
					"No personally identifiable information is collected.\n" +
					"You can change this at any time with `" + props.Tool.Name + " telemetry enable/disable`.",
			).
			Value(&optIn),
	))

	if err := form.Run(); err != nil {
		props.Logger.Debug("telemetry consent prompt skipped", "error", err)

		return
	}

	props.Config.Set("telemetry.enabled", optIn)

	// Persist the choice so we don't prompt again
	v := props.Config.GetViper()
	if err := v.WriteConfig(); err != nil {
		// No config file yet — create a minimal one
		if cfgErr := ensureMinimalConfig(props, v, optIn); cfgErr != nil {
			props.Logger.Debug("failed to persist telemetry consent", "error", cfgErr)
		}
	}
}

// ensureMinimalConfig creates a minimal config file with just the telemetry
// consent value. Used when no config file exists (tools without InitCmd).
func ensureMinimalConfig(props *p.Props, v *viper.Viper, enabled bool) error {
	configDir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)
	if configDir == "" {
		return errors.New("unable to determine config directory")
	}

	configFile := filepath.Join(configDir, setup.DefaultConfigFilename)

	// Create the config dir lazily at first write — setup.GetDefaultConfigDir
	// is pure and no longer creates it as a side effect.
	const configDirPerm = 0o700
	if err := props.FS.MkdirAll(configDir, configDirPerm); err != nil {
		return errors.Wrap(err, "failed to create config directory")
	}

	fresh := viper.New()
	fresh.SetFs(props.FS)
	fresh.SetConfigFile(configFile)
	fresh.SetConfigType("yaml")
	fresh.Set("telemetry.enabled", enabled)

	v.SetConfigFile(configFile)

	return errors.Wrap(fresh.WriteConfigAs(configFile), "failed to write config")
}

// buildTelemetryCollector creates the appropriate telemetry collector based on
// feature flags, user config, environment variables, and tool-author settings.
// resolveVersionString returns the tool version, or "" when Props was built
// without a Version (the interface field is nilable on hand-constructed Props).
func resolveVersionString(props *p.Props) string {
	if props.Version == nil {
		return ""
	}

	return props.Version.GetVersion()
}

func buildTelemetryCollector(ctx context.Context, props *p.Props) *telemetry.Collector {
	dataDir := telemetry.ResolveDataDir(props)

	// Version is an interface and may be nil on a hand-constructed Props (the
	// scaffold always sets it, but downstream tools need not). Resolve it once
	// with a nil guard, mirroring shouldSkipUpdateCheck and the doctor command.
	version := resolveVersionString(props)

	if props.Tool.IsDisabled(p.TelemetryCmd) {
		return telemetry.NewCollector(telemetry.Config{}, telemetry.NewNoopBackend(),
			props.Tool.Name, version, nil, props.Logger, dataDir, p.DeliveryAtLeastOnce, false)
	}

	cfg := telemetry.Config{
		Enabled:   props.Config.GetBool("telemetry.enabled"),
		LocalOnly: props.Config.GetBool("telemetry.local_only"),
	}

	// Env var override (non-interactive bypass — tool-name-agnostic)
	if val, ok := os.LookupEnv("TELEMETRY_ENABLED"); ok {
		cfg.Enabled, _ = strconv.ParseBool(val)
	}

	// Tool author has force-enabled telemetry — always on, cannot be overridden
	// by config or environment variable. Applied last to ensure it takes precedence.
	if props.Tool.Telemetry.ForceEnabled {
		cfg.Enabled = true
	}

	if val, ok := os.LookupEnv("TELEMETRY_LOCAL"); ok {
		cfg.LocalOnly, _ = strconv.ParseBool(val)
	}

	if !cfg.Enabled {
		return telemetry.NewCollector(telemetry.Config{}, telemetry.NewNoopBackend(),
			props.Tool.Name, version, nil, props.Logger, dataDir, p.DeliveryAtLeastOnce, false)
	}

	deliveryMode := props.Tool.Telemetry.DeliveryMode
	if deliveryMode == "" {
		deliveryMode = p.DeliveryAtLeastOnce
	}

	backend, backendInfo := selectTelemetryBackend(ctx, props, cfg, dataDir)

	collector := telemetry.NewCollector(cfg, backend, props.Tool.Name, version,
		props.Tool.Telemetry.Metadata, props.Logger, dataDir, deliveryMode, props.Tool.Telemetry.ExtendedCollection)
	collector.SetBackendInfo(backendInfo)

	return collector
}

func selectTelemetryBackend(ctx context.Context, props *p.Props, cfg telemetry.Config, dataDir string) (telemetry.Backend, string) {
	switch {
	case props.Tool.Telemetry.Backend != nil:
		raw := props.Tool.Telemetry.Backend(props)

		b, ok := raw.(telemetry.Backend)
		if !ok {
			props.Logger.Warn("TelemetryConfig.Backend did not return a telemetry.Backend; falling back to noop")

			return telemetry.NewNoopBackend(), "noop (invalid custom backend)"
		}

		return b, "custom"
	case cfg.LocalOnly:
		return telemetry.NewFileBackend(filepath.Join(dataDir, "telemetry.log")), "file (" + filepath.Join(dataDir, "telemetry.log") + ")"
	case props.Tool.Telemetry.OTelEndpoint != "":
		opts := []telemetry.OTelOption{
			telemetry.WithOTelLogger(props.Logger),
			telemetry.WithOTelService(props.Tool.Name, resolveVersionString(props)),
		}

		if props.Tool.Telemetry.OTelInsecure {
			opts = append(opts, telemetry.WithOTelInsecure())
		}

		if len(props.Tool.Telemetry.OTelHeaders) > 0 {
			opts = append(opts, telemetry.WithOTelHeaders(props.Tool.Telemetry.OTelHeaders))
		}

		b, err := telemetry.NewOTelBackend(ctx, props.Tool.Telemetry.OTelEndpoint, opts...)
		if err != nil {
			props.Logger.Warn("failed to initialise OTel backend, falling back to noop", "error", err)

			return telemetry.NewNoopBackend(), "noop (OTel init failed)"
		}

		return b, "otlp (" + props.Tool.Telemetry.OTelEndpoint + ")"
	case props.Tool.Telemetry.Endpoint != "":
		return telemetry.NewHTTPBackend(props.Tool.Telemetry.Endpoint, props.Logger), "http (" + props.Tool.Telemetry.Endpoint + ")"
	default:
		return telemetry.NewNoopBackend(), "noop (no endpoint configured)"
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
