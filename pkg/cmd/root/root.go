package root

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/njayp/ophis"
	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go/output"

	cmdchangelog "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/changelog"
	cmdconfig "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/docs"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/doctor"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/initialise"
	cmdman "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/man"
	cmdtelemetry "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/telemetry"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/update"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/version"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry"
	"gitlab.com/phpboyscout/go-tool-base/pkg/utils"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

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
	watching            bool
	formCreator         func(*bool) *huh.Form
	// watchStop tears down the config watcher started by startConfigWatch.
	// Retained (rather than discarded) so shutdown is deterministic instead of
	// purely contextual — an embedder driving the tree with a background
	// context could otherwise never stop the fsnotify/poll goroutines. See the
	// config-family follow-ups spec (F10).
	watchStop func()
	// watchOpts are passed to Store.Watch. Nil in production (the store picks
	// its own defaults); a test seam for injecting a config.WithWatcher fake.
	watchOpts []config.WatchOption
}

func newRootState() *rootState {
	return &rootState{
		formCreator: createUpdatePromptForm,
	}
}

// FlagValues holds the command-line flag values extracted from cobra command.
type FlagValues struct {
	Debug bool
}

// ConfigLoadOptions holds the options needed for loading configuration.
type ConfigLoadOptions struct {
	CfgPaths    []string
	ConfigPaths []string
	Props       *p.Props
	AllowEmpty  bool

	// ProjectConfigPath is the discovered project-local ".<tool>.yaml" (a
	// repo-root config layer), or "" when none applies (no file, or an
	// explicit --config suppressed it). When set it is layered as the
	// highest-precedence file — but a hostile clone must not be able to
	// downgrade security posture through it, so unless the directory is
	// trusted (setup.IsProjectConfigTrusted) its security-sensitive keys are
	// stripped and it is read-only. See projectLayerBackend.
	ProjectConfigPath string

	// Flags is the dispatched command's full flag set (local + inherited).
	// Changed flags become the store's highest-precedence layer; nil skips
	// the layer (reload paths that outlive the invocation's flag values).
	Flags *pflag.FlagSet
	// BoundFlags maps config keys to author-declared flags whose names do
	// not follow the hyphen-to-dot convention (WithBoundFlags).
	BoundFlags map[string]*pflag.Flag
}

// extractFlags extracts and validates command-line flags from cobra command.
// Only --debug needs pre-config extraction (it steers logging before the
// store exists); every other flag reaches configuration through the flags
// layer bound at store construction.
func extractFlags(cmd *cobra.Command) (*FlagValues, error) {
	debug, err := cmd.Flags().GetBool("debug")
	if err != nil {
		return nil, errors.Wrap(err, "failed to get debug flag")
	}

	return &FlagValues{Debug: debug}, nil
}

// projectConfigLayer returns the discovered project-local ".<tool>.yaml" (a
// repo-root config layer, found by walking up from the working directory), or ""
// when none applies. It is layered as the highest-precedence file so it
// deep-merges over the default config paths (env + flags still override it). A
// convention like .editorconfig — a tool opts out by not having the file.
//
// An explicit --config suppresses the layer entirely. That flag replaces the
// default paths rather than adding to them, which is the whole reason it is
// declared with the defaults as its default value: naming a config file means
// "use this one". A project-local file the caller did not name — and may not
// know is there — must not override files they did name, because nothing on the
// command line would explain the resulting settings.
//
// The returned path is handled specially at store construction: unless the
// directory is trusted, its security-sensitive keys are stripped so a hostile
// clone cannot downgrade update verification or telemetry consent (see
// projectLayerBackend and setup.IsProjectConfigTrusted).
func projectConfigLayer(props *p.Props, cmd *cobra.Command) string {
	if cmd.Flags().Changed("config") {
		return ""
	}

	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	pc := setup.DiscoverProjectConfig(props.FS, props.Tool.Name, cwd)
	if pc == "" {
		return ""
	}

	props.Logger.Debug("project config layer found", "file", pc)

	return pc
}

// ErrNoConfigFile reports that none of the candidate config files exist.
//
// config v0.2.0 supplied this as ErrNoFilesFound. The Store does not: a missing
// file is an empty layer, not an error, because in a layered model there is
// nothing unusual about a layer being absent. GTB still needs the distinction —
// it is what gates auto-initialise — so the sentinel is owned here.
var ErrNoConfigFile = errors.New("no config file found")

// buildConfigStore constructs the configuration store for a command.
//
// Layer order, lowest precedence first, matching the documented precedence:
//
//  1. assets/config.yaml merged across every registered bundle — the
//     embedded-defaults layer (framework, enabled features, the tool's own).
//     Always applies: a user file that omits a key resolves to the shipped
//     default (segregated-default-config spec, D4).
//  2. the tool's explicit ConfigPaths embedded assets
//  3. the config files — --config paths if given, otherwise the defaults,
//     with a project-local .<tool>.yaml appended last where one applies
//  4. environment variables under the tool's prefix
//  5. changed CLI flags
//
// The Store makes this a declaration rather than a sequence of merges. What it
// replaces built the user config, built the embedded config separately, then
// deep-merged one into the other by round-tripping through JSON — because Viper
// merges eagerly and had no notion of a layer.
func buildConfigStore(ctx context.Context, opts ConfigLoadOptions) (*config.Store, error) {
	fsys := opts.Props.GetConfigFS()

	storeOpts := []config.StoreOption{}

	if defaults := setup.AssetSource(opts.Props, setup.DefaultsAssetPath); defaults != nil {
		storeOpts = append(storeOpts, config.WithReaders(*defaults))
	}

	if embedded := embeddedSources(opts); len(embedded) > 0 {
		storeOpts = append(storeOpts, config.WithReaders(embedded...))
	}

	fileOpts, err := fileLayerOpts(fsys, opts)
	if err != nil {
		return nil, err
	}

	storeOpts = append(storeOpts, fileOpts...)

	if prefix := opts.Props.Tool.EnvPrefix; prefix != "" {
		storeOpts = append(storeOpts, config.WithEnv(prefix))
	}

	// Flags are the highest-precedence layer. Only flags the user actually
	// changed contribute (the backend walks pflag's Visit), so a flag at its
	// default never clobbers configuration.
	if opts.Flags != nil {
		storeOpts = append(storeOpts, config.WithFlags(opts.Flags, flagBindings(opts.BoundFlags)...))
	}

	store, err := config.NewStore(ctx, storeOpts...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load config")
	}

	return store, nil
}

