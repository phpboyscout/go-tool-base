package utils

import (
	"io/fs"
	"os"
)

// IsInteractive returns true if stdin is a terminal (not piped or redirected).
func IsInteractive() bool {
	return isCharDevice(os.Stdin.Stat())
}

// isCharDevice reports whether the stat result describes a character device
// (a terminal). A stat error resolves to "not interactive". Split out so both
// arms are testable without depending on the real os.Stdin.
func isCharDevice(info fs.FileInfo, err error) bool {
	if err != nil {
		return false
	}

	return (info.Mode() & os.ModeCharDevice) != 0
}
