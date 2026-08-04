package logger

import (
	"context"
	"log/slog"

	"gitlab.com/phpboyscout/go/errors"
)

// hintsKey is the attribute an error's user-facing hints are surfaced under. It
// matches what errorhandling emitted before it moved to slog.LogValuer, so
// operators, and the scenarios that assert on stderr, see what they always saw.
const hintsKey = "hints"

// NewPresentingHandler wraps h to render errors the way a person reads them,
// rather than the way a log pipeline stores them.
//
// [NewResolvingHandler] already makes the record CORRECT: everything the error
// carries reaches the handler. This decides what a human should be shown of it,
// which is a different question and deliberately a separate wrapper.
//
// Two decisions:
//
// **Hints get their own attribute.** Resolution alone leaves them nested inside
// the error group:
//
//	err="[msg=no config file found kind=… hint=[Run 'gtb init' …]]"
//
// A hint is the one part of an error written FOR the person reading it, and
// burying it in a rendered group is a poor way to show it.
//
// **The error group is debug-only.** The error's message is already the log
// message, so rendering the group beside it repeats every failure back at the
// user. Debug is where the kind, details and attributes earn their space, the
// same rule errorhandling applies to stack traces.
//
// The result is that ordinary output matches what GTB printed before
// errorhandling v0.2.0, while --debug shows strictly more than it used to.
//
// The example above is what charm's text formatter makes of a resolved group
// today. If charmbracelet/log#96 is fixed upstream and NewResolvingHandler goes
// away, the group renders as dotted keys instead (err.msg=..., err.hint=...) and
// the hint stops being buried in a blob. It is still a member of the error group
// rather than a thing in its own right, so this wrapper keeps its job either
// way: what changes is how bad the alternative looks, not whether the decision
// holds.
func NewPresentingHandler(h slog.Handler) slog.Handler {
	return &presentingHandler{inner: h}
}

type presentingHandler struct {
	inner slog.Handler
}

func (h *presentingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *presentingHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)

	verbose := h.inner.Enabled(ctx, slog.LevelDebug)

	var hints string

	r.Attrs(func(a slog.Attr) bool {
		// Read the error BEFORE anything resolves it: a resolved LogValuer is
		// the group it logs as, and the hints are no longer reachable through
		// the errors API by then.
		if err, ok := errorValue(a); ok {
			if hints == "" {
				hints = errors.FlattenHints(err)
			}

			if !verbose {
				return true
			}
		}

		out.AddAttrs(a)

		return true
	})

	if hints != "" {
		out.AddAttrs(slog.String(hintsKey, hints))
	}

	return h.inner.Handle(ctx, out) //nolint:wrapcheck // the inner handler's error passes through untouched
}

func (h *presentingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &presentingHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *presentingHandler) WithGroup(name string) slog.Handler {
	return &presentingHandler{inner: h.inner.WithGroup(name)}
}

// errorValue reports whether an attribute carries an error.
//
// Both kinds have to be checked. An error implementing slog.LogValuer, as
// every go/errors type does, arrives as KindLogValuer because slog.AnyValue
// detects the interface and stores it as one; a plain error arrives as KindAny.
// Checking only KindAny silently skips exactly the errors this exists for.
func errorValue(a slog.Attr) (error, bool) {
	switch a.Value.Kind() {
	case slog.KindAny, slog.KindLogValuer:
		err, ok := a.Value.Any().(error)

		return err, ok
	default:
		return nil, false
	}
}