// fileLayerOpts assembles the file-backed store layers: the user's config files
// and, when discovered, the project-local layer above them. It also enforces the
// missing-config gate. Split out of buildConfigStore to keep that function's
// branching within bounds.
func fileLayerOpts(fsys config.FS, opts ConfigLoadOptions) ([]config.StoreOption, error) {
	// Only files that actually exist are declared as layers. A non-existent
	// file contributes nothing to resolution, and declaring it anyway makes it
	// a candidate write target — which is how a write to the user's config
	// wrongly routed to a missing system /etc path. Deciding what is real
	// before the store is constructed is GTB's job, not the store's.
	existing, err := existingConfigPaths(fsys, opts.CfgPaths)
	if err != nil {
		return nil, err
	}

	// A discovered project-local ".<tool>.yaml" counts toward "a config file
	// exists" for the missing-config gate, even though it is layered separately
	// from the user's own config files (it may be the only config a
	// repository-scoped run has).
	projectExists := opts.ProjectConfigPath != "" && projectConfigExists(fsys, opts.ProjectConfigPath)

	// The missing-config gate. auto-initialise depends on the distinction
	// between "no config file exists" and "a file exists but is empty".
	if len(existing) == 0 && !projectExists && !opts.AllowEmpty {
		return nil, ErrNoConfigFile
	}

	// The one deliberate exception to the existence rule: the write target —
	// the highest-precedence path — is always declared so a write has somewhere
	// to land and can create the file. It never triggers the missing-file
	// re-read that other absent layers would, because it is the written
	// backend (staged, not reloaded).
	layerOpts := []config.StoreOption{
		config.WithFiles(fsys, declaredConfigPaths(existing, opts.CfgPaths)...),
	}

	// The project-local layer sits above the user's config files but below env
	// and flags, and is subject to the trust filter: an untrusted file has its
	// security-sensitive keys stripped and is read-only (writes route to the
	// user's own config instead of the repository file).
	if projectExists {
		layerOpts = append(layerOpts, config.WithBackend(projectLayerBackend(opts.Props, fsys, opts.ProjectConfigPath)))
	}

	return layerOpts, nil
}

// flagBindings maps author-declared bound flags (WithBoundFlags) onto flag
// backend options; every other flag maps by the hyphen-to-dot convention.
func flagBindings(boundFlags map[string]*pflag.Flag) []config.FlagOption {
	flagOpts := make([]config.FlagOption, 0, len(boundFlags))

	for key, flag := range boundFlags {
		if flag != nil {
			flagOpts = append(flagOpts, config.BindFlag(flag.Name, key))
		}
	}

	return flagOpts
}

// existingConfigPaths returns the subset of paths that exist on disk, in the
// same order.
//
// The Store tolerates a missing file silently, and Sources() lists every path
// that was declared whether or not it loaded — so neither answers which files
// are real. Asking the filesystem directly does. A path that exists but cannot
// be read (a permissions problem, a broken mount) is a hard error rather than
// silently dropped: treating it as absent would fall back to defaults and hide
// a real fault.
func existingConfigPaths(fsys config.FS, paths []string) ([]string, error) {
	existing := make([]string, 0, len(paths))

	for _, path := range paths {
		switch _, err := fsys.Stat(path); {
		case err == nil:
			existing = append(existing, path)
		case errors.Is(err, fs.ErrNotExist):
			continue
		default:
			return nil, errors.Wrapf(err, "checking config file %s", path)
		}
	}

	return existing, nil
}

// projectConfigExists reports whether the project-local config file is present
// on disk. A path that exists but cannot be stat'd is treated as absent — the
// project layer is an optional convenience, not a hard dependency, so a broken
// stat degrades to "not layered" rather than failing the whole bootstrap.
func projectConfigExists(fsys config.FS, path string) bool {
	_, err := fsys.Stat(path)

	return err == nil
}

// declaredConfigPaths returns the config files to declare as store layers: every
// file that exists, plus the write target when it does not.
//
// The write target is the highest-precedence declared path (the last in all) —
// where a write lands and, when absent, is created. Keeping it in the list even
// when it does not exist is the sole exception to declaring only real files;
// every other absent path is excluded so it can neither shadow resolution nor
// capture a write. The target keeps its precedence position (last), so it still
// wins on read once written.
func declaredConfigPaths(existing, all []string) []string {
	if len(all) == 0 {
		return existing
	}

	writeTarget := all[len(all)-1]
	if slices.Contains(existing, writeTarget) {
		return existing
	}

	return append(slices.Clone(existing), writeTarget)
}

// embeddedSources reads the tool's explicit embedded config assets into named
// sources.
func embeddedSources(opts ConfigLoadOptions) []config.NamedSource {
	sources := make([]config.NamedSource, 0, len(opts.ConfigPaths))

	for _, path := range opts.ConfigPaths {
		if src := setup.AssetSource(opts.Props, path); src != nil {
			sources = append(sources, *src)
		}
	}

	return sources
}

