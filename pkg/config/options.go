package config

import (
	"io"
	"log/slog"
	"time"
)

// DefaultReloadDebounce is the default coalescing window applied to the burst
// of filesystem events a single save produces before a hot-reload is performed.
// The relatively generous default tolerates slow or networked filesystems.
const DefaultReloadDebounce = 250 * time.Millisecond

// ContainerOption configures optional behavior for config containers.
type ContainerOption func(*containerOptions)

type containerOptions struct {
	logger         *slog.Logger
	envPrefix      string
	configFiles    []string
	configFormat   string
	configReaders  []io.Reader
	schema         *Schema
	reloadDebounce time.Duration
}

// WithLogger sets the logger for the config container. When not provided,
// a no-op logger is used.
func WithLogger(l *slog.Logger) ContainerOption {
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

// WithReloadDebounce sets the coalescing window for hot-reload events. A single
// file save typically emits a burst of filesystem events (write, rename,
// chmod); the container waits for the burst to settle for the given duration
// before performing a reload. Values <= 0 fall back to DefaultReloadDebounce.
func WithReloadDebounce(d time.Duration) ContainerOption {
	return func(o *containerOptions) {
		o.reloadDebounce = d
	}
}

func applyOptions(opts []ContainerOption) *containerOptions {
	o := &containerOptions{}
	for _, opt := range opts {
		opt(o)
	}

	if o.logger == nil {
		o.logger = slog.New(slog.DiscardHandler)
	}

	if o.reloadDebounce <= 0 {
		o.reloadDebounce = DefaultReloadDebounce
	}

	return o
}
