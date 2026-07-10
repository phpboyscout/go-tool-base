// Package logger provides GTB's logging boundary.
//
// The Logger interface mirrors the standard library's *slog.Logger method set
// exactly, so a *slog.Logger satisfies logger.Logger directly and can be passed
// anywhere a Logger is expected (for example props.Props.Logger). Consumers
// remain free to supply their own implementation with the same method set.
//
// Runtime level and format control are deliberately NOT part of the Logger
// interface — a plain *slog.Logger owns its level via its handler. They are
// exposed as optional capabilities through the Leveller and Reformatter
// interfaces and two package helpers that no-op when the logger does not
// implement them:
//
//	logger.SetLevel(log, slog.LevelDebug) // true if log implements Leveller
//	logger.SetFormatter(log, logger.JSONFormatter) // true if log implements Reformatter
//
// To branch on whether a level is active, use the interface method directly:
//
//	if log.Enabled(ctx, slog.LevelDebug) { /* expensive diagnostics */ }
//
// # Constructors
//
//   - NewCharm(w, opts...): GTB's default, backed by charmbracelet/log for
//     coloured, styled terminal output. The returned Logger also implements
//     Leveller and Reformatter, so --debug, log.level, and log.format take
//     effect on it. Use this for props.Props.Logger.
//   - NewCharmSlog(w, opts...) / NewCharmHandler(w, opts...): slog-native
//     construction over the same Charm output — a *slog.Logger and an
//     slog.Handler respectively.
//   - NewSlog(handler): slog.New(handler) for ecosystem handlers (zap, zerolog,
//     OpenTelemetry, custom). Wrap the handler with NewLevelGate for runtime
//     level control via a shared *slog.LevelVar.
//   - NewNoop(): a discarding *slog.Logger for tests where output is irrelevant.
//   - NewBuffer() / NewCaptureHandler(): capture records in memory for test
//     assertions.
package logger
