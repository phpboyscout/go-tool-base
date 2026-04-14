package logger

import (
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
	assert.ErrorIs(t, err, ErrInvalidLevel)
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

// FuzzParseLevel verifies ParseLevel is panic-free for arbitrary input and
// that successful parses round-trip through Level.String().
//
// Run with: go test -fuzz=FuzzParseLevel ./pkg/logger/
func FuzzParseLevel(f *testing.F) {
	// Seed corpus: valid levels (any case), invalid strings, edge cases.
	seeds := []string{
		"debug", "info", "warn", "error", "fatal",
		"DEBUG", "Info", "WARN", "Error", "FATAL",
		"", " ", "  debug  ", "invalid", "DeBuG",
		"\x00", "null", "trace", "off", "0", "-1",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		lvl, err := ParseLevel(s)
		if err != nil {
			// Invalid input must always report ErrInvalidLevel.
			require.ErrorIs(t, err, ErrInvalidLevel)

			return
		}

		// Successful parses must round-trip through String().
		roundTripped, err := ParseLevel(lvl.String())
		require.NoError(t, err)
		require.Equal(t, lvl, roundTripped)
	})
}
