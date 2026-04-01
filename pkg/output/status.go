package output

import (
	"fmt"
	"io"
	"os"
	"sync"
)

const (
	iconSuccess = "✓"
	iconWarn    = "⚠"
	iconFail    = "✗"
	iconSpin    = "⠋"
)

// Status displays a live-updating status message for multi-step operations.
// In interactive terminals, updates the current line in place.
// In non-interactive environments, prints sequential lines.
// Safe for concurrent use from multiple goroutines.
type Status struct {
	w           io.Writer
	interactive bool
	active      bool
	mu          sync.Mutex
}

// NewStatus creates a status display.
func NewStatus() *Status {
	return &Status{
		w:           os.Stderr,
		interactive: IsInteractive(),
	}
}

// Update replaces the current status message with a spinner icon.
func (s *Status) Update(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.interactive {
		if s.active {
			_, _ = fmt.Fprintf(s.w, "\r\033[K")
		}

		_, _ = fmt.Fprintf(s.w, "%s %s", iconSpin, msg)
		s.active = true
	} else {
		_, _ = fmt.Fprintf(s.w, "%s...\n", msg)
	}
}

// Success marks the current step as successful and moves to the next line.
func (s *Status) Success(msg string) {
	s.complete(iconSuccess, msg)
}

// Warn marks the current step as a warning and moves to the next line.
func (s *Status) Warn(msg string) {
	s.complete(iconWarn, msg)
}

// Fail marks the current step as failed and moves to the next line.
func (s *Status) Fail(msg string) {
	s.complete(iconFail, msg)
}

// Done cleans up the status display.
func (s *Status) Done() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.interactive && s.active {
		_, _ = fmt.Fprintf(s.w, "\r\033[K")
		s.active = false
	}
}

func (s *Status) complete(icon, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.interactive {
		if s.active {
			_, _ = fmt.Fprintf(s.w, "\r\033[K")
		}

		_, _ = fmt.Fprintf(s.w, "%s %s\n", icon, msg)
		s.active = false
	} else {
		_, _ = fmt.Fprintf(s.w, "%s %s\n", icon, msg)
	}
}
