package setup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/httpclient"

	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go/changelog"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/utils"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"

	"charm.land/huh/v2"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"

	"gitlab.com/phpboyscout/go/signing/verify"
)

const (
	// copyChunkSize is the size of chunks when copying files to prevent decompression bomb attacks.
	copyChunkSize = 1024
	// filePermExecutable is the permission mode for the installed binary (0755).
	// Chmod SETS the mode, so 0o111 would leave the binary execute-only
	// (--x--x--x) — runnable, but the owner could no longer read, copy,
	// checksum, or back it up.
	filePermExecutable = 0o755
	// defaultMaxExtractedBytes bounds the cumulative DECOMPRESSED size of the
	// extracted binary (1 GiB). Any legitimate GTB-family binary is far below
	// it; the cap exists so a crafted archive cannot expand without limit while
	// checksum/signature enforcement is off (the compile-time default).
	defaultMaxExtractedBytes int64 = 1 << 30
	// releasesPerPage is the number of releases to fetch per API page.
	releasesPerPage = 100
	// defaultCheckInterval is the default time interval for update checks.
	defaultCheckInterval = 24 * time.Hour
	// updateTimeout is the timeout for update operations. Five minutes
	// is sized for binary downloads: release archives can run to tens of
	// megabytes and must complete over slow or intermittent links, so the
	// previous 3-minute value risked aborting otherwise-healthy updates.
	updateTimeout = 5 * time.Minute
	// VersionCheckTimeout bounds passive, best-effort latest-version
	// lookups — the courtesy annotation on the `version` command — where
	// the check decorates an otherwise-local command and must not stall
	// it for minutes on a black-holing network. The root pre-run update
	// check currently still runs under the download-sized updateTimeout
	// and is expected to adopt this constant in a follow-up (see
	// https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0173-version-command-offline-degradation
	// §2, "Related but out of scope").
	VersionCheckTimeout = 10 * time.Second
)

type timeSinceKey string

const (
	UpdatedKey = timeSinceKey("updated")
	CheckedKey = timeSinceKey("checked")
)

// SelfUpdater manages checking for and applying tool updates.
type SelfUpdater struct {
	Tool           props.Tool
	force          bool
	version        string
	logger         logger.Logger
	releaseClient  forge.Provider
	CurrentVersion string
	NextRelease    forge.Release
	Fs             afero.Fs
	osExecutable   func() (string, error)
	execLookPath   func(string) (string, error)
	// isInteractive reports whether an interactive terminal is available
	// for prompts. Defaults to utils.IsInteractive; injectable for tests.
	isInteractive func() bool

	// requireChecksum resolved from config (update.require_checksum →
	// env-prefixed env var via the store's env layer → DefaultRequireChecksum).
	// When true, a missing or invalid checksums manifest aborts the update
	// rather than proceeding with a warning.
	requireChecksum bool
	// checksumAssetName overrides the default "checksums.txt" lookup for
	// releases that use a different manifest filename. Resolved from
	// `update.checksum_asset_name`; empty means use the default.
	checksumAssetName string

	// requireSignature resolved from config (update.require_signature →
	// env-prefixed env var → verify.DefaultRequireSignature). When true, a
	// missing/unverifiable signature over the checksums manifest aborts
	// the update.
	requireSignature bool
	// signatureAssetName overrides the default "checksums.txt.sig"
	// lookup. Resolved from `update.signature_asset_name`; empty means
	// use the default.
	signatureAssetName string
	// keyResolver resolves the trust set used to verify the manifest
	// signature. nil when signature verification is not configured.
	keyResolver verify.KeyResolver
	// embeddedKeys are armored public keys supplied via WithEmbeddedKeys;
	// consumed in NewUpdater to build keyResolver when one was not
	// supplied explicitly via WithKeyResolver.
	embeddedKeys [][]byte
	// keySource / externalKeyEmail / requireExternalCrosscheck are the
	// resolved update.* config values used to build the default
	// resolver from embeddedKeys.
	keySource                 string
	externalKeyEmail          string
	requireExternalCrosscheck bool

	// Connection identity and credential wiring for the release source, kept
	// so a refusal can be explained rather than flattened (spec 0193 D5). Set
	// by resolveReleaseClient, and left zero when a provider was injected
	// directly via props.Tool.ReleaseProvider — explainRefusal degrades to a
	// generic message rather than naming the wrong host.
	endpoint    forge.Endpoint
	authConfig  forge.Config
	fallbackEnv string

	// maxBinaryDownloadSize caps DownloadAsset's in-memory read. Zero means
	// use DefaultMaxBinaryDownloadSize.
	maxBinaryDownloadSize int64

	// maxChecksumsSize caps a downloaded checksums manifest. Zero means use
	// DefaultMaxChecksumsSize.
	maxChecksumsSize int64

	// maxExtractedBytes caps the cumulative DECOMPRESSED size written while
	// extracting the archived binary, so a gzip bomb cannot expand unbounded
	// even when checksum/signature enforcement is off. Zero means use
	// defaultMaxExtractedBytes.
	maxExtractedBytes int64
}

// extractedBound returns the decompressed-size bound for this updater.
func (s *SelfUpdater) extractedBound() int64 {
	if s.maxExtractedBytes > 0 {
		return s.maxExtractedBytes
	}

	return defaultMaxExtractedBytes
}

// checksumsBound returns the manifest bound for this updater.
func (s *SelfUpdater) checksumsBound() int64 {
	if s.maxChecksumsSize > 0 {
		return s.maxChecksumsSize
	}

	return DefaultMaxChecksumsSize
}

// checksumsDefaultAssetName is the filename GoReleaser produces for
// the manifest by default. Override per-tool via the
// `update.checksum_asset_name` config key.
const checksumsDefaultAssetName = "checksums.txt"

// DefaultCheckInterval is the update-check throttle used when
// `update.check_interval` is unset or invalid.
const DefaultCheckInterval = defaultCheckInterval

