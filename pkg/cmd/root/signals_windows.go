//go:build windows

package root

import (
	"os"
	"syscall"
)

// subscribedSignals is what the root drains on. Windows has no SIGHUP to take
// and no inherited ignore to preserve.
func subscribedSignals(func(os.Signal) bool) []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

// terminateBySignal exits with code: Windows cannot end a process by a signal.
func terminateBySignal(_ os.Signal, code int) {
	os.Exit(code)
}
