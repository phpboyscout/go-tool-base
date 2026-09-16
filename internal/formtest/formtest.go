// Package formtest drives real huh forms in tests with no TTY and no
// production seam (spec 0198 D3). Answers feeds accessible-mode prompts one
// line per field; Keys feeds the TUI path one key sequence at a time.
//
// Both rest on a reader that yields one chunk per Read: accessible mode reads
// each field through a fresh buffered reader, so a single reader holding every
// answer loses all but the first, and the TUI's key parser merges bytes that
// arrive in one read.
package formtest

import (
	"io"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Key sequences for Keys, named for what a person presses.
const (
	Enter = "\r"
	Down  = "\x1b[B"
	Up    = "\x1b[A"
	Space = " "
	Tab   = "\t"
	Yes   = "y"
	No    = "n"
)

const (
	keyPace    = 20 * time.Millisecond
	enterPace  = 150 * time.Millisecond
	settleTime = 50 * time.Millisecond
)

type chunkReader struct {
	chunks []string
	pause  time.Duration
	// last is the previous chunk; an Enter moves huh to the next field or
	// group through a command the program runs after Update returns, so the
	// key after it waits longer or lands on the field that was just left.
	last string
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		// Let the program process the last key before it sees EOF.
		time.Sleep(settleTime)

		return 0, io.EOF
	}

	switch {
	case r.pause > 0 && r.last == Enter:
		time.Sleep(enterPace)
	case r.pause > 0:
		time.Sleep(r.pause)
	}

	n := copy(p, r.chunks[0])
	r.last = r.chunks[0]
	r.chunks = r.chunks[1:]

	return n, nil
}

// Answers is what a person types at accessible-mode prompts, one per field:
// an option's number for a select, a line for an input, y or n for a confirm.
// Pair it with an IO whose Accessible reports true.
func Answers(lines ...string) io.Reader {
	chunks := make([]string, len(lines))
	for i, l := range lines {
		chunks[i] = l + "\n"
	}

	return &chunkReader{chunks: chunks}
}

// Keys is what a person presses on the TUI path, one sequence per Read, paced
// so the parser sees each on its own and a field change has happened before
// the next key. Typed text is one key per rune. The pacing is time, so a test
// on this route does not call t.Parallel: contention is what makes a paced
// key land early.
func Keys(seqs ...string) io.Reader {
	var chunks []string

	for _, s := range seqs {
		switch s {
		case Enter, Down, Up, Space, Tab:
			chunks = append(chunks, s)
		default:
			for _, r := range s {
				chunks = append(chunks, string(r))
			}
		}
	}

	return &chunkReader{chunks: chunks, pause: keyPace}
}

// TUI is an IO for the key route: interactive as far as the form is
// concerned, with the program running headless. The form's own
// WithProgramOptions receive ProgramOptions.
func TUI(keys io.Reader) props.IO {
	return &tuiIO{in: keys}
}

// ProgramOptions are the bubbletea options a form on the key route needs:
// the keys as input, no output, no renderer, a terminal the parser
// recognises, and a window size, since no renderer means no WindowSizeMsg
// and a field with a placeholder panics on a negative width.
func ProgramOptions(in io.Reader) []tea.ProgramOption {
	return []tea.ProgramOption{
		tea.WithInput(in),
		tea.WithOutput(io.Discard),
		tea.WithoutRenderer(),
		tea.WithWindowSize(headlessWidth, headlessHeight),
		tea.WithEnvironment([]string{"TERM=xterm-256color"}),
	}
}

const (
	headlessWidth  = 100
	headlessHeight = 40
)

// AccessibleTTY is an IO for a terminal in accessible mode (a screen reader,
// or TERM=dumb): interactive, so a prompt that is gated on a terminal is
// reached, and accessible, so it takes Answers. Parallel-safe, unlike the
// key route.
func AccessibleTTY(answers io.Reader) props.IO {
	return accessibleTTY{in: answers}
}

type accessibleTTY struct{ in io.Reader }

func (a accessibleTTY) In() io.Reader   { return a.in }
func (accessibleTTY) Out() io.Writer    { return io.Discard }
func (accessibleTTY) Err() io.Writer    { return io.Discard }
func (accessibleTTY) Interactive() bool { return true }
func (accessibleTTY) Accessible() bool  { return true }

// TUIForms is TUI for a wizard that runs several forms in turn: each In()
// hands the next script to the next form. A form's program reads its input
// ahead of the keys it has handled and does not give the surplus back when
// the form completes, so a later form's keys left on a shared reader are
// lost with the program that read them. One script per form, in the order
// the forms run; a form past the last script reads nothing.
func TUIForms(scripts ...io.Reader) props.IO {
	return &tuiIO{scripts: scripts}
}

type tuiIO struct {
	in      io.Reader
	mu      sync.Mutex
	scripts []io.Reader
}

func (t *tuiIO) In() io.Reader {
	if t.in != nil {
		return t.in
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.scripts) == 0 {
		return strings.NewReader("")
	}

	next := t.scripts[0]
	t.scripts = t.scripts[1:]

	return next
}

func (*tuiIO) Out() io.Writer    { return io.Discard }
func (*tuiIO) Err() io.Writer    { return io.Discard }
func (*tuiIO) Interactive() bool { return true }
func (*tuiIO) Accessible() bool  { return false }