// ResolveCheckInterval resolves the update-check throttle from, in order of
// precedence: the `update.check_interval` config value (if a valid, non-negative
// Go duration — where "0"/"0s" means "check on every invocation"), then the
// tool author's baseline (toolDefault, if greater than zero), then
// [DefaultCheckInterval]. A toolDefault of zero is treated as "unset" and falls
// through to the framework default rather than meaning "every run"; runtime
// config is the only way to request the no-throttle behaviour.
func ResolveCheckInterval(toolDefault time.Duration, configValue string) time.Duration {
	if v := strings.TrimSpace(configValue); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			return d
		}
	}

	if toolDefault > 0 {
		return toolDefault
	}

	return DefaultCheckInterval
}

// timeSinceLast returns the age of the recorded "last <status>" timestamp and
// whether such a record exists.
func timeSinceLast(fs afero.Fs, name string, status timeSinceKey) (time.Duration, bool) {
	dir := GetDefaultConfigDir(fs, name)
	lastSinceFile := filepath.Join(dir, fmt.Sprintf("last_%s", status))

	if di, err := fs.Stat(dir); err == nil && di.IsDir() {
		if fi, err := fs.Stat(lastSinceFile); err == nil {
			return time.Since(fi.ModTime()), true
		}
	}

	return 0, false
}

// GetTimeSinceLast returns the duration since the last update check or update,
// or [DefaultCheckInterval] when no timestamp has been recorded yet.
func GetTimeSinceLast(fs afero.Fs, name string, status timeSinceKey) time.Duration {
	if age, ok := timeSinceLast(fs, name, status); ok {
		return age
	}

	return defaultCheckInterval
}

// markerFilePerm restricts the last_* marker files to the owner.
const markerFilePerm = 0o600

// SetTimeSinceLast records the current time as the last check or update timestamp
// (an empty marker file whose modtime is the timestamp).
// When the default config directory cannot be resolved (empty/unset HOME),
// it is a no-op: stamping a relative path would otherwise write the marker
// into the current working directory.
func SetTimeSinceLast(fs afero.Fs, name string, status timeSinceKey) error {
	return setTimeSinceLastIn(fs, GetDefaultConfigDir(fs, name), status, "")
}

// SetCheckedVersion stamps the last-checked marker and stores the latest release
// version that check discovered as the marker's body, so a later invocation can
// warn that the running binary is out of date without a network call (see
// GetCheckedVersion). The marker's modtime still drives the interval throttle —
// one file, two jobs. A blank version clears the stored value while still
// refreshing the check timestamp (e.g. when the tool is found up to date).
func SetCheckedVersion(fs afero.Fs, name, version string) error {
	return setTimeSinceLastIn(fs, GetDefaultConfigDir(fs, name), CheckedKey, strings.TrimSpace(version))
}

