package config

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
)

func initContainer(l *log.Logger, fs afero.Fs) *Container {
	c := Container{
		ID:        "",
		viper:     viper.New(),
		logger:    l,
		observers: make([]Observable, 0),
	}

	c.viper.SetFs(fs)
	LoadEnv(fs, l)
	c.viper.AutomaticEnv()
	c.viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	c.viper.SetTypeByDefaultValue(true)

	return &c
}

// NewContainerFromViper creates a new Container from an existing Viper instance.
func NewContainerFromViper(l *log.Logger, v *viper.Viper) *Container {
	return &Container{
		ID:        "viper",
		viper:     v,
		logger:    l,
		observers: make([]Observable, 0),
	}
}

// LoadFilesContainer loads configuration from files and returns a Containable.
// It returns an error if the first file specified does not exist.
func LoadFilesContainer(l *log.Logger, fs afero.Fs, configFiles ...string) (Containable, error) {
	if len(configFiles) == 0 {
		return nil, errors.New("no config files specified")
	}

	exists, err := afero.Exists(fs, configFiles[0])
	if err != nil || !exists {
		return nil, err
	}

	c := initContainer(l, fs)
	c.ID = configFiles[0]
	c.viper.SetConfigFile(configFiles[0])

	if err := c.viper.ReadInConfig(); err != nil {
		return nil, errors.Newf("failed to read config file %s: %w", configFiles[0], err)
	}

	for _, f := range configFiles[1:] {
		exists, err := afero.Exists(fs, f)
		if err != nil || !exists {
			continue
		}

		c.viper.SetConfigFile(f)

		if err := c.viper.MergeInConfig(); err != nil {
			l.Warn(fmt.Sprintf("Failed to merge configuration file %s: %v", f, err))
		}
	}

	return c, nil
}

// NewFilesContainer Initialise configuration container to read files from the FS.
func NewFilesContainer(l *log.Logger, fs afero.Fs, configFiles ...string) *Container {
	c := initContainer(l, fs)

	if len(configFiles) > 0 {
		c.ID = configFiles[0]
		c.viper.SetConfigFile(configFiles[0])
		c.handleReadFileError(c.viper.ReadInConfig())
	}

	if len(configFiles) > 1 {
		for _, f := range configFiles[1:] {
			c.ID = fmt.Sprintf("%s;%s", c.ID, f)
			c.viper.SetConfigFile(f)
			c.handleReadFileError(c.viper.MergeInConfig())
		}

		c.logger.Info("Loaded Config")
		c.watchConfig()
	}

	return c
}

// NewReaderContainer Initialise configuration container to read config from ioReader.
func NewReaderContainer(l *log.Logger, format string, configReaders ...io.Reader) *Container {
	c := initContainer(l, afero.NewOsFs())

	c.viper.SetConfigType(format)

	if len(configReaders) > 0 {
		c.ID = "0"
		c.handleReadFileError(c.viper.ReadConfig(configReaders[0]))
	}

	if len(configReaders) > 1 {
		for i, f := range configReaders[1:] {
			c.ID = fmt.Sprintf("%s;%d", c.ID, i+1)
			c.handleReadFileError(c.viper.MergeConfig(f))
		}

		c.logger.Info("Loaded Config")
	}

	return c
}
