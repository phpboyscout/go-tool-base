package logger

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  Level
	}{
		{"debug", DebugLevel},
		{"DEBUG", DebugLevel},
		{"info", InfoLevel},
		{"Info", InfoLevel},
		{"warn", WarnLevel},
		{"WARN", WarnLevel},
		{"error", ErrorLevel},
		{"fatal", FatalLevel},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseLevel(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseLevel_Invalid(t *testing.T) {
	_, err := ParseLevel("invalid")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidLevel))
}

func TestLevel_String(t *testing.T) {
	tests := []struct {
		level Level
		want  string
	}{
		{DebugLevel, "debug"},
		{InfoLevel, "info"},
		{WarnLevel, "warn"},
		{ErrorLevel, "error"},
		{FatalLevel, "fatal"},
		{Level(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.level.String())
		})
	}
}

func TestLevel_RoundTrip(t *testing.T) {
	for _, level := range []Level{DebugLevel, InfoLevel, WarnLevel, ErrorLevel, FatalLevel} {
		parsed, err := ParseLevel(level.String())
		require.NoError(t, err)
		assert.Equal(t, level, parsed)
	}
}