// GetCheckedVersion returns the latest release version recorded by the most
// recent update check (the body of the last_checked marker), or "" when none
// has been recorded or the tool was up to date at the last check.
func GetCheckedVersion(fs afero.Fs, name string) string {
	dir := GetDefaultConfigDir(fs, name)
	if dir == "" {
		return ""
	}

	data, err := afero.ReadFile(fs, filepath.Join(dir, fmt.Sprintf("last_%s", CheckedKey)))
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

// setTimeSinceLastIn writes body to the "last_<status>" marker under configDir,
// refreshing its modtime (the timestamp). An empty configDir is a no-op (see
// SetTimeSinceLast) so the marker never lands in the current working directory
// via a relative join. body is usually empty; the last_checked marker
// additionally carries the latest known version (see SetCheckedVersion).
func setTimeSinceLastIn(fs afero.Fs, configDir string, status timeSinceKey, body string) error {
	if configDir == "" {
		return nil
	}

	// Create the config dir lazily at first write — GetDefaultConfigDir is pure
	// and no longer creates it as a side effect.
	if err := fs.MkdirAll(configDir, dirPermUserOnly); err != nil {
		return errors.Wrap(err, "failed to create config directory")
	}

	lastSinceFile := filepath.Join(configDir, fmt.Sprintf("last_%s", status))

	if err := afero.WriteFile(fs, lastSinceFile, []byte(body), markerFilePerm); err != nil {
		return errors.Wrap(err, "failed to write marker file")
	}

	return nil
}

// UpdaterOption configures a SelfUpdater.
type UpdaterOption func(*SelfUpdater)

// WithOsExecutable overrides os.Executable for testing.
func WithOsExecutable(fn func() (string, error)) UpdaterOption {
	return func(s *SelfUpdater) { s.osExecutable = fn }
}

// WithExecLookPath overrides exec.LookPath for testing.
func WithExecLookPath(fn func(string) (string, error)) UpdaterOption {
	return func(s *SelfUpdater) { s.execLookPath = fn }
}

// WithReleaseProvider injects the [forge.Provider] the SelfUpdater uses,
// bypassing the ReleaseSource.Type registry lookup (and, with it, the
// private-repository token gate that precedes the lookup — an injected
// provider is self-contained and owns its own auth). Parallel-safe: each call
// site receives its own provider, with no global registry mutation. Takes
// precedence over a props.Tool.ReleaseProvider field.
// WithRequireChecksum sets checksum enforcement for this updater when neither
// config nor environment provides a value.
//
// Replaces the former package-level DefaultRequireChecksum: a tool wanting
// fail-closed verification states it where it builds its updater, rather than
// mutating shared state at init.
func WithRequireChecksum(require bool) UpdaterOption {
	return func(s *SelfUpdater) {
		s.requireChecksum = require
	}
}

// WithMaxChecksumsSize raises the bound on a downloaded checksums manifest.
// Zero or negative keeps [DefaultMaxChecksumsSize].
func WithMaxChecksumsSize(maxBytes int64) UpdaterOption {
	return func(s *SelfUpdater) {
		s.maxChecksumsSize = maxBytes
	}
}

// WithMaxBinaryDownloadSize raises the bound on a downloaded binary asset.
// Zero or negative keeps [DefaultMaxBinaryDownloadSize].
func WithMaxBinaryDownloadSize(maxBytes int64) UpdaterOption {
	return func(s *SelfUpdater) {
		s.maxBinaryDownloadSize = maxBytes
	}
}

func WithReleaseProvider(p forge.Provider) UpdaterOption {
	return func(s *SelfUpdater) { s.releaseClient = p }
}

// NewOfflineUpdater creates a SelfUpdater configured for file-based updates
// that do not require a VCS client or network access.
func NewOfflineUpdater(tool props.Tool, log logger.Logger, fs afero.Fs, opts ...UpdaterOption) *SelfUpdater {
	s := &SelfUpdater{
		logger:        log,
		Tool:          tool,
		Fs:            fs,
		osExecutable:  os.Executable,
		execLookPath:  exec.LookPath,
		isInteractive: utils.IsInteractive,
	}

	for _, o := range opts {
		o(s)
	}

	return s
}

// NewUpdater creates a SelfUpdater configured with the tools release source.
// The context is forwarded to [forge.ResolveTokenContext] for
// private-repository token resolution, so remote-store credential
// backends (Vault, SSM) honour the caller's deadline when fetching
// the release token.
func NewUpdater(ctx context.Context, p *props.Props, version string, force bool, opts ...UpdaterOption) (*SelfUpdater, error) {
	if p.Config == nil {
		return nil, errors.New("configuration is not loaded")
	}

	// Version may be unset in tests that only exercise release-client
	// resolution; guard rather than dereference a nil provider.
	currentVersion := ""
	if p.Version != nil {
		currentVersion = ver.FormatVersionString(p.Version.GetVersion(), true)
	}

	// One pinned view for every read below, so the trust-model settings
	// resolve against a single snapshot (p.Config was nil-checked above).
	cfg := p.Config.View()

	s := &SelfUpdater{
		force:                     force,
		version:                   version,
		logger:                    p.Logger,
		Tool:                      p.Tool,
		CurrentVersion:            currentVersion,
		Fs:                        p.FS,
		osExecutable:              os.Executable,
		execLookPath:              exec.LookPath,
		isInteractive:             utils.IsInteractive,
		requireChecksum:           resolveRequireChecksum(cfg, p.Tool.Signing.RequireChecksum),
		checksumAssetName:         strings.TrimSpace(cfg.GetString("update.checksum_asset_name")),
		requireSignature:          resolveRequireSignature(cfg),
		signatureAssetName:        strings.TrimSpace(cfg.GetString("update.signature_asset_name")),
		keySource:                 resolveKeySource(cfg),
		externalKeyEmail:          resolveExternalKeyEmail(cfg),
		requireExternalCrosscheck: resolveRequireExternalCrosscheck(cfg),
		// Seed the embedded trust anchor from the tool author's
		// compile-time keys (e.g. an internal trustkeys //go:embed). All
		// library command call sites (version/update/root) thus pick up
		// the embedded keys without passing options. An explicit
		// WithEmbeddedKeys/WithKeyResolver option below still overrides.
		embeddedKeys: p.Tool.Signing.EmbeddedKeys,
	}

	// Options are applied before the release client is resolved so
	// WithReleaseProvider can supply s.releaseClient and short-circuit the
	// registry lookup (and its private-repo token gate).
	for _, o := range opts {
		o(s)
	}

	if err := resolveReleaseClient(ctx, p, s); err != nil {
		return nil, err
	}

	if err := s.buildDefaultKeyResolver(); err != nil {
		return nil, err
	}

	return s, nil
}

// resolveReleaseClient fills s.releaseClient using the precedence: an injected
// provider (WithReleaseProvider, already applied) → props.Tool.ReleaseProvider
// → the ReleaseSource.Type registry lookup. The registry path is the only one
// that enforces the private-repository token gate; an injected provider is
// self-contained.
func resolveReleaseClient(ctx context.Context, p *props.Props, s *SelfUpdater) error {
	if s.releaseClient != nil {
		return nil
	}

	if p.Tool.ReleaseProvider != nil {
		s.releaseClient = p.Tool.ReleaseProvider

		return nil
	}

	// One pinned view for the provider override and the release-client config
	// (NewUpdater nil-checked p.Config before calling here).
	cfg := p.Config.View()

	vcsProvider, _, _ := p.Tool.GetReleaseSource()
	if cfg.IsSet("vcs.provider") {
		vcsProvider = strings.ToLower(cfg.GetString("vcs.provider"))
	}

	if p.Tool.ReleaseSource.Private {
		if err := requireReleaseToken(ctx, vcsProvider, p); err != nil {
			return err
		}
	}

	factory, err := forge.Lookup(vcsProvider)
	if err != nil {
		return err
	}

	// Only connection identity reaches the factory. Owner and Repo are
	// arguments to the operations that need them (GetLatestRelease and its
	// siblings all take them), and Private is consumed above, where it decides
	// whether a token is required at all.
	//
	// Type is set from vcsProvider rather than from ReleaseSource.Type: config
	// may override the provider, and Lookup was already given the overridden
	// value. Endpoint.Section scopes a provider's config subtree by Type, so
	// disagreeing here would hand the provider the wrong subtree.
	endpoint := forge.Endpoint{
		Type: vcsProvider,
		Host: p.Tool.ReleaseSource.Host,
	}

	forgeCfg := vcs.ConfigFromReader(cfg)

	releaseClient, err := factory(ctx, endpoint, forgeCfg)
	if err != nil {
		return errors.WithStack(err)
	}

	s.releaseClient = releaseClient

	// Kept so a refusal can be explained rather than flattened: the endpoint
	// names the host in the message, and the subtree lets explainRefusal say
	// which credential rung supplied a token the forge rejected. Storing them
	// costs nothing and resolves nothing (spec 0193 D5).
	s.endpoint = endpoint
	s.authConfig = endpoint.Section(forgeCfg)
	s.fallbackEnv = strings.ToUpper(vcsProvider) + "_TOKEN"

	return nil
}

// buildDefaultKeyResolver constructs keyResolver from the resolved
// config and any keys supplied via WithEmbeddedKeys, unless an explicit
// resolver was already set via WithKeyResolver. Does nothing when
// neither embedded keys nor an external key email are configured —
// signature verification is then governed entirely by requireSignature
// at verify time.
func (s *SelfUpdater) buildDefaultKeyResolver() error {
	if s.keyResolver != nil {
		return nil
	}

	if len(s.embeddedKeys) == 0 && s.externalKeyEmail == "" {
		return nil
	}

	r, err := verify.BuildKeyResolver(verify.KeyResolverConfig{
		KeySource:                 s.keySource,
		ExternalKeyEmail:          s.externalKeyEmail,
		RequireExternalCrosscheck: s.requireExternalCrosscheck,
		// Bridge gtb's logger to the verify package's stdlib *slog.Logger seam,
		// and inject gtb's hardened HTTP client for WKD fetches (the module's
		// nil-default is a plain stdlib client).
		Logger:     slog.New(s.logger.Handler()),
		HTTPClient: httpclient.NewClient(),
	}, s.embeddedKeys...)
	if err != nil {
		return errors.Wrap(err, "configuring update signature verification")
	}

	s.keyResolver = r
	s.logger.Info("update signature verification configured", "resolver", r.Name())

	return nil
}

// boolConfig is the narrow subset of [config.Reader] that
// [resolveRequireChecksum] depends on. Declared here so tests can
// pass a two-method fake without stubbing the full interface.
type boolConfig interface {
	IsSet(key string) bool
	GetBool(key string) bool
}

// resolveRequireChecksum applies the precedence specified by the
// update trust model: explicit config value first (which the store's
// env layer supplies from a prefixed env var too),
// otherwise the compile-time [DefaultRequireChecksum] sentinel. Tool
// authors flip the default at link time for security-critical tools.
// resolveRequireChecksum applies the precedence: runtime config, then the tool
// author's baseline from props, then the framework default.
func resolveRequireChecksum(cfg boolConfig, toolDefault *bool) bool {
	fallback := requireChecksumDefault
	if toolDefault != nil {
		fallback = *toolDefault
	}

	if cfg == nil {
		return fallback
	}

	// Interface typed nil (e.g. a nil *config.View wrapped in boolConfig)
	// must not panic on IsSet. Protect the call with a reflective check.
	if reflectIsNil(cfg) {
		return fallback
	}

	if cfg.IsSet("update.require_checksum") {
		return cfg.GetBool("update.require_checksum")
	}

	return fallback
}

// reflectIsNil detects the interface-typed-nil case (an interface
// value whose concrete type is non-nil but whose value is the nil
// pointer for that type). A plain `cfg == nil` check does not catch
// this case; call sites that construct the interface from a pointer
// can pass through a nil pointer wrapped in a non-nil interface.
func reflectIsNil(i any) bool {
	if i == nil {
		return true
	}

	rv := reflect.ValueOf(i)

	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

// requireReleaseToken returns an error if no authentication token is available
// for the given VCS provider. Used to give a clear error for private repositories
// before attempting an unauthenticated API call that would fail with a 401.
func requireReleaseToken(ctx context.Context, vcsProvider string, p props.ConfigProvider) error {
	var (
		cfg         = vcs.ConfigFromReader(p.GetConfig().View()).Sub(vcsProvider)
		fallbackEnv string
	)

	switch vcsProvider {
	case "gitlab":
		fallbackEnv = "GITLAB_TOKEN"
	case "bitbucket":
		// Bitbucket uses username + app_password; credential presence is checked
		// inside the Bitbucket provider factory, which has access to both vars.
		return nil
	case "gitea":
		fallbackEnv = "GITEA_TOKEN"
	case "codeberg":
		fallbackEnv = "CODEBERG_TOKEN"
	case "direct":
		fallbackEnv = "DIRECT_TOKEN"
	default:
		fallbackEnv = "GITHUB_TOKEN"
	}

	token, err := vcs.ForgeCredential(cfg, fallbackEnv)(ctx)
	if token != "" {
		return nil
	}

	// FirstCredential retains why each rung failed when none produced a value.
	// Surfacing that turns a malformed keychain reference into a diagnosis
	// rather than something indistinguishable from having no token at all.
	problem := errors.Newf("no %s token available for private repository", strings.ToUpper(vcsProvider))
	if err != nil {
		problem = errors.Wrapf(err, "resolving %s credential for private repository", vcsProvider)
	}

	return errors.WithHint(
		problem,
		fmt.Sprintf("Set %s or configure %s.auth.env in your config to enable updates", fallbackEnv, vcsProvider),
	)
}

// SkipUpdateCheck reports whether the update check should be skipped this
// invocation: always for commands exempted by annotation or feature (see
// below), and otherwise when a prior check happened within checkInterval. A
// checkInterval <= 0 means check on every invocation; a missing timestamp
// (first run) is never skipped, regardless of the interval.
//
// Exemption is decided by the typed command metadata, not the Use string (the
// old {"update","auth","init","version"} name list collided with any
// downstream command coincidentally named "auth"/"init" and broke when Use
// carried an args suffix). A command is exempt when it — or any ancestor, so a
// stamped or feature-owned group covers its whole subtree — carries the
// [MarkSkipUpdateCheck] annotation or is wrapped with the UpdateCmd or InitCmd
// feature. Built-ins stamp themselves (version, doctor, mcp); a downstream
// command opts out with setup.MarkSkipUpdateCheck(cmd).
func SkipUpdateCheck(fs afero.Fs, name string, cmd *cobra.Command, checkInterval time.Duration) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if SkipsUpdateCheck(c) {
			return true
		}

		if f := FeatureOf(c); f == props.UpdateCmd || f == props.InitCmd {
			return true
		}
	}

	if checkInterval <= 0 {
		return false
	}

	age, ok := timeSinceLast(fs, name, CheckedKey)

	return ok && age < checkInterval
}

