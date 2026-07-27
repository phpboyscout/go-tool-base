package setup

import (
	"runtime/debug"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// resolveLogger returns the logger for the command being run: the one stamped
// on the command context by the root pre-run when present (so a second root in
// the same process uses its OWN logger, not the first root's), falling back to
// the registration-time logger otherwise.
func resolveLogger(cmd *cobra.Command, fallback logger.Logger) logger.Logger {
	if p := PropsFromContext(cmd.Context()); p != nil && p.Logger != nil {
		return p.Logger
	}

	return fallback
}

// WithTiming returns middleware that logs command execution duration.
func WithTiming(l logger.Logger) Middleware {
	return func(next func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
		return func(cmd *cobra.Command, args []string) error {
			l := resolveLogger(cmd, l)
			start := time.Now()

			err := next(cmd, args)

			duration := time.Since(start)

			if err != nil {
				l.Debug("command completed",
					"command", cmd.Name(),
					"duration", duration,
					"error", err.Error(),
				)
			} else {
				l.Debug("command completed",
					"command", cmd.Name(),
					"duration", duration,
				)
			}

			return err
		}
	}
}

// WithRecovery returns middleware that catches panics in the command
// handler and converts them to errors. The panic value and stack trace
// are logged at Error level.
func WithRecovery(l logger.Logger) Middleware {
	return func(next func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
		return func(cmd *cobra.Command, args []string) (retErr error) {
			l := resolveLogger(cmd, l)

			defer func() {
				if r := recover(); r != nil {
					stack := debug.Stack()

					l.Error("panic recovered in command",
						"command", cmd.Name(),
						"panic", r,
					)
					l.Debug("panic stack trace",
						"command", cmd.Name(),
						"stack", string(stack),
					)
					retErr = errors.Newf("panic in command %q: %v", cmd.Name(), r)
				}
			}()

			return next(cmd, args)
		}
	}
}

// WithTelemetry returns middleware that automatically tracks command invocations
// via the telemetry collector on Props. Records command name, duration, and exit
// code for every command execution. No-op when the collector is nil or telemetry
// is disabled (the collector is a noop in that case).
//
// The Props used is the one the root pre-run stamped onto the command context
// when present, so a second root constructed in the same process reports through
// its OWN collector rather than the first root's (the registry is process-global
// and would otherwise bind the first root forever). The registration-time p is
// the fallback for commands that never entered the pre-run.
func WithTelemetry(p *props.Props) Middleware {
	return func(next func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
		return func(cmd *cobra.Command, args []string) error {
			start := time.Now()

			err := next(cmd, args)

			durationMs := time.Since(start).Milliseconds()
			exitCode := 0

			if err != nil {
				exitCode = 1
			}

			active := p
			if ctxProps := PropsFromContext(cmd.Context()); ctxProps != nil {
				active = ctxProps
			}

			// Props.Collector is always non-nil (defaulted to a noop in the
			// root bootstrap), but guard anyway for a hand-built fallback.
			if active != nil && active.Collector != nil {
				active.Collector.TrackCommand(cmd.Name(), durationMs, exitCode, nil)
			}

			return err
		}
	}
}

// WithAuthCheck returns middleware that validates the specified
// configuration keys are non-empty before allowing command execution.
// If any key is empty, a descriptive error is returned without
// executing the command.
//
// The keys resolve against p's live store at execution time. The previous
// implementation read the global viper singleton, which GTB's own
// configuration never populated — every check passed vacuously.
func WithAuthCheck(p props.ConfigProvider, keys ...string) Middleware {
	return func(next func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
		return func(cmd *cobra.Command, args []string) error {
			if len(keys) == 0 {
				return next(cmd, args)
			}

			if p == nil || p.GetConfig() == nil {
				return errors.New("auth check requires a loaded configuration")
			}

			view := p.GetConfig().View()

			for _, key := range keys {
				if view.GetString(key) == "" {
					return errors.Newf(
						"required configuration %q is not set; run 'config set %s <value>' first",
						key, key,
					)
				}
			}

			return next(cmd, args)
		}
	}
}
