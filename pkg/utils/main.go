package utils

import (
	"io/fs"
	"os"
)

// IsInteractive returns true if the process's stdin is a terminal (not piped
// or redirected).
//
// Superseded by the invocation's own streams, p.GetIO().Interactive() (spec
// 0198): this reads the process's stdin, which is not the same thing under a
// test or an embedding. Nothing in pkg/ calls it any more; it carries the
// Deprecated marker once the gtb CLI has moved off it, which waits on a
// framework release, and goes after that.
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