// IsLatestVersion checks if the current running binary is the latest version.
func (s *SelfUpdater) IsLatestVersion(ctx context.Context) (bool, string, error) {
	if s.isDevelopmentVersion() {
		return true, "you are using a development version and must use the'update' command with the --force flag to update", nil
	}

	latestVersion, err := s.GetLatestVersionString(ctx)
	if err != nil {
		return false, "failed to check for latest version", errors.WithStack(err)
	}

	if latestVersion == "" {
		// A nil err with an empty tag means the release source returned an
		// unparseable/blank version. Surface a real error instead of
		// errors.WithStack(nil) (which is nil) so the caller does not treat
		// the binary as up to date and silently skip the update.
		return false, "failed to check for latest version", errors.New("setup: latest version string is empty")
	}

	switch ver.CompareVersions(s.CurrentVersion, latestVersion) {
	case -1: // need an upgrade
		return false, fmt.Sprintf("newer version found. Please upgrade to %s.  Run the `update`", latestVersion), nil
	case 1: // the release source reports an older "latest" than the running binary
		return false, fmt.Sprintf("the release source reports %s as latest but you are running %s; a downgrade will not be applied automatically — run `update --force` or pin a target with `update --version`", latestVersion, s.CurrentVersion), nil
	default: // just right
		return true, fmt.Sprintf("already running latest version, %s", s.CurrentVersion), nil
	}
}

