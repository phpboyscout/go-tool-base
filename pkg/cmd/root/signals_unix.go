//go:build !windows

package root

import (
	"os"
	"os/signal"
	"syscall"
	"time"
)

// signalDeliveryWait bounds how long terminateBySignal waits for its own
// re-raised signal before falling back to an exit. A self-sent signal needs the
// process to yield before it lands: none landed with no wait, all within 1ms
// (spec 0207, measured 2026-10-09).
const signalDeliveryWait = 100 * time.Millisecond

// subscribedSignals is what the root drains on. SIGTERM is always taken, since
// the runtime never leaves it ignored; SIGINT and SIGHUP are left alone when the
// process started with them ignored, as a background job or nohup does, because
// subscribing would undo that (spec 0207 D2, D3).
func subscribedSignals(ignored func(os.Signal) bool) []os.Signal {
	sigs := []os.Signal{syscall.SIGTERM}

	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGHUP} {
		if !ignored(sig) {
			sigs = append(sigs, sig)
		}
	}

	return sigs
}

// terminateBySignal ends the process by sig, so a parent reading the wait
// status (systemd, a supervisor) sees a death by that signal rather than an
// exit status. If the signal does not land, because it is ignored or this is
// PID 1, it exits with code.
func terminateBySignal(sig os.Signal, code int) {
	if s, ok := sig.(syscall.Signal); ok {
		signal.Reset(s)

		_ = syscall.Kill(syscall.Getpid(), s)

		time.Sleep(signalDeliveryWait)
	}

	os.Exit(code)
}
