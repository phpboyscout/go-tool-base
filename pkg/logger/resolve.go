package logger

import (
	"context"
	"log/slog"
)

// NewResolvingHandler wraps h so attribute values are Resolve()d before they
// reach it.
//
// slog's contract says a handler must call Value.Resolve() on every attribute:
// that is what turns a slog.LogValuer into its logged form. charmbracelet/log's
// handler does not, so a LogValuer arrives unresolved and renders as whatever
// its Go value prints as — a struct as "{}", an error as its message alone.
//
// That became load-bearing with errorhandling v0.2.0. It stopped taking errors
// apart and now hands the error to slog whole, expecting the handler to resolve
// it; everything the error carries — kind, hints, details, attributes — arrives
// through LogValue. Against an unresolving handler all of it silently vanished,
// including the hints that tell a user what to do next.
//
// This is conformance and nothing more: it makes the record say what the
// producer meant it to say. How an error is best PRESENTED to a human is a
// separate question, deliberately not answered here.
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
