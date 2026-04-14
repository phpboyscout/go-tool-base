package setup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/phpboyscout/go-tool-base/pkg/changelog"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/vcs"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
	ver "github.com/phpboyscout/go-tool-base/pkg/version"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

const (
	// copyChunkSize is the size of chunks when copying files to prevent decompression bomb attacks.
	copyChunkSize = 1024
	// filePermExecutable is the permission mode for executable files (0111).
	filePermExecutable = 0o111
	// releasesPerPage is the number of releases to fetch per API page.
	releasesPerPage = 100
	// defaultCheckInterval is the default time interval for update checks.
	defaultCheckInterval = 24 * time.Hour
	// updateTimeout is the timeout for update operations (5 minutes).
	updateTimeout = 3 * time.Minute
)

type timeSinceKey string

const (
	UpdatedKey = timeSinceKey("updated")
	CheckedKey = timeSinceKey("checked")
)

var (
	osExecutable = os.Executable
	execLookPath = exec.LookPath
)

// SelfUpdater manages checking for and applying tool updates.
type SelfUpdater struct {
	Tool           props.Tool
	force          bool
	version        string
	logger         logger.Logger
	releaseClient  release.Provider
	CurrentVersion string
	NextRelease    release.Release
	Fs             afero.Fs
}

// GetTimeSinceLast returns the duration since the last update check or update.
func GetTimeSinceLast(fs afero.Fs, name string, status timeSinceKey) time.Duration {
	defaultConfigDir := GetDefaultConfigDir(fs, name)
	lastSinceFile := filepath.Join(defaultConfigDir, fmt.Sprintf("last_%s", status))

	if fi, err := fs.Stat(defaultConfigDir); err == nil && fi.IsDir() {
		if fi, err := fs.Stat(lastSinceFile); err == nil {
			return time.Since(fi.ModTime())
		}
	}

	return defaultCheckInterval
}

// SetTimeSinceLast records the current time as the last check or update timestamp.
func SetTimeSinceLast(fs afero.Fs, name string, status timeSinceKey) error {
	defaultConfigDir := GetDefaultConfigDir(fs, name)
	lastSinceFile := filepath.Join(defaultConfigDir, fmt.Sprintf("last_%s", status))

	if _, err := fs.Stat(lastSinceFile); os.IsNotExist(err) {
		f, err := fs.Create(lastSinceFile)
		if err != nil {
			return errors.Wrap(err, "failed to create timestamp file")
		}

		defer func() { _ = f.Close() }()

		return nil
	}

	return fs.Chtimes(lastSinceFile, time.Now(), time.Now())
}

// NewOfflineUpdater creates a SelfUpdater configured for file-based updates
// that do not require a VCS client or network access.
func NewOfflineUpdater(tool props.Tool, log logger.Logger, fs afero.Fs) *SelfUpdater {
	return &SelfUpdater{
		logger: log,
		Tool:   tool,
		Fs:     fs,
	}
}

// NewUpdater creates a SelfUpdater configured with the tools release source.
func NewUpdater(p *props.Props, version string, force bool) (*SelfUpdater, error) {
	if p.Config == nil {
		return nil, errors.New("configuration is not loaded")
	}

	vcsProvider, _, _ := p.Tool.GetReleaseSource()
	if p.Config.IsSet("vcs.provider") {
		vcsProvider = strings.ToLower(p.Config.GetString("vcs.provider"))
	}

	if p.Tool.ReleaseSource.Private {
		if err := requireReleaseToken(vcsProvider, p); err != nil {
			return nil, err
		}
	}

	factory, err := release.Lookup(vcsProvider)
	if err != nil {
		return nil, err
	}

	sourceCfg := release.ReleaseSourceConfig{
		Type:    p.Tool.ReleaseSource.Type,
		Host:    p.Tool.ReleaseSource.Host,
		Owner:   p.Tool.ReleaseSource.Owner,
		Repo:    p.Tool.ReleaseSource.Repo,
		Private: p.Tool.ReleaseSource.Private,
		Params:  p.Tool.ReleaseSource.Params,
	}

	releaseClient, err := factory(sourceCfg, p.Config)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &SelfUpdater{
		force:          force,
		version:        version,
		logger:         p.Logger,
		Tool:           p.Tool,
		releaseClient:  releaseClient,
		CurrentVersion: ver.FormatVersionString(p.Version.GetVersion(), true),
		Fs:             p.FS,
	}, nil
}

