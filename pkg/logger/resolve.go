package logger

import (
	"context"
	"log/slog"
)

// NewResolvingHandler wraps h so attribute values are Resolve()d before they
// reach it.
//
// # Why this exists
//
// slog's contract says a handler must call Value.Resolve() on every attribute.
// That is what turns a slog.LogValuer into the value it wants logged.
//
// charmbracelet/log does this in ONE of its three formatters. jsonFormatterItem
// resolves and nests structured values; text and logfmt do neither, because
// Handle passes the raw slog.Value on and leaves the decision to each formatter.
// So the same record carries less information as text than as JSON, and a
// LogValuer renders as whatever its Go value prints as: a struct as "{}", an
// error as its message alone.
//
// It became load-bearing with errorhandling v0.2.0, which stopped taking errors
// apart and now hands the error to slog whole, expecting the handler to resolve
// it. Everything the error carries, hints included, arrives through LogValue.
// Against the text formatter all of it silently vanished.
//
// # Delete this when upstream fixes it
//
//	https://github.com/charmbracelet/log/issues/96
//
// The asymmetry looks accidental rather than deliberate: PR #127 ("support slog
// attributes") touched json.go and the handler, not the two text formatters.
// A patch exists and was NOT proposed, because resolving properly changes output
// for anyone pinning the old rendering, and that call belongs to the maintainers.
// The findings are recorded on the issue.
//
// If it lands, this file and its wiring in NewCharm go away with it. Until then
// this is the smallest thing that makes GTB's own output correct. See
// docs/explanation/components/logger.md for why charm was kept rather than
// swapped out.
//
// # Scope
//
// Conformance and nothing more: it makes the record say what the producer meant
// it to say. How an error is best PRESENTED to a human is a separate question,
// deliberately not answered here.
func NewResolvingHandler(h slog.Handler) slog.Handler {
	return &resolvingHandler{inner: h}
}

type resolvingHandler struct {
	inner slog.Handler
}

func (h *resolvingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *resolvingHandler) Handle(ctx context.Context, r slog.Record) error {
	// Rebuild rather than mutate: a Record's attributes are not addressable,
	// and a clone leaves the caller's record untouched.
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)

	r.Attrs(func(a slog.Attr) bool {
		a.Value = a.Value.Resolve()
		out.AddAttrs(a)

		return true
	})

	return h.inner.Handle(ctx, out) //nolint:wrapcheck // the inner handler's error passes through untouched
}

func (h *resolvingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &resolvingHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *resolvingHandler) WithGroup(name string) slog.Handler {
	return &resolvingHandler{inner: h.inner.WithGroup(name)}
}
