//go:build windows

package generator

import "os"

// controllingTerminalAvailable reports whether the process can open the Windows
// console input device (CONIN$) — the terminal huh/bubbletea drives for an
// interactive prompt.
func controllingTerminalAvailable() bool {
	f, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return false
	}

	_ = f.Close()

	return true
}
