package osinfo

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersion_NonEmpty(t *testing.T) {
	t.Parallel()

	// Exercises the exported wrapper end-to-end on the host (the linux+success
	// path on CI); the result is always a non-empty label.
	assert.NotEmpty(t, Version())
}

func TestVersion_Core(t *testing.T) {
	t.Parallel()

	okReader := func() (string, error) {
		return "Linux version 6.8.0-106-generic (buildd@host) ...", nil
	}
	errReader := func() (string, error) { return "", errors.New("boom") }

	tests := []struct {
		name     string
		goos     string
		readProc func() (string, error)
		want     string
	}{
		{"non-linux falls back to goos", "darwin", errReader, "darwin"},
		{"non-linux windows", "windows", okReader, "windows"},
		{"linux read error falls back to goos", "linux", errReader, "linux"},
		{"linux happy path extracts kernel", "linux", okReader, "6.8.0-106-generic"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, version(tt.goos, tt.readProc))
		})
	}
}

func TestParseLinuxKernel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"standard line", "Linux version 6.8.0-106-generic (gcc) #1 SMP", "6.8.0-106-generic"},
		{"too few fields trims whole", "weird", "weird"},
		{"empty trims to empty", "   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, parseLinuxKernel(tt.in))
		})
	}
}
