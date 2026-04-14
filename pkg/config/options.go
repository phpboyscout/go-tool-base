package config

import (
	"io"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

// ContainerOption configures optional behavior for config containers.
type ContainerOption func(*containerOptions)

type containerOptions struct {
	logger        logger.Logger
	envPrefix     string
	configFiles   []string
	configFormat  string
	configReaders []io.Reader
	schema        *Schema
}

// WithLogger sets the logger for the config container. When not provided,
// a no-op logger is used.
func WithLogger(l logger.Logger) ContainerOption {
	return func(o *containerOptions) {
		o.logger = l
	}
}

// WithEnvPrefix sets the environment variable prefix for automatic env binding.
// When set to "GTB", the config key "ai.provider" resolves from the
// environment variable "GTB_AI_PROVIDER". An empty string disables prefixing
// (the default, preserving backward compatibility).
func WithEnvPrefix(prefix string) ContainerOption {
	return func(o *containerOptions) {
		o.envPrefix = prefix
	}
}

// WithConfigFiles specifies one or more config file paths to load. The first
// file is treated as the primary config; subsequent files are merged in order.
func WithConfigFiles(files ...string) ContainerOption {
	return func(o *containerOptions) {
		o.configFiles = files
	}
}

// WithConfigFormat sets the config format (e.g. "yaml", "json") for
// reader-based config loading.
func WithConfigFormat(format string) ContainerOption {
	return func(o *containerOptions) {
		o.configFormat = format
	}
}

// WithConfigReaders provides one or more io.Readers as config sources.
// The first reader is the primary config; subsequent readers are merged.
// Requires WithConfigFormat to be set.
func WithConfigReaders(readers ...io.Reader) ContainerOption {
	return func(o *containerOptions) {
		o.configReaders = readers
	}
}

// WithSchema attaches a validation schema to the container.
func WithSchema(schema *Schema) ContainerOption {
	return func(o *containerOptions) {
		o.schema = schema
	}
}

func applyOptions(opts []ContainerOption) *containerOptions {
	o := &containerOptions{}
	for _, opt := range opts {
		opt(o)
	}

	if o.logger == nil {
		o.logger = logger.NewNoop()
	}

	return o
}
