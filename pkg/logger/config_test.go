package logger_test

import (
	"testing"

	charmlog "charm.land/log/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := logger.DefaultConfig()

	assert.Equal(t, "info", cfg.Level)
	assert.Equal(t, "text", cfg.Format)
	assert.False(t, cfg.Timestamp)
	assert.False(t, cfg.Caller)
	require.NoError(t, cfg.Validate())
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     logger.Config
		wantErr bool
	}{
		{"valid text", logger.Config{Level: "info", Format: "text"}, false},
		{"valid json", logger.Config{Level: "debug", Format: "json"}, false},
		{"valid logfmt", logger.Config{Level: "error", Format: "logfmt"}, false},
		{"invalid level", logger.Config{Level: "loud", Format: "text"}, true},
		{"invalid format", logger.Config{Level: "info", Format: "xml"}, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	t.Parallel()

	defaults := logger.DefaultConfig()

	// An empty overlay leaves the defaults untouched.
	assert.Equal(t, defaults, logger.Merge(defaults, logger.Config{}))

	// Set overlay fields win; unset string fields keep the default.
	got := logger.Merge(defaults, logger.Config{Level: "debug", Timestamp: true})
	assert.Equal(t, "debug", got.Level)
	assert.Equal(t, "text", got.Format)
	assert.True(t, got.Timestamp)

	got = logger.Merge(defaults, logger.Config{Format: "json", Caller: true})
	assert.Equal(t, "info", got.Level)
	assert.Equal(t, "json", got.Format)
	assert.True(t, got.Caller)
}

func TestParseFormatter(t *testing.T) {
	t.Parallel()

	cases := map[string]logger.Formatter{
		"text":   logger.TextFormatter,
		"json":   logger.JSONFormatter,
		"logfmt": logger.LogfmtFormatter,
		"JSON":   logger.JSONFormatter, // case-insensitive
	}

	for input, want := range cases {
		got, err := logger.ParseFormatter(input)
		require.NoErrorf(t, err, "ParseFormatter(%q)", input)
		assert.Equalf(t, want, got, "ParseFormatter(%q)", input)
	}

	_, err := logger.ParseFormatter("xml")
	require.Error(t, err)
}

func TestFormatterString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "text", logger.TextFormatter.String())
	assert.Equal(t, "json", logger.JSONFormatter.String())
	assert.Equal(t, "logfmt", logger.LogfmtFormatter.String())
}

func TestConfigCharmOptions(t *testing.T) {
	t.Parallel()

	cfg := logger.Config{Level: "debug", Format: "json", Timestamp: true, Caller: true}

	var opts charmlog.Options
	for _, apply := range cfg.CharmOptions() {
		apply(&opts)
	}

	assert.Equal(t, charmlog.DebugLevel, opts.Level)
	assert.True(t, opts.ReportTimestamp)
	assert.True(t, opts.ReportCaller)
	assert.Equal(t, charmlog.JSONFormatter, opts.Formatter)
}

func TestConfigCharmOptions_InvalidLevelOmitsLevelOption(t *testing.T) {
	t.Parallel()

	// An unparseable level must not panic or emit a WithLevel option; the
	// charm default (InfoLevel) survives.
	cfg := logger.Config{Level: "bogus", Format: "text"}

	opts := charmlog.Options{Level: charmlog.InfoLevel}
	for _, apply := range cfg.CharmOptions() {
		apply(&opts)
	}

	assert.Equal(t, charmlog.InfoLevel, opts.Level)
}

func TestConfigFormatter(t *testing.T) {
	t.Parallel()

	assert.Equal(t, logger.JSONFormatter, logger.Config{Format: "json"}.Formatter())
	// An invalid or empty format falls back to the human-readable text format.
	assert.Equal(t, logger.TextFormatter, logger.Config{Format: "bogus"}.Formatter())
	assert.Equal(t, logger.TextFormatter, logger.Config{}.Formatter())
}
