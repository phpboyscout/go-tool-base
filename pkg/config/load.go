package config

import (
	"bytes"
	"io"
	"io/fs"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/subosito/gotenv"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

var (
	ErrNoFilesFound = errors.Newf("no configuration files found please run init, or provide a config file using the --config flag")
)

// LoadEnv loads environment variables from a .env file if it exists.
func LoadEnv(fs afero.Fs, logger logger.Logger) {
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

	logger.Debug("Loading environment variables from .env")

	if err := gotenv.Apply(f); err != nil {
		logger.Warn("Failed to format merged config", "error", err)
	}
}

// Load reads configuration from the first available file in paths.
// Returns ErrNoFilesFound if no files exist and allowEmptyConfig is false.
func Load(paths []string, fs afero.Fs, logger logger.Logger, allowEmptyConfig bool) (Containable, error) {
	logger.Debug("Loading configuration")

	loadable := []string{}

	for _, path := range paths {
		if _, err := fs.Stat(path); err == nil {
			loadable = append(loadable, path)
		}
	}

	if !allowEmptyConfig && len(loadable) == 0 {
		return nil, errors.WithStack(ErrNoFilesFound)
	}

	return NewFilesContainer(logger, fs, loadable...), nil
}

// LoadEmbed reads configuration from embedded filesystem assets and merges them.
func LoadEmbed(paths []string, assets fs.FS, logger logger.Logger) (Containable, error) {
	logger.Debug("Loading embedded configuration")

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

	return NewReaderContainer(logger, "yaml", configs...), nil
}
