package output

import (
	"os"

	"github.com/charmbracelet/x/term"
)

// IsInteractive returns true if stdout is a TTY and CI mode is not active.
// Used by progress helpers to decide between animated TUI output and plain
// text fallback.
func IsInteractive() bool {
	if os.Getenv("CI") == "true" {
		return false
	}

	return term.IsTerminal(os.Stdout.Fd())
}
