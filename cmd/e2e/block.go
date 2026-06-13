package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// gracefulShutdownMarker is printed to stdout by the contrived `block` command
// when its context is cancelled. The SIGINT E2E scenario asserts on this exact
// line to prove the signal cancelled cmd.Context() and the command unwound
// gracefully — rather than the process being killed outright.
const gracefulShutdownMarker = "graceful shutdown complete"

// newBlockCommand returns a hidden, e2e-only fixture command. It exists solely
// so the signal-aware execution context can be exercised at the OS level: the
// command blocks on cmd.Context().Done(), and when an external SIGINT/SIGTERM
// cancels that context it performs an observable graceful action (prints
// gracefulShutdownMarker) before returning. The root Execute wrapper then maps
// the received signal to the conventional 128+signum exit code.
//
// It is NOT a product command — it is hidden and only registered on the e2e
// test binary (cmd/e2e), never on cmd/gtb.
func newBlockCommand() *setup.Command {
	cmd := &cobra.Command{
		Use:    "block",
		Short:  "e2e fixture: block until interrupted, then shut down gracefully",
		Hidden: true,
		RunE: func(c *cobra.Command, _ []string) error {
			// Signal readiness on stdout so the test harness knows the command
			// is in its blocking state before it delivers the signal.
			_, _ = fmt.Fprintln(c.OutOrStdout(), "blocking until interrupted")

			<-c.Context().Done()

			// Observable graceful action proving the context was cancelled and
			// the command unwound rather than the process being hard-killed.
			_, _ = fmt.Fprintln(c.OutOrStdout(), gracefulShutdownMarker)

			return nil
		},
	}

	return setup.Wrap("", cmd)
}
