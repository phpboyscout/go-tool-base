package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/controls"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Markers printed by the `supervise` fixture. The signal-ownership E2E scenario
// asserts on these exact lines to prove the whole stack cooperated on a single
// real SIGINT.
const (
	// superviseReadyMarker confirms the controller is running, so the harness
	// never delivers a signal before there is anything to interrupt.
	superviseReadyMarker = "supervisor running"

	// superviseCauseMarker reports the context cause the supervised service
	// actually observed. This is the assertion that matters: it proves the
	// controller drove the stop through its own shutdown sequence rather than
	// the root's cancellation leaking through as context.Canceled.
	superviseCauseMarker = "service stopped: cause="

	// superviseDoneMarker confirms the full shutdown sequence completed and
	// Wait() released — i.e. the root did not exit out from under it.
	superviseDoneMarker = "supervised shutdown complete"
)

// newSuperviseCommand returns a hidden, e2e-only fixture that runs a real
// controls.Controller underneath the GTB root command.
//
// It exists to cover the one arrangement neither existing suite reaches: the CLI
// signal feature drives a real OS signal but has no controller, and the controls
// features have a controller but inject on the channel rather than raising a
// signal. Only the combination can catch a regression where both layers install
// a handler and race for a single Ctrl-C.
//
// Note what is deliberately absent: controls.WithSignals(). The root command owns
// signal disposition and cancels cmd.Context(); the controller observes that. If
// someone reintroduces a second handler here, the cause assertion breaks.
func newSuperviseCommand(props *p.Props) *setup.Command {
	cmd := &cobra.Command{
		Use:    "supervise",
		Short:  "e2e fixture: run a controls.Controller until interrupted",
		Hidden: true,
		RunE: func(c *cobra.Command, _ []string) error {
			out := c.OutOrStdout()

			controller := controls.NewController(c.Context(),
				controls.WithLogger(logger.ToSlog(props.Logger)),
			)

			observed := make(chan error, 1)

			controller.Register("blocker",
				controls.WithStart(func(ctx context.Context) error {
					<-ctx.Done()

					observed <- context.Cause(ctx)

					return ctx.Err()
				}),
			)

			controller.Start()

			_, _ = fmt.Fprintln(out, superviseReadyMarker)

			// Blocks until the full shutdown sequence has completed. This is
			// also what stops the root exiting early: RunE cannot return, so
			// ExecuteContext cannot return, so the 128+signum exit cannot fire
			// until the controller is genuinely finished.
			controller.Wait()

			cause := <-observed

			// Report the cause by name so the scenario can assert on it without
			// depending on the error's formatting.
			name := "unexpected"
			if errors.Is(cause, controls.ErrShutdown) {
				name = "ErrShutdown"
			}

			_, _ = fmt.Fprintf(out, "%s%s\n", superviseCauseMarker, name)
			_, _ = fmt.Fprintln(out, superviseDoneMarker)

			return nil
		},
	}

	return setup.Wrap("", cmd)
}
