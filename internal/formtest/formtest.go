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
	settleTime = 50 * time.Millisecond
)

type chunkReader struct {
	chunks []string
	pause  time.Duration
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		// Let the program process the last key before it sees EOF.
		time.Sleep(settleTime)

		return 0, io.EOF
	}

	if r.pause > 0 {
		time.Sleep(r.pause)
	}

	n := copy(p, r.chunks[0])
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
// so the parser sees each on its own. Typed text is one key per rune.
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
	return tuiIO{in: keys}
}

// ProgramOptions are the bubbletea options a form on the key route needs:
// the keys as input, no output, no renderer, a terminal the parser recognises.
func ProgramOptions(in io.Reader) []tea.ProgramOption {
	return []tea.ProgramOption{
		tea.WithInput(in),
		tea.WithOutput(io.Discard),
		tea.WithoutRenderer(),
		tea.WithEnvironment([]string{"TERM=xterm-256color"}),
	}
}

type tuiIO struct{ in io.Reader }

func (t tuiIO) In() io.Reader   { return t.in }
func (tuiIO) Out() io.Writer    { return io.Discard }
func (tuiIO) Err() io.Writer    { return io.Discard }
func (tuiIO) Interactive() bool { return true }
func (tuiIO) Accessible() bool  { return false }
