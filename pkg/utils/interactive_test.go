package utils

import (
	"errors"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestIsInteractive exercises IsInteractive. The function's return value
// depends on whether os.Stdin is a character device, which differs between CI
// (stdin is a pipe -> false) and an attached terminal (stdin is a TTY ->
// true). We therefore assert only that the os.Stdin.Stat path is executed and
// that the result agrees with a direct inspection of stdin's mode, rather than
// pinning a fixed boolean that would flake across environments.
func TestIsInteractive(t *testing.T) {
	got := IsInteractive()
	assert.IsType(t, false, got)

	info, err := os.Stdin.Stat()
	if err != nil {
		// Stat failed: IsInteractive must report the non-interactive arm.
		assert.False(t, got)

		return
	}

	want := (info.Mode() & os.ModeCharDevice) != 0
	assert.Equal(t, want, got, "IsInteractive must match stdin's char-device mode")
}

// fakeFileInfo is a minimal fs.FileInfo whose Mode is controllable.
type fakeFileInfo struct {
	mode fs.FileMode
}

func (f fakeFileInfo) Name() string       { return "fake" }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }

// TestIsCharDevice covers both arms of the char-device predicate deterministically.
func TestIsCharDevice(t *testing.T) {
	t.Parallel()

	assert.False(t, isCharDevice(nil, errors.New("stat failed")),
		"a stat error is not interactive")
	assert.True(t, isCharDevice(fakeFileInfo{mode: os.ModeCharDevice}, nil),
		"a character device is interactive")
	assert.False(t, isCharDevice(fakeFileInfo{mode: 0}, nil),
		"a regular file (pipe/redirect) is not interactive")
}
