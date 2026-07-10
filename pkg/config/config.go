package config

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
)

func initContainer(fs afero.Fs, opts *containerOptions) *Container {
	l := opts.logger
	if l == nil {
		l = slog.New(slog.DiscardHandler)
	}

	debounce := opts.reloadDebounce
	if debounce <= 0 {
		debounce = DefaultReloadDebounce
	}

	c := Container{
		ID:             "",
		viper:          newResolverViper(fs, opts.envPrefix),
		logger:         l,
		observers:      make([]Observable, 0),
		fs:             fs,
		configFiles:    append([]string(nil), opts.configFiles...),
		reloadDebounce: debounce,
	}

	LoadEnv(fs, opts.logger)

	return &c
}

// newResolverViper builds a viper instance configured identically to the
// container's live viper (afero filesystem, env prefix, automatic env, key
// replacer, type-by-default). Used both for the initial load and for building
// reload candidates so a swapped-in candidate behaves identically.
func newResolverViper(fs afero.Fs, envPrefix string) *viper.Viper {
	v := viper.New()
	v.SetFs(fs)

	if envPrefix != "" {
		v.SetEnvPrefix(envPrefix)
	}

	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.SetTypeByDefaultValue(true)

	return v
}

// NewContainerFromViper creates a new Container from an existing Viper instance.
// If l is nil, a no-op logger is used.
func NewContainerFromViper(l *slog.Logger, v *viper.Viper) *Container {
	if l == nil {
		l = slog.New(slog.DiscardHandler)
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

	// Start the watcher only after construction (read, merge, schema) is
	// complete so the reload goroutine never observes a half-built container.
	c.watchConfig()

	return c, nil
}

// LoadFilesContainer loads configuration from files and returns a Containable.
// Config files are specified via WithConfigFiles. It returns
// ErrConfigFileNotFound if the first file specified does not exist; it never
// returns a nil Containable with a nil error.
func LoadFilesContainer(fs afero.Fs, opts ...ContainerOption) (Containable, error) {
	o := applyOptions(opts)

	c, err := loadFilesContainer(fs, o)
	if err != nil {
		return nil, err
	}

	if c == nil {
		return nil, errors.WithStack(ErrConfigFileNotFound)
	}

	// Start the watcher only after construction completes.
	c.watchConfig()

	return c, nil
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
	}

	// Watch every file-backed container, including single-file ones, only
	// after construction completes (D5). No-op when there are no files.
	c.watchConfig()

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