// resolveBootstrapConfig loads configuration for cmd, applying the tool's
// bootstrap policy. It relaxes the missing-config gate for commands that opt out
// (Tool.Bootstrap.SkipConfigCheck or the setup.SkipConfigCheck annotation) and
// heals a missing config via a non-interactive init when Tool.Bootstrap.
// AutoInitialise is set and the init feature is enabled. SkipConfigCheck takes
// precedence over AutoInitialise: a command that declared it owns bootstrap is
// never auto-initialised for. Neither branch skips the framework bootstrap
// itself — only the missing-config outcome changes, preserving the
// "bootstrap always runs" invariant (2026-06-12-bootstrap-prerun-traversal).
func resolveBootstrapConfig(props *p.Props, cmd *cobra.Command, configPaths, cfgPaths []string, boundFlags map[string]*pflag.Flag) (*config.Store, error) {
	initEnabled := props.Tool.IsEnabled(p.InitCmd)
	skipConfigCheck := setup.SkipsConfigCheck(cmd) ||
		props.Tool.Bootstrap.MatchesSkipList(cmd.Name(), cmd.CommandPath())

	allowEmpty := !initEnabled || skipConfigCheck

	loadOpts := ConfigLoadOptions{
		CfgPaths:          cfgPaths,
		ConfigPaths:       configPaths,
		Props:             props,
		AllowEmpty:        allowEmpty,
		Flags:             cmd.Flags(),
		BoundFlags:        boundFlags,
		ProjectConfigPath: projectConfigLayer(props, cmd),
	}

	cfg, err := buildConfigStore(cmd.Context(), loadOpts)
	if err != nil {
		// Auto-initialise heals a genuinely missing config: run a
		// non-interactive init (no credential wizards) to write the default
		// localised config, then load it for real. ErrNoConfigFile is only
		// returned when AllowEmpty is false — i.e. init is enabled and the
		// command did not opt out — so the gate needs no further conjuncts.
		if props.Tool.Bootstrap.AutoInitialise && errors.Is(err, ErrNoConfigFile) {
			return autoInitialiseConfig(cmd.Context(), props, loadOpts)
		}

		return nil, err
	}

	return cfg, nil
}

// autoInitialiseConfig heals a missing configuration by running a
// non-interactive init (credential wizards suppressed) to write the default
// localised config, then reloads it. It is invoked by the root pre-run only
// when Tool.Bootstrap.AutoInitialise is set, the init feature is enabled, and
// the initial load failed with ErrNoConfigFile. The reload uses
// AllowEmpty:false so a genuinely broken init surfaces an error rather than
// silently masking it with embedded defaults.
func autoInitialiseConfig(ctx context.Context, props *p.Props, opts ConfigLoadOptions) (*config.Store, error) {
	dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)
	if dir == "" {
		return nil, errors.New("auto-initialise: cannot resolve config directory (is HOME set?)")
	}

	props.Logger.Debug("No config file found; auto-initialising default configuration")

	// Non-interactive, and with no Initialisers supplied the credential
	// wizards cannot run regardless -- only the base configuration is written.
	noInteractive := false
	if _, err := setup.Initialise(ctx, props, setup.InitOptions{
		Dir:         dir,
		Interactive: &noInteractive,
	}); err != nil {
		return nil, errors.Wrap(err, "auto-initialise failed")
	}

	opts.AllowEmpty = false

	cfg, err := buildConfigStore(ctx, opts)
	if err != nil {
		return nil, errors.Wrap(err, "auto-initialise: reload after init failed")
	}

	return cfg, nil
}

// reloadLoggingObserver returns the config observer that re-applies logging
// configuration on every reload, against the snapshot that triggered it. It is
// the hot-reload half of logging setup: without it a reloaded log.level/log.format
// changes the store but never the logger. The --debug flag still wins (see
// configureLogging), so a reload cannot downgrade an explicit --debug.
func reloadLoggingObserver(props *p.Props, flags *FlagValues, mcpLogLevel *slog.LevelVar) func(config.Observed) error {
	return func(o config.Observed) error {
		configureLogging(props, flags, o, mcpLogLevel)

		return nil
	}
}

