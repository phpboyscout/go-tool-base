package logger

import "fmt"

// Config is the typed, config-system-agnostic construction configuration for a
// logger. A host application (GTB or any other) unmarshals its config section
// into this struct and passes it to the package; the package never imports a
// config system to obtain it.
//
// Level and Format are applied both at construction (via CharmOptions) and at
// runtime (via SetLevel / SetFormatter). Timestamp and Caller are
// construction-time only — the Logger interface exposes no runtime setter for
// them — so a host that wants them config-driven must build its logger from
// this Config (e.g. logger.NewCharm(w, cfg.CharmOptions()...)).
type Config struct {
	Level     string `mapstructure:"level"     json:"level"     yaml:"level"`
	Format    string `mapstructure:"format"    json:"format"    yaml:"format"`
	Timestamp bool   `mapstructure:"timestamp" json:"timestamp" yaml:"timestamp"`
	Caller    bool   `mapstructure:"caller"    json:"caller"    yaml:"caller"`
}

// DefaultConfig returns the package defaults: info level, human-readable text
// format, no timestamp, no caller — matching a freshly constructed NewCharm
// logger.
func DefaultConfig() Config {
	return Config{
		Level:  InfoLevel.String(),
		Format: TextFormatter.String(),
	}
}

// Validate reports whether the Level and Format strings are recognised.
func (c Config) Validate() error {
	if _, err := ParseLevel(c.Level); err != nil {
		return fmt.Errorf("log level: %w", err)
	}

	if _, err := ParseFormatter(c.Format); err != nil {
		return fmt.Errorf("log format: %w", err)
	}

	return nil
}

// Merge overlays a decoded Config onto a base (usually DefaultConfig). Empty
// string fields in the overlay preserve the base value; boolean fields take the
// overlay value directly (the package defaults are false, so an unset overlay
// boolean is indistinguishable from an explicit false and both keep the
// default).
func Merge(base, overlay Config) Config {
	if overlay.Level != "" {
		base.Level = overlay.Level
	}

	if overlay.Format != "" {
		base.Format = overlay.Format
	}

	base.Timestamp = overlay.Timestamp
	base.Caller = overlay.Caller

	return base
}

// Formatter returns the parsed Format, falling back to TextFormatter when the
// Format string is empty or unrecognised.
func (c Config) Formatter() Formatter {
	if f, err := ParseFormatter(c.Format); err == nil {
		return f
	}

	return TextFormatter
}

// CharmOptions renders the Config as options for NewCharm. An unparseable Level
// is skipped so the charm default (InfoLevel) survives rather than being reset
// to an arbitrary value.
func (c Config) CharmOptions() []CharmOption {
	opts := []CharmOption{
		WithTimestamp(c.Timestamp),
		WithCaller(c.Caller),
		WithFormatter(c.Formatter()),
	}

	if level, err := ParseLevel(c.Level); err == nil {
		opts = append(opts, WithLevel(level))
	}

	return opts
}
