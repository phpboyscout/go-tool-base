package config

import (
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/spf13/viper"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func initContainer(fs afero.Fs, opts *containerOptions) *Container {
	l := opts.logger
	if l == nil {
		l = logger.NewNoop()
	}

	c := Container{
		ID:        "",
		viper:     viper.New(),
		logger:    l,
		observers: make([]Observable, 0),
	}

	c.viper.SetFs(fs)
	LoadEnv(fs, opts.logger)

	if opts.envPrefix != "" {
		c.viper.SetEnvPrefix(opts.envPrefix)
	}

	c.viper.AutomaticEnv()
	c.viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	c.viper.SetTypeByDefaultValue(true)

	return &c
}

// NewContainerFromViper creates a new Container from an existing Viper instance.
// If l is nil, a no-op logger is used.
func NewContainerFromViper(l logger.Logger, v *viper.Viper) *Container {
	if l == nil {
		l = logger.NewNoop()
	}

	return &Container{
		ID:        "viper",
		viper:     v,
		logger:    l,
		observers: make([]Observable, 0),
	}
}

// LoadFilesContainerWithSchema loads config files and validates against the schema.
// Returns an error wrapping all validation errors if the config is invalid.
// The schema can also be provided via WithSchema; if both are present, the option takes precedence.
func LoadFilesContainerWithSchema(fs afero.Fs, schema *Schema, opts ...ContainerOption) (Containable, error) {
	o := applyOptions(opts)
	if o.schema == nil {
		o.schema = schema
	}

	c, err := loadFilesContainer(fs, o)
	if err != nil {
		return nil, err
	}

	if c == nil {
		return nil, nil
	}

	c.SetSchema(o.schema)

	result := c.Validate(o.schema)
	if !result.Valid() {
		return nil, errors.New(result.Error())
	}

	return c, nil
}

// LoadFilesContainer loads configuration from files and returns a Containable.
// Config files are specified via WithConfigFiles. It returns an error if the
// first file specified does not exist.
func LoadFilesContainer(fs afero.Fs, opts ...ContainerOption) (Containable, error) {
	o := applyOptions(opts)

	c, err := loadFilesContainer(fs, o)
	if c == nil {
		return nil, err
	}

	return c, err
}

func loadFilesContainer(fs afero.Fs, o *containerOptions) (*Container, error) {
	if len(o.configFiles) == 0 {
		return nil, errors.New("no config files specified (use WithConfigFiles)")
	}

	exists, err := afero.Exists(fs, o.configFiles[0])
	if err != nil {
		return nil, errors.Wrap(err, "failed to check config file existence")
	}

	if !exists {
		return nil, nil
	}

	c := initContainer(fs, o)
	c.ID = o.configFiles[0]
	c.viper.SetConfigFile(o.configFiles[0])

	if err := c.viper.ReadInConfig(); err != nil {
		return nil, errors.Newf("failed to read config file %s: %w", o.configFiles[0], err)
	}

	for _, f := range o.configFiles[1:] {
		exists, err := afero.Exists(fs, f)
		if err != nil || !exists {
			continue
		}

		c.viper.SetConfigFile(f)

		if err := c.viper.MergeInConfig(); err != nil {
			o.logger.Warn(fmt.Sprintf("Failed to merge configuration file %s: %v", f, err))
		}
	}

	return c, nil
}

// NewFilesContainer initialises a configuration container to read files from the FS.
// Config files are specified via WithConfigFiles.
func NewFilesContainer(fs afero.Fs, opts ...ContainerOption) *Container {
	o := applyOptions(opts)
	c := initContainer(fs, o)

	if len(o.configFiles) > 0 {
		c.ID = o.configFiles[0]
		c.viper.SetConfigFile(o.configFiles[0])
		c.handleReadFileError(c.viper.ReadInConfig())
	}

	if len(o.configFiles) > 1 {
		for _, f := range o.configFiles[1:] {
			c.ID = fmt.Sprintf("%s;%s", c.ID, f)
			c.viper.SetConfigFile(f)
			c.handleReadFileError(c.viper.MergeInConfig())
		}

		c.logger.Info("Loaded Config")
		c.watchConfig()
	}

	return c
}

// NewReaderContainer initialises a configuration container to read config from io.Readers.
// Readers are specified via WithConfigReaders; format via WithConfigFormat.
func NewReaderContainer(fs afero.Fs, opts ...ContainerOption) *Container {
	o := applyOptions(opts)
	c := initContainer(fs, o)

	if o.configFormat != "" {
		c.viper.SetConfigType(o.configFormat)
	}

	if len(o.configReaders) > 0 {
		c.ID = "0"
		c.handleReadFileError(c.viper.ReadConfig(o.configReaders[0]))
	}

	if len(o.configReaders) > 1 {
		for i, f := range o.configReaders[1:] {
			c.ID = fmt.Sprintf("%s;%d", c.ID, i+1)
			c.handleReadFileError(c.viper.MergeConfig(f))
		}

		c.logger.Info("Loaded Config")
	}

	return c
}
