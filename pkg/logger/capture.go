package logger

import (
	"context"
	"log/slog"
	"strings"
	"sync"
)

// CaptureHandler is an slog.Handler that records emitted records in memory for
// test assertions. It is the slog-first counterpart to the buffer logger: pass
// slog.New(logger.NewCaptureHandler()) into a package under test and assert on
// Records / Messages. Handlers derived via WithAttrs and WithGroup share the
// same underlying record store, so records land in one place regardless of
// which derived logger emitted them. It is safe for concurrent use.
type CaptureHandler struct {
	store  *captureStore
	attrs  []slog.Attr
	groups []string
	level  slog.Leveler
}

type captureStore struct {
	mu      sync.Mutex
	records []slog.Record
}

// NewCaptureHandler returns a CaptureHandler that records everything at
// DebugLevel and above.
func NewCaptureHandler() *CaptureHandler {
	return &CaptureHandler{store: &captureStore{}, level: slog.LevelDebug}
}

// Enabled reports whether level is at or above the handler's minimum level.
func (h *CaptureHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// Handle records a clone of r, decorated with any attributes accumulated
// through WithAttrs so assertions see the full attribute set.
func (h *CaptureHandler) Handle(_ context.Context, record slog.Record) error {
	stored := record.Clone()
	if len(h.attrs) > 0 {
		stored.AddAttrs(h.attrs...)
	}

	h.store.mu.Lock()
	defer h.store.mu.Unlock()

	h.store.records = append(h.store.records, stored)

	return nil
}

// WithAttrs returns a derived handler that decorates future records with attrs
// and shares this handler's record store.
func (h *CaptureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	derived := h.clone()
	derived.attrs = append(derived.attrs, attrs...)

	return derived
}

// WithGroup returns a derived handler that shares this handler's record store.
func (h *CaptureHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	derived := h.clone()
	derived.groups = append(derived.groups, name)

	return derived
}

func (h *CaptureHandler) clone() *CaptureHandler {
	return &CaptureHandler{
		store:  h.store,
		attrs:  append([]slog.Attr(nil), h.attrs...),
		groups: append([]string(nil), h.groups...),
		level:  h.level,
	}
}

// Records returns a snapshot of every captured record.
func (h *CaptureHandler) Records() []slog.Record {
	h.store.mu.Lock()
	defer h.store.mu.Unlock()

	out := make([]slog.Record, len(h.store.records))
	copy(out, h.store.records)

	return out
}

// Messages returns the message of every captured record, in order.
func (h *CaptureHandler) Messages() []string {
	h.store.mu.Lock()
	defer h.store.mu.Unlock()

	out := make([]string, len(h.store.records))
	for i, record := range h.store.records {
		out[i] = record.Message
	}

	return out
}

// Contains reports whether any captured record's message contains substr.
func (h *CaptureHandler) Contains(substr string) bool {
	for _, msg := range h.Messages() {
		if strings.Contains(msg, substr) {
			return true
		}
	}

	return false
}
