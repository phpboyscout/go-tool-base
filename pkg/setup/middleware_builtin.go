package setup

import (
	"runtime/debug"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

// WithTiming returns middleware that logs command execution duration.
func WithTiming(l logger.Logger) Middleware {
	return func(next func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
		return func(cmd *cobra.Command, args []string) error {
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

			if p.Collector != nil {
				p.Collector.TrackCommand(cmd.Name(), durationMs, exitCode, nil)
			}

			return err
		}
	}
}

// WithAuthCheck returns middleware that validates the specified
// configuration keys are non-empty before allowing command execution.
// If any key is empty, a descriptive error is returned without
// executing the command.
func WithAuthCheck(keys ...string) Middleware {
	return func(next func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
		return func(cmd *cobra.Command, args []string) error {
			for _, key := range keys {
				val := viper.GetString(key)
				if val == "" {
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