// requireReleaseToken returns an error if no authentication token is available
// for the given VCS provider. Used to give a clear error for private repositories
// before attempting an unauthenticated API call that would fail with a 401.
func requireReleaseToken(vcsProvider string, p *props.Props) error {
	var (
		cfg         = p.Config.Sub(vcsProvider)
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

	if vcs.ResolveToken(cfg, fallbackEnv) == "" {
		return errors.WithHint(
			errors.Newf("no %s token available for private repository", strings.ToUpper(vcsProvider)),
			fmt.Sprintf("Set %s or configure %s.auth.env in your config to enable updates", fallbackEnv, vcsProvider),
		)
	}

	return nil
}

// SkipUpdateCheck returns true if the update check should be skipped for this invocation.
func SkipUpdateCheck(fs afero.Fs, name string, cmd *cobra.Command) bool {
	skippableCommands := []string{"update", "auth", "init", "version"}
	if slices.Contains(skippableCommands, cmd.Use) {
		return true
	}

	timeSinceChecked := GetTimeSinceLast(fs, name, CheckedKey)

	return timeSinceChecked < 24*time.Hour
}

// IsLatestVersion checks if the current running binary is the latest version.
func (s *SelfUpdater) IsLatestVersion(ctx context.Context) (bool, string, error) {
	if s.isDevelopmentVersion() {
		return true, "you are using a development version and must use the'update' command with the --force flag to update", nil
	}

	latestVersion, err := s.GetLatestVersionString(ctx)
	if err != nil || latestVersion == "" {
		return false, "failed to check for latest version", errors.WithStack(err)
	}

	switch ver.CompareVersions(s.CurrentVersion, latestVersion) {
	case -1: // need an upgrade
		return false, fmt.Sprintf("newer version found. Please upgrade to %s.  Run the `update`", latestVersion), nil
	case 1: // a future version somehow
		return false, fmt.Sprintf("your tardis travelled too far into the future. You are on %s, please downgrade to %s", s.CurrentVersion, latestVersion), nil
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

	if skip := s.shouldSkipUpdate(ctx); skip {
		return targetPath, nil
	}

	latestVersion, err := s.GetLatestRelease(ctx)
	if err != nil {
		return targetPath, errors.WithStack(err)
	}

	s.logger.Infof("targetting version %s", latestVersion.GetName())

	asset, err := s.findReleaseAsset(latestVersion)
	if err != nil {
		return targetPath, err
	}

	s.logger.Info("downloading", "name", asset.GetName(), "url", asset.GetBrowserDownloadURL())

	file, err := s.DownloadAsset(ctx, asset)
	if err != nil {
		return targetPath, errors.WithStack(err)
	}

	defer func() {
		_ = SetTimeSinceLast(s.Fs, s.Tool.Name, UpdatedKey)
		_ = SetTimeSinceLast(s.Fs, s.Tool.Name, CheckedKey)
	}()

	return targetPath, s.extract(file, targetPath)
}

// UpdateFromFile installs a binary from a local .tar.gz file.
// If a .sha256 sidecar file exists at filePath+".sha256", the checksum
// is verified before extraction. Returns the installation target path.
func (s *SelfUpdater) UpdateFromFile(filePath string) (string, error) {
	targetPath, err := s.resolveTargetPath()
	if err != nil {
		return "", err
	}

	data, err := afero.ReadFile(s.Fs, filePath)
	if err != nil {
		return "", errors.Wrap(err, "failed to read update file")
	}

	sidecarPath := filePath + ".sha256"
	if exists, _ := afero.Exists(s.Fs, sidecarPath); exists {
		if err := VerifyChecksum(s.Fs, sidecarPath, data); err != nil {
			return "", err
		}

		s.logger.Info("checksum verified", "file", filePath)
	} else {
		s.logger.Warn("no checksum sidecar found, skipping verification", "expected", sidecarPath)
	}

	var file bytes.Buffer

	file.Write(data)

	defer func() {
		_ = SetTimeSinceLast(s.Fs, s.Tool.Name, UpdatedKey)
	}()

	return targetPath, s.extract(file, targetPath)
}

func (s *SelfUpdater) resolveTargetPath() (string, error) {
	targetPath, err := osExecutable()
	if err != nil {
		return "", errors.WithStack(err)
	}

	execPath, err := execLookPath(s.Tool.Name)
	if err != nil {
		return "", errors.WithStack(err)
	}

	if targetPath != execPath {
		err := huh.NewSelect[string]().
			Title("Multiple installations detected, Please select which to update").
			Options(
				huh.NewOption(targetPath, targetPath),
				huh.NewOption(execPath, execPath),
			).
			Value(&targetPath).
			Run()
		if err != nil {
			return "", errors.WithStack(err)
		}
	}

	return targetPath, nil
}

func (s *SelfUpdater) shouldSkipUpdate(ctx context.Context) bool {
	isLatestVersion, message, err := s.IsLatestVersion(ctx)
	if err != nil {
		s.logger.Warn("failed to check for latest version", "error", err)

		return true
	}

	if isLatestVersion && !s.force {
		s.logger.Warn(message)

		return true
	}

	return false
}

func (s *SelfUpdater) findReleaseAsset(rel release.Release) (release.ReleaseAsset, error) {
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
func (s *SelfUpdater) DownloadAsset(ctx context.Context, asset release.ReleaseAsset) (bytes.Buffer, error) {
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

	i, err := io.Copy(&file, rc)
	if err != nil {
		return file, errors.WithStack(err)
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

		if header.Name == s.Tool.Name {
			s.logger.Infof("writing updated %s to %s", header.Name, targetPath)

			return s.extractAndInstallBinary(tarReader, targetPath)
		}
	}

	return nil
}

func (s *SelfUpdater) isDevelopmentVersion() bool {
	return s.CurrentVersion == "v0.0.0"
}

func (s *SelfUpdater) GetLatestRelease(ctx context.Context) (release.Release, error) {
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
			return nil, errors.Wrap(err, "failed to get release by tag")
		}
	}

	if s.NextRelease == nil {
		timeoutCtx, cancel := context.WithTimeout(ctx, updateTimeout)
		defer cancel()

		_, owner, repo := s.Tool.GetReleaseSource()

		s.NextRelease, err = s.releaseClient.GetLatestRelease(timeoutCtx, owner, repo)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get latest release")
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

func (s *SelfUpdater) extractAndInstallBinary(tarReader *tar.Reader, targetPath string) error {
	tempFilePath := fmt.Sprintf("%s_", targetPath)

	tempFile, err := s.Fs.Create(tempFilePath)
	if err != nil {
		return errors.Wrap(err, "failed to create temp file")
	}

	// Copy file in chunks to help mitigate a decompression bomb attack
	for {
		_, err := io.CopyN(tempFile, tarReader, copyChunkSize)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return errors.Wrap(err, "failed to copy binary chunk")
		}
	}

	if err := tempFile.Close(); err != nil {
		return errors.Wrap(err, "failed to close temp file")
	}

	if err := s.Fs.Rename(tempFilePath, targetPath); err != nil {
		return errors.Wrap(err, "failed to rename temp file to target")
	}

	return s.Fs.Chmod(targetPath, filePermExecutable)
}

// GetReleaseNotes retrieves the release notes for releases between the specified 'from' and 'to' versions (inclusive).
func (s *SelfUpdater) GetReleaseNotes(ctx context.Context, from string, to string) (string, error) {
	// Get all releases
	timeoutCtx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	_, owner, repo := s.Tool.GetReleaseSource()

	releases, err := s.releaseClient.ListReleases(timeoutCtx, owner, repo, releasesPerPage)
	if err != nil {
		return "", errors.WithStack(err)
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

	cl := changelog.Parse(raw)
	cl.FromVersion = from
	cl.ToVersion = to

	return cl, nil
}

func (s *SelfUpdater) filterReleaseNotes(releases []release.Release, from, to string) []string {
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
