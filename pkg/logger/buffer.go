package logger

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
)

// Entry represents a single captured log message.
type Entry struct {
	Level   Level
	Message string
	Keyvals []any
}

// bufferLogger captures log messages in memory for test assertions. It embeds a
// *slog.Logger backed by a CaptureHandler, so it satisfies the Logger interface
// directly and records everything logged through it — including loggers derived
// via With/WithGroup, which share the same capture store.
type bufferLogger struct {
	*slog.Logger
	capture *CaptureHandler
}

// NewBuffer returns a bufferLogger that captures all log messages in memory.
// Use Entries(), Messages(), and Contains() to assert on captured output.
//
// Example:
//
//	buf := logger.NewBuffer()
//	myFunc(buf)
//	if !buf.Contains("expected message") {
//	    t.Error("missing expected log message")
//	}
func NewBuffer() *bufferLogger {
	capture := NewCaptureHandler()

	// Wrapped exactly as the charm logger is. Without this the test double
	// renders errors differently from production: a test asserting on a hint
	// would find nothing and say so, or worse, assert its absence and pass.
	return &bufferLogger{
		Logger:  slog.New(NewPresentingHandler(NewResolvingHandler(capture))),
		capture: capture,
	}
}

// Entries returns all captured log entries, in order.
func (b *bufferLogger) Entries() []Entry {
	records := b.capture.Records()

	entries := make([]Entry, len(records))
	for i, record := range records {
		entries[i] = recordToEntry(record)
	}

	return entries
}

func recordToEntry(record slog.Record) Entry {
	entry := Entry{
		Level:   fromSlogLevel(record.Level),
		Message: record.Message,
	}

	record.Attrs(func(attr slog.Attr) bool {
		entry.Keyvals = append(entry.Keyvals, attr.Key, attr.Value.Any())

		return true
	})

	return entry
}

// Messages returns all captured log messages as strings, in order.
func (b *bufferLogger) Messages() []string {
	return b.capture.Messages()
}

// Contains returns true if any captured message contains the given substring.
func (b *bufferLogger) Contains(substr string) bool {
	return b.capture.Contains(substr)
}

// ContainsLevel returns true if any captured entry at the given level contains
// the given substring.
func (b *bufferLogger) ContainsLevel(level Level, substr string) bool {
	for _, record := range b.capture.Records() {
		if fromSlogLevel(record.Level) == level && strings.Contains(record.Message, substr) {
			return true
		}
	}

	return false
}

// SetLevel raises or lowers the minimum level at which the buffer captures
// records, implementing Leveller so tests can exercise level-gated code paths.
func (b *bufferLogger) SetLevel(level slog.Level) {
	b.capture.SetLevel(level)
}

// Reset clears all captured entries.
func (b *bufferLogger) Reset() {
	b.capture.Reset()
}

// String returns all captured messages joined by newlines.
func (b *bufferLogger) String() string {
	var buf bytes.Buffer
	for _, record := range b.capture.Records() {
		fmt.Fprintf(&buf, "[%s] %s\n", fromSlogLevel(record.Level), record.Message)
	}

	return buf.String()
}

// Len returns the number of captured entries.
func (b *bufferLogger) Len() int {
	return len(b.capture.Records())
}
