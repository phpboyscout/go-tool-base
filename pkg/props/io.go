package props

import (
	"io"
	"os"

	"golang.org/x/term"
)

// IO is where an invocation reads and writes, and whether that is a person.
// It is one field on Props rather than three so the terminal is a thing Props
// has, and so a replacement (a recorded session, a remote terminal, an
// accessible-mode wrapper) swaps in whole (spec 0198 D1).
type IO interface {
	In() io.Reader
	Out() io.Writer
	Err() io.Writer
	// Interactive reports whether In is a terminal a person is typing at.
	Interactive() bool
	// Accessible reports whether forms should run as line prompts rather than
	// a TUI: a screen reader, TERM=dumb, or a test.
	Accessible() bool
}

// StdIO is the process's streams, or the ones a caller supplies. The zero
// value is stdin, stdout and stderr.
type StdIO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// AccessibleMode asks for line prompts; the root's --accessible flag and
	// GTB_ACCESSIBLE set it.
	AccessibleMode bool
}

// In returns Stdin, or the process's.
func (s StdIO) In() io.Reader {
	if s.Stdin != nil {
		return s.Stdin
	}

	return os.Stdin
}

// Out returns Stdout, or the process's.
func (s StdIO) Out() io.Writer {
	if s.Stdout != nil {
		return s.Stdout
	}

	return os.Stdout
}

// Err returns Stderr, or the process's.
func (s StdIO) Err() io.Writer {
	if s.Stderr != nil {
		return s.Stderr
	}

	return os.Stderr
}

// Interactive reports whether In is a terminal. A reader that is not an
// *os.File is not, and neither is a file that is not a terminal: /dev/null is
// a character device, which is why the old ModeCharDevice check said yes to a
// stdin nobody was typing at.
func (s StdIO) Interactive() bool {
	f, ok := s.In().(*os.File)
	if !ok {
		return false
	}

	return term.IsTerminal(int(f.Fd()))
}

// Accessible reports the explicit switch, or TERM=dumb, which is huh's own
// rule for switching to line prompts, applied here so the two agree.
func (s StdIO) Accessible() bool {
	return s.AccessibleMode || os.Getenv("TERM") == "dumb"
}

// GetIO returns the invocation's streams, the process's when none were set.
func (p *Props) GetIO() IO {
	if p == nil || p.IO == nil {
		return StdIO{}
	}

	return p.IO
}