// Update installs the latest version of the binary to the resolved target path.
func (s *SelfUpdater) Update(ctx context.Context) (string, error) {
	targetPath, err := s.resolveTargetPath()
	if err != nil {
		return "", err
	}

	skip, err := s.shouldSkipUpdate(ctx)
	if err != nil {
		return targetPath, err
	}

	if skip {
		return targetPath, nil
	}

	latestVersion, err := s.GetLatestRelease(ctx)
	if err != nil {
		return targetPath, errors.WithStack(err)
	}

	s.logger.Info("targeting version", "version", latestVersion.GetName())

	asset, err := s.findReleaseAsset(latestVersion)
	if err != nil {
		return targetPath, err
	}

	s.logger.Info("downloading", "name", asset.GetName(), "url", asset.GetBrowserDownloadURL())

	file, err := s.DownloadAsset(ctx, asset)
	if err != nil {
		return targetPath, errors.WithStack(err)
	}

	if err := s.verifyAssetChecksum(ctx, latestVersion, asset, file.Bytes()); err != nil {
		return targetPath, err
	}

	if err := s.extract(file, targetPath); err != nil {
		return targetPath, err
	}

	// Stamp the freshness markers only after a successful extract — a
	// failed extract must not record the binary as freshly updated, or the
	// reminder/skip logic would defer the next attempt for up to the check
	// interval despite no update having landed.
	_ = SetTimeSinceLast(s.Fs, s.Tool.Name, UpdatedKey)
	_ = SetTimeSinceLast(s.Fs, s.Tool.Name, CheckedKey)

	return targetPath, nil
}

// verifyAssetChecksum locates the release's checksums manifest and
// verifies the downloaded binary against it. The behaviour when no
// manifest is present depends on the resolved [SelfUpdater.requireChecksum]:
// fail-closed tools abort with an actionable error; fail-open tools
// log a warning and proceed (backward-compatible with pre-Phase-1
// releases that predate the manifest).
//
// Returns nil when the asset matches its manifest entry, or when no
// manifest was present AND requireChecksum is false. Any verification
// failure (mismatch, malformed manifest, asset-not-listed) is fatal.
func (s *SelfUpdater) verifyAssetChecksum(
	ctx context.Context,
	rel forge.Release,
	asset forge.ReleaseAsset,
	data []byte,
) error {
	manifest, err := s.fetchChecksumsManifest(ctx, rel)
	if err != nil {
		if s.requireChecksum || s.requireSignature {
			return errors.Wrap(err, "failed to retrieve checksums manifest (require_checksum/require_signature is enabled)")
		}

		s.logger.Warn("failed to retrieve checksums manifest; proceeding without verification",
			"release", rel.GetName(), "error", err)

		return nil
	}

	if manifest == nil {
		if s.requireChecksum || s.requireSignature {
			return errors.WithHint(
				errors.Newf("no checksums manifest found in release %q", rel.GetName()),
				"The release does not include a checksums file and update.require_checksum or "+
					"update.require_signature is enabled. Publish a GoReleaser-style checksums.txt "+
					"alongside the binaries, or disable the require_* flag to accept unverified updates.",
			)
		}

		s.logger.Warn("no checksums manifest found; skipping checksum verification",
			"release", rel.GetName(), "asset", asset.GetName())

		return nil
	}

	// Signature is verified over the raw manifest bytes BEFORE the
	// manifest is parsed: unsigned content must never reach the parser
	// (spec decision 17). The trust-set cross-check runs inside Resolve,
	// the earliest failure point.
	if err := s.verifyManifestSignature(ctx, rel, manifest); err != nil {
		return err
	}

	if err := VerifyChecksumFromManifest(manifest, asset.GetName(), data); err != nil {
		return err
	}

	s.logger.Info("checksum verified", "asset", asset.GetName())

	return nil
}

// fetchChecksumsManifest retrieves the manifest for rel, preferring
// [forge.ChecksumProvider] when the provider opts in and falling
// back to asset-list lookup otherwise. Returns (nil, nil) when no
// manifest is available by either route — the caller distinguishes
// "not found" from "download failed".
func (s *SelfUpdater) fetchChecksumsManifest(ctx context.Context, rel forge.Release) ([]byte, error) {
	var cp forge.ChecksumProvider
	if forge.As(s.releaseClient, &cp) {
		manifest, err := cp.DownloadChecksumManifest(ctx, rel, s.checksumsBound())
		if err == nil {
			return manifest, nil
		}

		if !errors.Is(err, forge.ErrNotSupported) {
			return nil, err
		}
		// ErrNotSupported: provider opted out for this release's
		// configuration. Fall through to asset-list lookup.
	}

	manifestAsset, found := s.findChecksumsAsset(rel)
	if !found {
		return nil, nil
	}

	return s.downloadChecksumManifest(ctx, manifestAsset)
}

// findChecksumsAsset returns the release asset whose name matches the
// configured checksums filename (`update.checksum_asset_name` or the
// GoReleaser default `checksums.txt`). The second return value is
// false when no matching asset is present, signalling to the caller
// that checksum verification is unavailable for this release.
func (s *SelfUpdater) findChecksumsAsset(rel forge.Release) (forge.ReleaseAsset, bool) {
	name := s.checksumAssetName
	if name == "" {
		name = checksumsDefaultAssetName
	}

	for _, asset := range rel.GetAssets() {
		if asset.GetName() == name {
			return asset, true
		}
	}

	return nil, false
}

