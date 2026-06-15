package config

import (
	"bytes"
	"io"
	"io/fs"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/subosito/gotenv"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

var (
	ErrNoFilesFound = errors.Newf("no configuration files found please run init, or provide a config file using the --config flag")

	// ErrConfigFileNotFound is returned by LoadFilesContainer when the first
	// configured file does not exist. Callers can branch on this with
	// errors.Is to fall back to defaults (e.g. on first run).
	ErrConfigFileNotFound = errors.Newf("config file not found")
)

// LoadEnv loads environment variables from a .env file if it exists.
// A nil log is tolerated — it falls back to a no-op logger so callers
// that have not yet wired logging cannot trigger a nil-pointer panic.
func LoadEnv(fs afero.Fs, log logger.Logger) {
	if log == nil {
		log = logger.NewNoop()
	}

	dotEnv := ".env"

	exists, err := afero.Exists(fs, dotEnv)
	if err != nil || !exists {
		return
	}

	f, err := fs.Open(dotEnv)
	if err != nil {
		return
	}

	defer func() { _ = f.Close() }()

	log.Debug("Loading environment variables from .env")

	if err := gotenv.Apply(f); err != nil {
		log.Warn("Failed to apply .env environment variables", "error", err)
	}
}

// Load reads configuration from the first available file in paths.
// Returns ErrNoFilesFound if no files exist and allowEmptyConfig is false.
func Load(paths []string, fs afero.Fs, allowEmptyConfig bool, opts ...ContainerOption) (Containable, error) {
	o := applyOptions(opts)
	o.logger.Debug("Loading configuration")

	loadable := []string{}

	for _, path := range paths {
		if _, err := fs.Stat(path); err == nil {
			loadable = append(loadable, path)
		}
	}

	if !allowEmptyConfig && len(loadable) == 0 {
		return nil, errors.WithStack(ErrNoFilesFound)
	}

	return NewFilesContainer(fs, append(opts, WithConfigFiles(loadable...))...), nil
}

// LoadEmbed reads configuration from embedded filesystem assets and merges them.
func LoadEmbed(paths []string, assets fs.FS, opts ...ContainerOption) (Containable, error) {
	o := applyOptions(opts)
	o.logger.Debug("Loading embedded configuration")

	configs := []io.Reader{}

	for _, path := range paths {
		configFile, err := assets.Open(path)
		if err != nil {
			return nil, errors.Wrap(err, "failed to open embedded config file "+path)
		}

		defer func() { _ = configFile.Close() }()

		config, err := io.ReadAll(configFile)
		if err != nil {
			return nil, errors.Wrap(err, "failed to read embedded config file "+path)
		}

		configs = append(configs, bytes.NewReader(config))
	}

	// Default to YAML but let the caller override the format via their
	// own WithConfigFormat: the default goes first so caller-supplied
	// opts (applied later, last-wins) take precedence. WithConfigReaders
	// is appended last because the readers are intrinsic to this call,
	// not caller-overridable.
	// intrinsicOpts is the count of options LoadEmbed always supplies
	// itself (the default format + the reader source), bracketing the
	// caller's opts.
	const intrinsicOpts = 2

	merged := make([]ContainerOption, 0, len(opts)+intrinsicOpts)
	merged = append(merged, WithConfigFormat("yaml"))
	merged = append(merged, opts...)
	merged = append(merged, WithConfigReaders(configs...))

	return NewReaderContainer(afero.NewOsFs(), merged...), nil
}
