package logger

import (
	"bytes"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

// Entry represents a single captured log message.
type Entry struct {
	Level   Level
	Message string
	Keyvals []any
}

// entryStore is a shared, mutex-protected log entry store.
type entryStore struct {
	mu      sync.Mutex
	entries []Entry
}

func (s *entryStore) append(e Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = append(s.entries, e)
}

func (s *entryStore) snapshot() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Entry, len(s.entries))
	copy(out, s.entries)

	return out
}

func (s *entryStore) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = s.entries[:0]
}

func (s *entryStore) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.entries)
}

// bufferLogger captures log messages in memory for test assertions.
//
// level is stored atomically because SetLevel may be called concurrently with
// logging from other goroutines; prefix and fields are construction-only and
// never mutated after the value is created.
type bufferLogger struct {
	store  *entryStore
	level  atomic.Int32
	prefix string
	fields []any
}

// NewBuffer returns a Logger that captures all log messages in memory.
// Use Messages(), Entries(), and Contains() to assert on captured output.
//
// Example:
//
//	buf := logger.NewBuffer()
//	myFunc(buf)
//	if !buf.Contains("expected message") {
//	    t.Error("missing expected log message")
//	}
func NewBuffer() *bufferLogger {
	b := &bufferLogger{
		store: &entryStore{},
	}
	b.storeLevel(DebugLevel)

	return b
}

// getLevel returns the current level, read atomically.
func (b *bufferLogger) getLevel() Level {
	return Level(b.level.Load())
}

// storeLevel writes the level atomically. The defined levels are small
// non-negative constants, so the int->int32 narrowing is always lossless; the
// explicit bound makes that invariant checkable and avoids storing a corrupted
// value if an out-of-range Level is ever passed.
func (b *bufferLogger) storeLevel(level Level) {
	if level < DebugLevel || level > FatalLevel {
		level = ErrorLevel
	}

	b.level.Store(int32(level))
}

func (b *bufferLogger) record(level Level, msg string, keyvals ...any) {
	if level < b.getLevel() {
		return
	}

	fullMsg := msg
	if b.prefix != "" {
		fullMsg = b.prefix + ": " + msg
	}

	b.store.append(Entry{
		Level:   level,
		Message: fullMsg,
		Keyvals: slices.Concat(b.fields, keyvals),
	})
}

func (b *bufferLogger) Debug(msg string, keyvals ...any) { b.record(DebugLevel, msg, keyvals...) }
func (b *bufferLogger) Info(msg string, keyvals ...any)  { b.record(InfoLevel, msg, keyvals...) }
func (b *bufferLogger) Warn(msg string, keyvals ...any)  { b.record(WarnLevel, msg, keyvals...) }
func (b *bufferLogger) Error(msg string, keyvals ...any) { b.record(ErrorLevel, msg, keyvals...) }
func (b *bufferLogger) Fatal(msg string, keyvals ...any) { b.record(FatalLevel, msg, keyvals...) }

func (b *bufferLogger) Debugf(format string, args ...any) {
	b.record(DebugLevel, fmt.Sprintf(format, args...))
}

func (b *bufferLogger) Infof(format string, args ...any) {
	b.record(InfoLevel, fmt.Sprintf(format, args...))
}

func (b *bufferLogger) Warnf(format string, args ...any) {
	b.record(WarnLevel, fmt.Sprintf(format, args...))
}

func (b *bufferLogger) Errorf(format string, args ...any) {
	b.record(ErrorLevel, fmt.Sprintf(format, args...))
}

func (b *bufferLogger) Fatalf(format string, args ...any) {
	b.record(FatalLevel, fmt.Sprintf(format, args...))
}

func (b *bufferLogger) Print(msg any, keyvals ...any) {
	b.record(InfoLevel, fmt.Sprint(msg), keyvals...)
}

func (b *bufferLogger) With(keyvals ...any) Logger {
	merged := make([]any, len(b.fields)+len(keyvals))
	copy(merged, b.fields)
	copy(merged[len(b.fields):], keyvals)

	derived := &bufferLogger{
		store:  b.store,
		prefix: b.prefix,
		fields: merged,
	}
	derived.storeLevel(b.getLevel())

	return derived
}

func (b *bufferLogger) WithPrefix(prefix string) Logger {
	newPrefix := prefix
	if b.prefix != "" {
		newPrefix = b.prefix + ": " + prefix
	}

	derived := &bufferLogger{
		store:  b.store,
		prefix: newPrefix,
		fields: b.fields,
	}
	derived.storeLevel(b.getLevel())

	return derived
}

func (b *bufferLogger) SetLevel(level Level)     { b.storeLevel(level) }
func (b *bufferLogger) GetLevel() Level          { return b.getLevel() }
func (b *bufferLogger) SetFormatter(_ Formatter) {}
func (b *bufferLogger) Handler() slog.Handler    { return noopHandler{} }

// Entries returns all captured log entries.
func (b *bufferLogger) Entries() []Entry {
	return b.store.snapshot()
}

// Messages returns all captured log messages as strings.
func (b *bufferLogger) Messages() []string {
	entries := b.store.snapshot()

	msgs := make([]string, len(entries))
	for i, e := range entries {
		msgs[i] = e.Message
	}

	return msgs
}

// Contains returns true if any captured message contains the given substring.
func (b *bufferLogger) Contains(substr string) bool {
	for _, e := range b.store.snapshot() {
		if strings.Contains(e.Message, substr) {
			return true
		}
	}

	return false
}

// ContainsLevel returns true if any captured entry at the given level contains
// the given substring.
func (b *bufferLogger) ContainsLevel(level Level, substr string) bool {
	for _, e := range b.store.snapshot() {
		if e.Level == level && strings.Contains(e.Message, substr) {
			return true
		}
	}

	return false
}

// Reset clears all captured entries.
func (b *bufferLogger) Reset() {
	b.store.reset()
}

// String returns all captured messages joined by newlines.
func (b *bufferLogger) String() string {
	var buf bytes.Buffer
	for _, e := range b.store.snapshot() {
		fmt.Fprintf(&buf, "[%s] %s\n", e.Level, e.Message)
	}

	return buf.String()
}

// Len returns the number of captured entries.
func (b *bufferLogger) Len() int {
	return b.store.len()
}