// downloadChecksumManifest retrieves a checksums manifest asset with
// the size bound enforced. The manifest is a small text file (~1 KiB
// for a typical GoReleaser release); an unbounded read would let a
// hostile server stream indefinitely.
func (s *SelfUpdater) downloadChecksumManifest(
	ctx context.Context,
	asset forge.ReleaseAsset,
) ([]byte, error) {
	return s.downloadBoundedAsset(ctx, asset, s.checksumsBound(), ErrChecksumTooLarge, "checksums manifest")
}

// downloadBoundedAsset downloads a small release asset (manifest or
// signature) with a hard byte cap. Redirect responses are refused —
// these artefacts are tiny and a redirect signals a non-standard
// layout the verifier should not silently follow. tooLarge is the
// sentinel returned on overshoot; kind labels the artefact in errors.
func (s *SelfUpdater) downloadBoundedAsset(
	ctx context.Context,
	asset forge.ReleaseAsset,
	maxBytes int64,
	tooLarge error,
	kind string,
) ([]byte, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	_, owner, repo := s.Tool.GetReleaseSource()

	rc, redirectURL, err := s.releaseClient.DownloadReleaseAsset(timeoutCtx, owner, repo, asset)
	if err != nil {
		return nil, errors.Wrapf(err, "downloading %s", kind)
	}

	if rc != nil {
		defer func() { _ = rc.Close() }()
	}

	if redirectURL != "" {
		return nil, errors.Newf("%s redirected to %s; unsupported", kind, redirectURL)
	}

	// +1 so we can distinguish "exactly the limit" (accepted) from
	// "exceeded the limit" (refused).
	buf := bytes.Buffer{}

	n, err := io.Copy(&buf, io.LimitReader(rc, maxBytes+1))
	if err != nil {
		return nil, errors.Wrapf(err, "reading %s", kind)
	}

	if n > maxBytes {
		return nil, errors.WithHintf(tooLarge,
			"%s %q exceeded maximum size (%d bytes)", kind, asset.GetName(), maxBytes)
	}

	return buf.Bytes(), nil
}

// UpdateFromFile installs a binary from a local .tar.gz file.
// If a .sha256 sidecar file exists at filePath+".sha256", the checksum
// is verified before extraction. Returns the installation target path.
func (s *SelfUpdater) UpdateFromFile(filePath string) (string, error) {
	targetPath, err := s.resolveTargetPath()
	if err != nil {
		return "", err
	}

	// The offline path has no way to verify an OpenPGP signature (there is
	// no manifest+signature flow for a local tarball), so a required
	// signature cannot be satisfied here — fail closed rather than skip it.
	if s.requireSignature {
		return "", errors.WithHint(
			errors.New("update.require_signature is enabled but the offline update path cannot verify signatures"),
			"Use the online update path for signature-verified updates, or disable update.require_signature.",
		)
	}

	data, err := afero.ReadFile(s.Fs, filePath)
	if err != nil {
		return "", errors.Wrap(err, "failed to read update file")
	}

	sidecarPath := filePath + ".sha256"

	switch exists, _ := afero.Exists(s.Fs, sidecarPath); {
	case exists:
		if err := VerifyChecksum(s.Fs, sidecarPath, data); err != nil {
			return "", err
		}

		s.logger.Info("checksum verified", "file", filePath)
	case s.requireChecksum:
		return "", errors.WithHintf(
			errors.New("update.require_checksum is enabled but no checksum sidecar was found"),
			"Provide %s alongside the archive, or disable update.require_checksum.", sidecarPath,
		)
	default:
		s.logger.Warn("no checksum sidecar found, skipping verification", "expected", sidecarPath)
	}

	var file bytes.Buffer

	file.Write(data)

	if err := s.extract(file, targetPath); err != nil {
		return targetPath, err
	}

	// Stamp the freshness marker only after a successful extract (see the
	// online Update path for the rationale).
	_ = SetTimeSinceLast(s.Fs, s.Tool.Name, UpdatedKey)

	return targetPath, nil
}

func (s *SelfUpdater) resolveTargetPath() (string, error) {
	targetPath, err := s.osExecutable()
	if err != nil {
		return "", errors.WithStack(err)
	}

	// os.Executable already gives an authoritative path. A PATH lookup
	// failure (e.g. running ./mytool, or the tool isn't on PATH) is not
	// fatal — fall back to the running executable. Only prompt when the
	// lookup succeeded AND resolved to a different path.
	execPath, lookErr := s.execLookPath(s.Tool.Name)
	pathsDiffer := lookErr == nil && targetPath != execPath

	if !pathsDiffer {
		return targetPath, nil
	}

	// Paths differ. Without an interactive terminal (cron, CI, piped stdin)
	// we cannot prompt — default to the running executable rather than
	// blocking on a select that can never be answered.
	if s.isInteractive == nil || !s.isInteractive() {
		s.logger.Warn("multiple installations detected; updating the running executable",
			"running", targetPath, "on_path", execPath)

		return targetPath, nil
	}

	if err := huh.NewSelect[string]().
		Title("Multiple installations detected, Please select which to update").
		Options(
			huh.NewOption(targetPath, targetPath),
			huh.NewOption(execPath, execPath),
		).
		Value(&targetPath).
		Run(); err != nil {
		return "", errors.WithStack(err)
	}

	return targetPath, nil
}

// shouldSkipUpdate reports whether the explicit update should stop early
// because the binary is already current. A version-check error is returned to
// the caller rather than swallowed: the user invoked update explicitly, so a
// failure to determine the target version must surface (non-zero exit) instead
// of being treated as "already up to date". The throttled background check in
// pkg/cmd/root uses a separate path that intentionally tolerates check errors.
func (s *SelfUpdater) shouldSkipUpdate(ctx context.Context) (bool, error) {
	isLatestVersion, message, err := s.IsLatestVersion(ctx)
	if err != nil {
		return false, errors.Wrap(err, "failed to check for latest version")
	}

	if isLatestVersion && !s.force {
		s.logger.Warn(message)

		return true, nil
	}

	if err := s.refuseImplicitDowngrade(ctx); err != nil {
		return false, err
	}

	return false, nil
}

