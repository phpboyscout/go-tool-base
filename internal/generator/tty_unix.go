//go:build !windows

package generator

import "os"

// controllingTerminalAvailable reports whether the process can open its
// controlling terminal (/dev/tty) — the device huh/bubbletea drives for an
// interactive prompt. A stat of stdin is not enough on its own: a char-device
// stdin (e.g. /dev/null in some containers) can coexist with no attachable
// controlling terminal.
func controllingTerminalAvailable() bool {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}

	_ = f.Close()

	return true
}