// configureLogging sets up logging based on debug flag and config values.
func configureLogging(props *p.Props, flags *FlagValues, cfg config.Reader, mcpLogLevel *slog.LevelVar) {
	// Apply debug flag first. SetLevel/SetFormatter are no-ops for an injected
	// plain *slog.Logger (which owns its own level); they take effect on GTB's
	// default Charm-backed logger, which implements Leveller/Reformatter.
	if flags.Debug {
		logger.SetLevel(props.Logger, slog.LevelDebug)
		mcpLogLevel.Set(slog.LevelDebug)
	} else if level, err := logger.ParseLevel(cfg.GetString("log.level")); err == nil {
		// Apply config-based log level if debug flag is not set
		logger.SetLevel(props.Logger, mapLogLevel(level))
		mcpLogLevel.Set(mapLogLevel(level))
	}

	// Apply log format from config
	switch cfg.GetString("log.format") {
	case "json":
		logger.SetFormatter(props.Logger, logger.JSONFormatter)
	case "logfmt":
		logger.SetFormatter(props.Logger, logger.LogfmtFormatter)
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
func checkForUpdates(ctx context.Context, cmd *cobra.Command, props *p.Props, state *rootState) *UpdateCheckResult {
	result := &UpdateCheckResult{}

	// One pinned view for the whole check, so the policy, CI gate and
	// interval agree with each other even under a mid-sequence reload.
	view := props.Config.View()

	policy := p.ResolveUpdatePolicy(props.Tool.UpdatePolicy, view.GetString("update.policy"))

	// Persistent out-of-date reminder from the cached latest version: emitted
	// every invocation (even when the network check is throttled below), so a
	// user who declined an update — or runs a disabled-policy tool — keeps
	// being reminded. Suppressed in CI (the --ci flag / ci config key or the
	// CI=true environment variable), for full flag/environment parity.
	if !isCIEnvironment(view) {
		warnIfBehindCached(props)
	}

	if shouldSkipUpdateCheck(props, view, cmd, state) {
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

	// The passive pre-run check must never block a command for long: a captive
	// portal or firewalled release host would otherwise stall every invocation
	// for the download-sized updateTimeout (5m). Bound this probe by the short
	// VersionCheckTimeout; the full budget is reserved for an explicit `update`
	// run (performUpdate below, which uses update.Update's own timeout).
	checkCtx, cancel := context.WithTimeout(ctx, setup.VersionCheckTimeout)
	defer cancel()

	spinErr := output.New().Spin(checkCtx, "Checking for latest version", func(ctx context.Context) error {
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

// isCIEnvironment reports whether the invocation should be treated as running
// in CI. It agrees with the two ways CI is signalled: the --ci flag / `ci`
// config key (reached through the config layers), and the bare CI=true
// environment variable that the forge initialisers already honour
// (pkg/setup/forge/profile.go). Config-flag and environment detection must not
// diverge — a real CI run that forgets --ci must still be recognised — so both
// the update-check gate and the telemetry-consent gate route through here.
func isCIEnvironment(view *config.View) bool {
	return view.GetBool("ci") || os.Getenv("CI") == "true"
}

func shouldSkipUpdateCheck(props *p.Props, view *config.View, cmd *cobra.Command, state *rootState) bool {
	// Skip update checks in various conditions
	if props.Tool.IsDisabled(p.UpdateCmd) ||
		(props.Version != nil && props.Version.IsDevelopment()) ||
		state.redirectingToUpdate ||
		isCIEnvironment(view) {
		return true
	}

	interval := setup.ResolveCheckInterval(props.Tool.UpdateCheckInterval, view.GetString("update.check_interval"))

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
	formCreator   func(*bool) *huh.Form
	isInteractive func() bool
}

// WithForm allows providing a custom form creator for testing.
func WithForm(formCreator func(*bool) *huh.Form) OutdatedVersionOption {
	return func(cfg *outdatedVersionConfig) {
		cfg.formCreator = formCreator
	}
}

// WithInteractive overrides the TTY gate (default: utils.IsInteractive) so tests
// can exercise the interactive prompt path without a real terminal.
func WithInteractive(isInteractive func() bool) OutdatedVersionOption {
	return func(cfg *outdatedVersionConfig) {
		cfg.isInteractive = isInteractive
	}
}

func handleOutdatedVersion(ctx context.Context, props *p.Props, message string, result *UpdateCheckResult, state *rootState, policy p.UpdatePolicy, opts ...OutdatedVersionOption) {
	props.Logger.Warn(message)

	// disabled: log that an update is available and carry on — no prompt, no
	// block. The persistent cached-version WARN keeps reminding on later runs.
	if policy == p.UpdatePolicyDisabled {
		props.Logger.Warn(fmt.Sprintf("a newer version is available — run '%s update' to upgrade when ready", props.Tool.Name))

		return
	}

	// Apply options
	cfg := &outdatedVersionConfig{
		formCreator:   state.formCreator,
		isInteractive: utils.IsInteractive,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Default to declining: without a usable TTY (cron, CI, piped stdin, MCP
	// stdio) the prompt cannot be answered, so runUpdate must stay false.
	// Defaulting to true here would silently self-update without consent.
	var runUpdate = false

	// Gate the prompt on interactivity rather than relying on form.Run to error
	// out on a non-terminal stdin — the assumption the MR !157 incident
	// disproved (huh forms hung the e2e suite on piped stdin). When
	// non-interactive we skip the prompt deterministically without touching
	// stdin; the policy semantics below are unchanged (enabled still blocks with
	// the "update required" error, prompt still warns and continues).
	if cfg.isInteractive() {
		form := cfg.formCreator(&runUpdate)
		// Allow nil form for testing (form creator can set the value and return nil)
		if form != nil {
			if err := form.Run(); err != nil {
				runUpdate = false

				props.Logger.Debug("update prompt unavailable; declining update", "error", err)
			}
		}
	} else {
		props.Logger.Debug("update prompt skipped: non-interactive stdin")
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

	props.Logger.Warn(fmt.Sprintf("Continuing with an out of date version, please run '%s update' ASAP", props.Tool.Name))
}

// performUpdate runs the self-update and records the outcome on result: a
// successful update sets HasUpdated/ShouldExit (the caller exits 0 and asks the
// user to re-run); a failed update sets result.Error so the process exits
// non-zero rather than masking the failure.
func performUpdate(ctx context.Context, props *p.Props, result *UpdateCheckResult, state *rootState) {
	state.redirectingToUpdate = true

	if _, err := update.Update(ctx, props, "", false, os.Stdout); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			result.Error = errors.WithHint(
				errors.New("update timed out"),
				"Check your internet connection or try again later.")

			return
		}

		result.Error = err

		return
	}

	props.Logger.Warn("update complete please run command again to use the updated version")

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
		props.Logger.Warn(fmt.Sprintf("a newer %s is available: %s — run '%s update' to upgrade",
			props.Tool.Name, cached, props.Tool.Name))
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
// init enables cobra's root→leaf PersistentPreRunE traversal for the whole
// process. Without it, a downstream subcommand that defines its own
// PersistentPreRunE silently shadows the root bootstrap (config load, telemetry,
// update check) for that subtree; with it set, the framework bootstrap always
// runs first and a child hook runs after it. Cobra exposes this only as a
// package-global, so it is set exactly once here at package initialisation —
// which happens-before any command construction OR execution — rather than on
// every NewCmdRootWithOptions call. Writing it per-construction raced with a
// concurrent ExecuteContext reading the same global (two roots, or a construct
// alongside an execute, in one process). See spec
// 2026-06-12-bootstrap-prerun-traversal.
func init() {
	cobra.EnableTraverseRunHooks = true
}

func NewCmdRootWithOptions(props *p.Props, opts ...RootOption) *setup.Command {
	o := applyRootOptions(opts)

	// Default the required dependencies so the blessed construction path never
	// nil-derefs, even for a hand-built Props (tests, cmd/e2e, downstream tools
	// that skip props.New). A nil Logger/FS is a programmer error, but the root
	// cannot return one, so it fills them rather than panicking deep in a
	// command.
	if props.Logger == nil {
		props.Logger = logger.NewNoop()
	}

	if props.FS == nil {
		props.FS = afero.NewOsFs()
	}

	// Fill the optional invariants: Collector (NoopCollector), ErrorHandler,
	// and Version. The real *telemetry.Collector is resolved later in the
	// PersistentPreRunE; ErrorHandler and Version must be non-nil before then
	// because the init/help paths return early and the update-check path
	// dereferences Version unconditionally. ApplyDefaults is the same routine
	// props.New uses, so struct-literal and New-built Props converge here.
	props.ApplyDefaults()

	// Surface a contract violation the defaults cannot fix (an unnamed Tool)
	// instead of failing obscurely later. Non-fatal: the root cannot return an
	// error, so it warns rather than aborting.
	if err := props.Validate(); err != nil {
		props.Logger.Warn("props contract violation at root construction", "error", err)
	}

	// Wire the logger into Assets so a malformed embedded bundle surfaces as a
	// WARN during merged structured reads instead of vanishing silently.
	if setter, ok := any(props.Assets).(interface{ SetLogger(logger.Logger) }); ok {
		setter.SetLogger(props.Logger)
	}

	// Feature-gated asset bundles: apply every enabled feature's registered
	// bundle before the command tree is built, so the merged defaults and
	// init-template reads include them (segregated-default-config spec, D8).
	registerFeatureAssets(props)

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
	// cobra parses them; the pre-run then binds them into the store's flags
	// layer (only changed flags contribute).
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
		// Stamp THIS root's Props onto the command context so the process-global
		// middleware chain resolves the right Props at execution time — a second
		// root in the same process must not report through the first root's
		// logger/collector. Done before the fast path so auxiliary/init commands
		// carry it too.
		cmd.SetContext(setup.ContextWithProps(cmd.Context(), props))

		// Fast path — skip the framework bootstrap entirely (config load,
		// telemetry consent, collector wiring, update check) but still honour
		// --debug so `--debug tool completion bash` stays debuggable:
		//
		//   - the init subtree: init is what CREATES the config, and its
		//     provider subcommands (`init github`, …) must equally run on a
		//     configless machine. Identified by the typed feature annotation
		//     (stamped by setup.Wrap), walking up the tree — never the fragile
		//     Use string, which misfires for any unrelated command literally
		//     named "init" and breaks if Use carries an arg suffix.
		//   - cobra's own generated help/completion/__complete commands: on a
		//     fresh install they must not fail the missing-config gate, and
		//     shell tab-completion must never pay the bootstrap (least of all
		//     the network update check) on every keystroke.
		//   - commands a downstream tool lists in Tool.Bootstrap.
		//     AuxiliaryCommands, so the set is extensible without a framework
		//     release.
		if isInitFeatureSubtree(cmd) || isAuxiliaryCommand(props, cmd) {
			applyDebugFlag(props, cmd, mcpLogLevel)

			return nil
		}

		// Extract and validate flags
		flags, err := extractFlags(cmd)
		if err != nil {
			return errors.Wrap(err, "failed to read command flags")
		}

		// Load configuration, applying the tool's bootstrap policy (skip-config
		// check / auto-initialise). Bootstrap itself always runs — only the
		// missing-config outcome is relaxed. The project-local config layer
		// (projectConfigLayer) is resolved inside resolveBootstrapConfig so it
		// is honoured on both the initial load and any auto-initialise reload.
		cfg, err := resolveBootstrapConfig(props, cmd, configPaths, state.cfgPaths, boundFlags)
		if err != nil {
			return configLoadError(props, err)
		}

		// Set config in props
		props.Config = cfg

		startConfigWatch(props, cfg, cmd, state, flags, mcpLogLevel)

		// One pinned view for the bootstrap reads below.
		view := cfg.View()

		// Validate config for common misconfigurations
		validateConfig(view, props.Logger)

		// Configure logging based on flags and config
		configureLogging(props, flags, view, mcpLogLevel)

		// Prompt for telemetry consent if the feature is enabled but not yet
		// configured. Never under the mcp subtree: an MCP server's stdout carries
		// JSON-RPC frames, so prompt UI must not be rendered there even on an
		// interactive terminal (the update check is already exempt via
		// MarkSkipUpdateCheck).
		if !isMCPFeatureSubtree(cmd) {
			promptTelemetryConsent(cmd.Context(), props)
		}

		// Build and wire telemetry collector
		props.Collector = buildTelemetryCollector(cmd.Context(), props)

		// Check for updates
		if props.Tool.IsDisabled(p.UpdateCmd) {
			return nil
		}

		updateResult := checkForUpdates(cmd.Context(), cmd, props, state)
		if updateResult.Error != nil {
			return updateResult.Error
		}

		if updateResult.ShouldExit {
			return ErrUpdateComplete
		}

		return nil
	}
}

// configLoadError wraps a bootstrap config-load failure for the user. A
// missing config file is a fresh-install state, not a fault: the error gains a
// hint pointing at the command that creates one. ErrNoConfigFile is only
// returned when the init feature is enabled (otherwise the load tolerates an
// empty config), so the hint is always actionable.
func configLoadError(props *p.Props, err error) error {
	err = errors.Wrap(err, "failed to load configuration")

	if errors.Is(err, ErrNoConfigFile) && props.Tool.IsEnabled(p.InitCmd) {
		err = errors.WithHintf(err, "Run '%s init' to create a configuration.", props.Tool.Name)
	}

	return err
}

// startConfigWatch wires hot-reload for the loaded store. Watching is explicit
// in config v0.3.x: without this call the code compiles, the tests pass, and
// configuration silently stops reloading (migration spec D6). A filesystem that
// cannot be watched (tests on a MemMapFs, exotic mounts) degrades to a debug
// line rather than an error. Guarded per rootState so a re-entrant pre-run
// (tests executing the same tree twice) does not stack watchers.
//
// The stop func Store.Watch returns is retained on rootState and, when the run
// is framework-driven (Execute), published to the command context's cleanup
// slot so execute can tear the watcher down on shutdown. Context cancellation
// remains the backstop: a raw ExecuteContext embedder has no cleanup slot, so
// the watcher stops when its context is cancelled (config-family spec F10).
//
// A reload observer re-applies logging (flags/mcpLogLevel) so an edited
// log.level/log.format takes effect on a long-running command without a
// restart, with --debug still winning.
func startConfigWatch(props *p.Props, cfg *config.Store, cmd *cobra.Command, state *rootState, flags *FlagValues, mcpLogLevel *slog.LevelVar) {
	if state.watching {
		return
	}

	cfg.OnReloadError(func(err error) {
		props.Logger.Warn("config reload rejected; keeping the last good configuration", "error", err)
	})

	// Re-apply logging on every reload so a long-running command (docs serve,
	// controls-based services) picks up an edited log.level/log.format without a
	// restart — the whole point of hot-reload. configureLogging keeps the
	// --debug flag winning, so a reload can never downgrade an explicit --debug.
	// The observer only mutates the logger (never configuration), so it cannot
	// trip ErrWriteFromObserver.
	cfg.AddObserverFunc(reloadLoggingObserver(props, flags, mcpLogLevel))

	stop, err := cfg.Watch(cmd.Context(), state.watchOpts...)
	if err != nil {
		props.Logger.Debug("config watching unavailable", "error", err)

		return
	}

	state.watching = true

	// teardown stops the watcher and clears the guard so a reused command tree
	// re-establishes the watch on its next run rather than silently losing
	// hot-reload after the first teardown. Store.Watch's stop is once-guarded,
	// so a second call (context-cancel backstop plus explicit invoke) is safe.
	teardown := func() {
		stop()

		state.watching = false
	}
	state.watchStop = teardown

	if cleanup := watchCleanupFrom(cmd.Context()); cleanup != nil {
		cleanup.register(teardown)
	}
}

// isInitFeatureSubtree reports whether cmd is the init command or any
// descendant of it, identified by walking up the tree for the InitCmd feature
// annotation stamped by setup.Wrap. The walk is what exempts provider
// subcommands (`init github`, `init bitbucket`), which are wrapped with their
// *provider* feature and would otherwise fail the missing-config gate — the
// one command tree that exists to fix that state.
func isInitFeatureSubtree(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if setup.FeatureOf(c) == p.InitCmd {
			return true
		}
	}

	return false
}

// isMCPFeatureSubtree reports whether cmd is the mcp command or any descendant
// of it, identified by walking up the tree for the McpCmd feature annotation
// stamped by setup.Wrap. Used to suppress the interactive pre-run prompts
// (telemetry consent) whose UI would corrupt the MCP server's JSON-RPC stdout —
// a feature match, never a fragile name match.
func isMCPFeatureSubtree(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if setup.FeatureOf(c) == p.McpCmd {
			return true
		}
	}

	return false
}

// Cobra's help and completion command names. Unlike the __complete constants
// (cobra.ShellCompRequestCmd et al.) cobra does not export these, but they are
// equally reserved: cobra only generates its help/completion commands when no
// command of that name exists.
const (
	cobraHelpCommandName       = "help"
	cobraCompletionCommandName = "completion"
)

// isAuxiliaryCommand reports whether cmd takes the pre-run's auxiliary fast
// path: cobra's own generated help/completion/__complete commands, plus any
// command the tool author listed in Tool.Bootstrap.AuxiliaryCommands.
func isAuxiliaryCommand(props *p.Props, cmd *cobra.Command) bool {
	return isCobraAuxiliaryCommand(cmd) ||
		props.Tool.Bootstrap.MatchesAuxiliaryList(cmd.Name(), cmd.CommandPath())
}

// isCobraAuxiliaryCommand reports whether cmd is one of cobra's own generated
// auxiliary commands: the hidden __complete/__completeNoDesc used by shell
// tab-completion, the help command, or the completion group (including its
// bash/zsh/fish/powershell subcommands, hence the parent walk).
//
// Only cobra's OWN instances are exempted, not any downstream command that
// happens to share a name. The __complete names are cobra-reserved constants;
// for help/completion the discriminator is isCobraGeneratedCommand — cobra's
// generated commands are direct children of the root and carry no annotations,
// whereas every GTB-wrapped command is stamped by setup.Wrap.
func isCobraAuxiliaryCommand(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return true
	case cobraHelpCommandName:
		return isCobraGeneratedCommand(cmd)
	}

	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == cobraCompletionCommandName && isCobraGeneratedCommand(c) {
			return true
		}
	}

	return false
}

// isCobraGeneratedCommand reports whether c looks like a command cobra
// generated itself: a direct child of the root carrying no annotations. GTB
// commands always carry at least the feature annotation stamped by setup.Wrap
// (or a skip-check annotation), so a downstream feature command named "help"
// or "completion" is never mistaken for cobra's.
func isCobraGeneratedCommand(c *cobra.Command) bool {
	return c.Parent() == c.Root() && len(c.Annotations) == 0
}

// applyDebugFlag applies the --debug flag to the logger for fast-path
// commands that skip the config-driven configureLogging. Tolerant of commands
// whose flags are not parsed (cobra's __complete disables flag parsing), where
// the debug flag is simply unavailable.
func applyDebugFlag(props *p.Props, cmd *cobra.Command, mcpLogLevel *slog.LevelVar) {
	flags, err := extractFlags(cmd)
	if err != nil || !flags.Debug {
		return
	}

	logger.SetLevel(props.Logger, slog.LevelDebug)
	mcpLogLevel.Set(slog.LevelDebug)
}

func setupRootFlags(rootCmd *cobra.Command, props *p.Props, state *rootState) {
	// Precedence is declaration order, lowest first — so the system-wide
	// /etc file is declared before the per-user file, letting the user's
	// config override the machine's. This is the Unix convention (user beats
	// system), and it also makes the user file the highest-precedence writable
	// layer, so set/unset/edit land there rather than in the root-owned /etc
	// path an unprivileged user cannot write. A project-local .<tool>.yaml,
	// when present, is appended after both and wins over each.
	defaultConfigPaths := []string{
		fmt.Sprintf("%s%s", string(os.PathSeparator), filepath.Join("etc", props.Tool.Name, setup.DefaultConfigFilename)),
		filepath.Join(setup.GetDefaultConfigDir(props.FS, props.Tool.Name), setup.DefaultConfigFilename),
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

// registerFeatureAssets applies the asset bundles of enabled features onto
// props.Assets, in deterministic feature order. Non-command feature packages
// (pkg/setup/forge, pkg/setup/ai) announce their bundles via
// setup.RegisterAssets from init(); command-owned bundles register in their
// constructors, which only run for enabled features.
func registerFeatureAssets(props *p.Props) {
	if props.Assets == nil {
		return
	}

	registered := setup.GetAssets()

	for _, feature := range slices.Sorted(maps.Keys(registered)) {
		if !props.Tool.IsEnabled(feature) {
			continue
		}

		for _, bundle := range registered[feature] {
			props.Assets.Register(bundle.Name, bundle.Bundle)
		}
	}
}

func registerFeatureCommands(rootCmd *setup.Command, props *p.Props, mcpLogLevel *slog.LevelVar) {
	registerGlobalMiddlewareOnce(props)

	// version produces its output from build-time ldflags and embedded assets,
	// so it must run on a fresh install with no config file yet. Relaxing the
	// missing-config gate (rather than erroring) is what lets it.
	rootCmd.Register(skipConfigGate(version.NewCmdVersion(props)))

	// Simple feature-gated commands: register each when its feature is enabled.
	// Constructors with optional variadic options are wrapped in thunks so the
	// table stays a single uniform func() type. skipGate marks the commands
	// that read nothing from config and must work before any config exists.
	simple := []struct {
		feature  p.FeatureCmd
		build    func() *setup.Command
		skipGate bool
	}{
		{p.UpdateCmd, func() *setup.Command { return update.NewCmdUpdate(props) }, false},
		{p.InitCmd, func() *setup.Command { return initialise.NewCmdInit(props) }, false},
		{p.DoctorCmd, func() *setup.Command { return doctor.NewCmdDoctor(props) }, false},
		{p.ConfigCmd, func() *setup.Command { return cmdconfig.NewCmdConfig(props) }, false},
		{p.TelemetryCmd, func() *setup.Command { return cmdtelemetry.NewCmdTelemetry(props) }, false},
		{p.ChangelogCmd, func() *setup.Command { return cmdchangelog.NewCmdChangelog(props) }, true},
		{p.ManCmd, func() *setup.Command { return cmdman.NewCmdMan(props) }, true},
	}
	for _, c := range simple {
		if props.Tool.IsEnabled(c.feature) {
			cmd := c.build()
			if c.skipGate {
				skipConfigGate(cmd)
			}

			rootCmd.Register(cmd)
		}
	}

	if props.Tool.IsEnabled(p.McpCmd) {
		mcpCmd := ophis.Command(&ophis.Config{
			SloggerOptions: &slog.HandlerOptions{
				Level: mcpLogLevel,
			},
			Selectors: mcpSelectors(),
		})
		// An MCP server's stdout carries JSON-RPC frames; the pre-run update
		// check's spinner/log output must never race it. The stamp covers the
		// whole mcp subtree (start/tools) via SkipUpdateCheck's parent walk.
		setup.MarkSkipUpdateCheck(mcpCmd)
		rootCmd.Register(setup.Wrap(p.McpCmd, mcpCmd))
	}

	if props.Tool.IsEnabled(p.DocsCmd) {
		if docsCmd := docs.NewCmdDocs(props); docsCmd != nil {
			rootCmd.Register(skipConfigGate(docsCmd))
		}
	}
}

// skipConfigGate marks a built-in command so the root pre-run relaxes its
// missing-config gate: when no config file exists the framework loads embedded
// defaults instead of erroring, so a command that reads nothing from config
// (version, changelog, man, docs) still runs on a fresh install. props.Config
// stays populated, so any incidental read is safe. A nil command passes
// through unchanged.
func skipConfigGate(cmd *setup.Command) *setup.Command {
	if cmd != nil {
		setup.SkipConfigCheck(cmd.Command)
	}

	return cmd
}

// mcpSelectors returns the ophis selector that gates commands off the MCP tool
// surface. A single selector exposes a command when the nearest explicit
// mcp_enabled decision in its ancestor chain is exposed (or none is set) — see
// [setup.IsExposedToMCP]. With nothing marked, every command resolves to
// exposed and, because the flag selectors are nil, every flag is included,
// making this equivalent to ophis' nil-selector default (expose all).
//
// The decision is resolved lazily: ophis invokes the CmdSelector when it
// enumerates tools at `mcp start` / `mcp tools` run time — by which point the
// full command tree (including self-registering tool commands) exists. It is
// therefore wrong to branch on the tree at root-build time here.
func mcpSelectors() []ophis.Selector {
	return []ophis.Selector{{CmdSelector: setup.IsExposedToMCP}}
}

const telemetryFlushTimeout = 2 * time.Second

// ConsentOption configures promptTelemetryConsent behaviour.
type ConsentOption func(*consentConfig)

type consentConfig struct {
	isInteractive func() bool
}

// WithConsentInteractive overrides the TTY gate (default: utils.IsInteractive)
// so tests can exercise the interactive consent path without a real terminal.
func WithConsentInteractive(isInteractive func() bool) ConsentOption {
	return func(cfg *consentConfig) {
		cfg.isInteractive = isInteractive
	}
}

// consentPromptDeferred reports whether the one-time telemetry consent prompt
// must be skipped without touching stdin, logging the reason for the CI and
// non-interactive defers. It centralises the guard chain so the prompt body
// stays simple. The order matters: author/config decisions (disabled,
// force-enabled, env var, already-answered) short-circuit before the
// environment gates (CI, then interactivity).
func consentPromptDeferred(props *p.Props, view *config.View, isInteractive func() bool) bool {
	_, telemetryEnvSet := os.LookupEnv("TELEMETRY_ENABLED")

	switch {
	case props.Tool.IsDisabled(p.TelemetryCmd):
		return true
	case props.Tool.Telemetry.ForceEnabled:
		// Tool author has force-enabled telemetry — no prompt, always on.
		return true
	case telemetryEnvSet:
		// TELEMETRY_ENABLED pre-answers the consent question.
		return true
	case view.IsSet("telemetry.enabled"):
		// Already configured — no prompt needed.
		return true
	case isCIEnvironment(view):
		// CI (flag/config key or CI=true env) — defer silently, persist nothing.
		props.Logger.Debug("telemetry consent deferred: CI environment")

		return true
	case !isInteractive():
		// Non-interactive stdin (cron, piped input, MCP stdio) — defer silently
		// rather than relying on huh to error out on a non-terminal (the
		// assumption the MR !157 incident disproved). Persist nothing; the opt-in
		// reappears on the next interactive run.
		props.Logger.Debug("telemetry consent deferred: non-interactive stdin")

		return true
	default:
		return false
	}
}

// promptTelemetryConsent shows a one-time opt-in prompt when TelemetryCmd is
// enabled but the user hasn't made a choice yet. Prompting is skipped — without
// ever touching stdin — when telemetry is force-enabled, the TELEMETRY_ENABLED
// env var is set, telemetry.enabled is already present in config, the run is in
// CI (the --ci flag / ci config key or the CI=true environment variable), or
// stdin is not a terminal. A skipped prompt persists nothing: absence of consent
// is not refusal, so the one-time opt-in simply reappears on the next
// interactive run.
func promptTelemetryConsent(ctx context.Context, props *p.Props, opts ...ConsentOption) {
	cfg := &consentConfig{isInteractive: utils.IsInteractive}
	for _, opt := range opts {
		opt(cfg)
	}

	view := props.Config.View()

	if consentPromptDeferred(props, view, cfg.isInteractive) {
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

	// Persist the choice so we don't prompt again. Apply writes only this key
	// and creates a missing file; the default config dir may not exist yet
	// (tools without InitCmd), so ensure it first.
	if _, err := setup.EnsureDefaultConfigDir(props.FS, props.Tool.Name); err != nil {
		props.Logger.Debug("failed to create config directory", "error", err)

		return
	}

	if _, err := props.Config.Apply(ctx, config.Set("telemetry.enabled", optIn)); err != nil {
		props.Logger.Debug("failed to persist telemetry consent", "error", err)
	}
}

// resolveVersionString returns the tool version, or "" when Props was built
// without a Version (the interface field is nilable on hand-constructed Props).
func resolveVersionString(props *p.Props) string {
	if props.Version == nil {
		return ""
	}

	return props.Version.GetVersion()
}

// buildTelemetryCollector creates the appropriate telemetry collector based on
// feature flags, user config, environment variables, and tool-author settings.
func buildTelemetryCollector(ctx context.Context, props *p.Props) *telemetry.Collector {
	dataDir := telemetry.ResolveDataDirFromProps(props)

	// Version is an interface and may be nil on a hand-constructed Props (the
	// scaffold always sets it, but downstream tools need not). Resolve it once
	// with a nil guard, mirroring shouldSkipUpdateCheck and the doctor command.
	version := resolveVersionString(props)

	if props.Tool.IsDisabled(p.TelemetryCmd) {
		return telemetry.NewCollector(telemetry.Config{}, telemetry.NewNoopBackend(),
			props.Tool.Name, version, nil, logger.ToSlog(props.Logger), dataDir, p.DeliveryAtLeastOnce, false)
	}

	view := props.Config.View()
	cfg := telemetry.Config{
		Enabled:   view.GetBool("telemetry.enabled"),
		LocalOnly: view.GetBool("telemetry.local_only"),
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
			props.Tool.Name, version, nil, logger.ToSlog(props.Logger), dataDir, p.DeliveryAtLeastOnce, false)
	}

	deliveryMode := props.Tool.Telemetry.DeliveryMode
	if deliveryMode == "" {
		deliveryMode = p.DeliveryAtLeastOnce
	}

	backend, backendInfo := selectTelemetryBackend(ctx, props, cfg, dataDir)

	collector := telemetry.NewCollector(cfg, backend, props.Tool.Name, version,
		props.Tool.Telemetry.Metadata, logger.ToSlog(props.Logger), dataDir, deliveryMode, props.Tool.Telemetry.ExtendedCollection)
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
			telemetry.WithOTelLogger(logger.ToSlog(props.Logger)),
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
		return telemetry.NewHTTPBackend(props.Tool.Telemetry.Endpoint, logger.ToSlog(props.Logger)), "http (" + props.Tool.Telemetry.Endpoint + ")"
	default:
		return telemetry.NewNoopBackend(), "noop (no endpoint configured)"
	}
}

// validateConfig warns about common misconfigurations.
//
// The keys checked are the credential-resolution literals — the same list
// doctor's credentials.no-literal check uses (doctor.LiteralCredentialKeys) —
// so the warning tracks the current credential schema instead of a hardcoded,
// pre-forge-migration list. The stale "github.token" key it once checked can
// never fire against today's schema (the forge migration moved to
// "github.auth.value").
func validateConfig(cfg config.Reader, l logger.Logger) {
	for _, key := range doctor.LiteralCredentialKeys {
		if cfg.IsSet(key) && cfg.GetString(key) == "" {
			l.Warn(key + " is set but empty — operations using this key will fail")
		}
	}
}