// ErrDowngradeRefused is returned when the implicit (no --version) update
// path would install a release older than the running binary. Signature and
// checksum verification authenticate an artefact, not its recency, so a
// stale or rolled-back release listing must not silently downgrade the tool
// (see the 2026-07-23 self-update downgrade guard spec).
var ErrDowngradeRefused = errors.NewSentinel("gtb.setup.downgrade_refused", "refusing to downgrade")

// refuseImplicitDowngrade rejects the implicit "install latest" path when the
// release source reports a "latest" release older than the running binary.
// Sanctioned downgrade routes are unaffected: --force overrides the guard,
// and an explicit --version is sufficient intent on its own. The refusal is
// a hard error (non-zero exit), not a warning-and-skip — a silently skipped
// update under rollback-attack conditions must be visible to the operator.
func (s *SelfUpdater) refuseImplicitDowngrade(ctx context.Context) error {
	if s.version != "" || s.force {
		return nil
	}

	// GetLatestRelease was already resolved (and cached in NextRelease) by
	// the IsLatestVersion call above, so this performs no extra API call.
	latestVersion, err := s.GetLatestVersionString(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to check for latest version")
	}

	if ver.CompareVersions(s.CurrentVersion, latestVersion) != 1 {
		return nil
	}

	return errors.WithHint(
		errors.Wrapf(ErrDowngradeRefused,
			"refusing to downgrade from %s to %s: the release source reports an older \"latest\" release",
			s.CurrentVersion, latestVersion),
		"If this is intentional, re-run with --force, or pin the target with --version.",
	)
}

func (s *SelfUpdater) findReleaseAsset(rel forge.Release) (forge.ReleaseAsset, error) {
	c := cases.Title(language.Und)
	targetOS := c.String(runtime.GOOS)

	targetArch := runtime.GOARCH
	if targetArch == "amd64" {
		targetArch = "x86_64"
	}

	targetName := fmt.Sprintf("%s_%s_%s.tar.gz", s.Tool.Name, targetOS, targetArch)

	for _, asset := range rel.GetAssets() {
		if asset.GetName() == targetName {
			return asset, nil
		}
	}

	return nil, errors.Newf("unable to find asset for %s %s", targetOS, targetArch)
}

// DownloadAsset downloads the raw bytes of a release asset.
func (s *SelfUpdater) DownloadAsset(ctx context.Context, asset forge.ReleaseAsset) (bytes.Buffer, error) {
	var file bytes.Buffer

	s.logger.Debug("downloading asset", "id", asset.GetID())

	timeoutCtx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	_, owner, repo := s.Tool.GetReleaseSource()

	rc, redirectURL, err := s.releaseClient.DownloadReleaseAsset(timeoutCtx, owner, repo, asset)
	if err != nil {
		return file, errors.WithStack(err)
	}

	if rc != nil {
		defer func() { _ = rc.Close() }()
	}

	if redirectURL != "" {
		return file, errors.Newf("redirected to %s", redirectURL)
	}

	maxBytes := s.maxBinaryDownloadSize
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBinaryDownloadSize
	}

	// +1 so we can distinguish "exactly the limit" from "over the limit".
	i, err := io.Copy(&file, io.LimitReader(rc, maxBytes+1))
	if err != nil {
		return file, errors.WithStack(err)
	}

	if i > maxBytes {
		return file, errors.WithHintf(ErrBinaryTooLarge,
			"asset %q exceeded the binary download bound (%d bytes); raise it with WithMaxBinaryDownloadSize if this is legitimate",
			asset.GetName(), maxBytes)
	}

	s.logger.Debug("downloaded", "size", i)

	return file, nil
}

// Extract from the file buffer containing the raw bytes of a .tar.gz file.
func (s *SelfUpdater) extract(file bytes.Buffer, targetPath string) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(file.Bytes()))
	if err != nil {
		return errors.WithStack(err)
	}

	defer func() { _ = gzipReader.Close() }()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break // End of archive
		}

		if err != nil {
			return errors.WithStack(err)
		}

		// Match the tool name or its Windows form: GoReleaser names the
		// inner Windows binary "<name>.exe". The archive is platform-
		// specific, so only one of these is ever present.
		if header.Name == s.Tool.Name || header.Name == s.Tool.Name+".exe" {
			s.logger.Info("writing updated binary", "name", header.Name, "path", targetPath)

			return s.extractAndInstallBinary(tarReader, targetPath)
		}
	}

	// Fall-through means no matching binary was found. Returning nil here
	// would report a successful update while leaving the old binary in place.
	return errors.Wrapf(ErrBinaryNotInArchive, "no %q binary in archive", s.Tool.Name)
}

func (s *SelfUpdater) isDevelopmentVersion() bool {
	return s.CurrentVersion == "v0.0.0"
}

func (s *SelfUpdater) GetLatestRelease(ctx context.Context) (forge.Release, error) {
	var err error

	if s.NextRelease != nil {
		return s.NextRelease, nil
	}

	if s.version != "" {
		timeoutCtx, cancel := context.WithTimeout(ctx, updateTimeout)
		defer cancel()

		_, owner, repo := s.Tool.GetReleaseSource()

		s.NextRelease, err = s.releaseClient.GetReleaseByTag(timeoutCtx, owner, repo, s.version)
		if err != nil {
			return nil, s.explainRefusal(ctx, errors.Wrap(err, "failed to get release by tag"))
		}
	}

	if s.NextRelease == nil {
		timeoutCtx, cancel := context.WithTimeout(ctx, updateTimeout)
		defer cancel()

		_, owner, repo := s.Tool.GetReleaseSource()

		s.NextRelease, err = s.releaseClient.GetLatestRelease(timeoutCtx, owner, repo)
		if err != nil {
			return nil, s.explainRefusal(ctx, errors.Wrap(err, "failed to get latest release"))
		}
	}

	return s.NextRelease, nil
}

func (s *SelfUpdater) GetLatestVersionString(ctx context.Context) (string, error) {
	release, err := s.GetLatestRelease(ctx)
	if err != nil {
		return "", err
	}

	return ver.FormatVersionString(release.GetTagName(), true), nil
}

func (s *SelfUpdater) GetCurrentVersion() string {
	return s.CurrentVersion
}

// errDecompressedSizeExceeded is returned when the extracted binary grows past
// the decompressed-size bound — the guard against a gzip bomb whose compressed
// form fits under the download cap.
var errDecompressedSizeExceeded = errors.New("extracted binary exceeded the decompressed-size bound")

func (s *SelfUpdater) extractAndInstallBinary(tarReader *tar.Reader, targetPath string) error {
	tempFilePath := fmt.Sprintf("%s_", targetPath)

	tempFile, err := s.Fs.Create(tempFilePath)
	if err != nil {
		return errors.Wrap(err, "failed to create temp file")
	}

	// Remove the partial temp file on any failure path — including a
	// context-cancelled download (SIGINT mid-install) — so an interrupted
	// update never leaves a "<target>_" orphan behind.
	installed := false

	defer func() {
		if !installed {
			_ = tempFile.Close()
			_ = s.Fs.Remove(tempFilePath)
		}
	}()

	// Copy in chunks, tracking the cumulative decompressed size. The compressed
	// download is capped, but gzip expansion is not — so a crafted archive is
	// aborted here once it grows past the bound rather than filling the disk.
	bound := s.extractedBound()

	var written int64

	for {
		n, copyErr := io.CopyN(tempFile, tarReader, copyChunkSize)
		written += n

		if written > bound {
			return errors.Wrapf(errDecompressedSizeExceeded, "extracted binary exceeded %d bytes", bound)
		}

		if copyErr != nil {
			if errors.Is(copyErr, io.EOF) {
				break
			}

			return errors.Wrap(copyErr, "failed to copy binary chunk")
		}
	}

	if err := tempFile.Close(); err != nil {
		return errors.Wrap(err, "failed to close temp file")
	}

	// Chmod the temp file BEFORE the rename, so the installed binary is never
	// observable on disk in an intermediate execute-only state.
	if err := s.Fs.Chmod(tempFilePath, filePermExecutable); err != nil {
		return errors.Wrap(err, "failed to set executable mode")
	}

	if err := s.Fs.Rename(tempFilePath, targetPath); err != nil {
		return errors.Wrap(err, "failed to rename temp file to target")
	}

	installed = true

	return nil
}

// GetReleaseNotes retrieves the release notes for releases between the specified 'from' and 'to' versions (inclusive).
func (s *SelfUpdater) GetReleaseNotes(ctx context.Context, from string, to string) (string, error) {
	// Get all releases
	timeoutCtx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	_, owner, repo := s.Tool.GetReleaseSource()

	releases, err := s.releaseClient.ListReleases(timeoutCtx, owner, repo, releasesPerPage)
	if err != nil {
		return "", s.explainRefusal(ctx, errors.WithStack(err))
	}

	releaseNotes := s.filterReleaseNotes(releases, from, to)

	if len(releaseNotes) == 0 {
		return fmt.Sprintf("No release notes found between %s and %s", from, to), nil
	}

	slices.Reverse(releaseNotes)

	return fmt.Sprintf("# Release Notes from %s to %s\n\n%s", from, to, strings.Join(releaseNotes, "\n\n")), nil
}

// GetStructuredReleaseNotes retrieves release notes between two versions and
// returns them as a parsed Changelog. If an archive buffer is provided, it
// attempts to extract a bundled CHANGELOG.md first, falling back to per-release
// API calls when the archive contains no changelog.
func (s *SelfUpdater) GetStructuredReleaseNotes(ctx context.Context, from, to string, archive ...bytes.Buffer) (*changelog.Changelog, error) {
	// Try archive-bundled changelog first.
	if len(archive) > 0 {
		cl, err := changelog.ParseFromArchive(bytes.NewReader(archive[0].Bytes()))
		if err != nil {
			s.logger.Debug("failed to parse changelog from archive, falling back to API", "error", err)
		}

		if cl != nil {
			cl.FromVersion = from
			cl.ToVersion = to

			return cl, nil
		}
	}

	// Fall back to per-release API calls.
	raw, err := s.GetReleaseNotes(ctx, from, to)
	if err != nil {
		return nil, err
	}

	cl, err := changelog.Parse(raw)
	if err != nil {
		return nil, err
	}

	cl.FromVersion = from
	cl.ToVersion = to

	return cl, nil
}

func (s *SelfUpdater) filterReleaseNotes(releases []forge.Release, from, to string) []string {
	var releaseNotes []string

	fromVersion := ver.FormatVersionString(from, true)
	toVersion := ver.FormatVersionString(to, true)
	reachedToVersion := false

	for _, release := range releases {
		// Skip draft releases
		if release.GetDraft() {
			continue
		}

		releaseVersion := ver.FormatVersionString(release.GetTagName(), true)

		// Check if this is the 'to' version
		if ver.CompareVersions(releaseVersion, toVersion) == 0 {
			reachedToVersion = true
		}

		// If we haven't reached the 'to' version yet, skip versions newer than 'to'
		if !reachedToVersion && ver.CompareVersions(releaseVersion, toVersion) > 0 {
			continue
		}

		// Include if release is > from and <= to
		if shouldIncludeRelease(fromVersion, toVersion, releaseVersion) {
			releaseNote := fmt.Sprintf("# %s\n%s", release.GetTagName(), release.GetBody())
			releaseNotes = append(releaseNotes, releaseNote)
		}

		// Stop processing if we've gone beyond the 'from' version (older than 'from')
		if ver.CompareVersions(releaseVersion, fromVersion) < 0 {
			break
		}
	}

	return releaseNotes
}

func shouldIncludeRelease(fromVersion, toVersion, releaseVersion string) bool {
	// Compare versions - include releases between 'from' and 'to' (exclusive of 'from', inclusive of 'to')
	// CompareVersions returns: -1 if a < b, 0 if a == b, 1 if a > b
	fromCompare := ver.CompareVersions(fromVersion, releaseVersion)
	toCompare := ver.CompareVersions(releaseVersion, toVersion)

	// Include if release is > from and <= to
	return fromCompare < 0 && toCompare <= 0
}
