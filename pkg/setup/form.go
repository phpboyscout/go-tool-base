package setup

import (
	"context"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"gitlab.com/phpboyscout/go/credentials"
	"gitlab.com/phpboyscout/go/errors"
	"golang.org/x/term"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// ErrNonInteractive is a wizard asked for where nobody can answer it: stdin
// is not a terminal and accessible mode is off.
var ErrNonInteractive = errors.NewSentinel("gtb.setup.non_interactive", "an interactive terminal is needed")

// ErrInputEnded is an accessible form whose stdin ran out before every
// question was answered.
var ErrInputEnded = errors.NewSentinel("gtb.setup.input_ended", "input ended before every question was answered")

// RunForm runs a huh form on the invocation's streams (spec 0198 D2). The
// accessible decision is the IO's, applied to the form here, because huh
// decides it per form from TERM=dumb and offers no way to ask. A stdin that
// is neither a terminal nor in accessible mode is refused before the form
// opens, with the non-interactive route in the hint.
func RunForm(ctx context.Context, p *props.Props, f *huh.Form) error {
	return RunFormOn(ctx, p.GetIO(), f)
}

// RunFormOn is RunForm for a caller that holds the streams rather than the
// Props (the self-updater, built without them).
func RunFormOn(ctx context.Context, io props.IO, f *huh.Form) error {
	if !Promptable(io) {
		return errors.WithHint(ErrNonInteractive,
			"Run this from a terminal, or non-interactively: pass the answers as flags, or set GTB_ACCESSIBLE=true for line prompts on a piped stdin.")
	}

	f, answers := prepareForm(io, f)
	if err := f.RunWithContext(ctx); err != nil {
		return err
	}

	if answers != nil && answers.ranOut {
		return errors.WithHint(ErrInputEnded,
			"Give one line per question; an empty line takes the question's default.")
	}

	return nil
}

// PrepareForm binds f to the IO's streams the way RunFormOn does, without
// running it: the accessible decision, the theme, and the headless program
// options. It is for the few prompts reached from code that carries no
// context, which then call Run themselves; everything else uses RunFormOn.
func PrepareForm(io props.IO, f *huh.Form) *huh.Form {
	f, _ = prepareForm(io, f)

	return f
}

// prepareForm is PrepareForm returning the accessible line reader too, nil
// on the TUI path, so RunFormOn can ask whether the answers ran out.
func prepareForm(io props.IO, f *huh.Form) (*huh.Form, *lineReader) {
	var answers *lineReader

	// Read once: an IO may hand each form its own input (formtest.TUIForms).
	in, out := io.In(), io.Err()

	if io.Accessible() {
		// huh builds a fresh scanner for every accessible prompt, and a scanner
		// over a pipe reads ahead, so every answer after the first was lost
		// with the scanner that read it. One line per Read means a prompt can
		// take no more than its own answer.
		answers = newLineReader(in)
		in = answers

		if tty, ok := terminal(io.In()); ok {
			// huh reads a password straight from the terminal and needs its
			// descriptor to do it (#108); the line reader byte-reads, so it
			// holds nothing back from that read.
			in = terminalLineReader{lineReader: answers, tty: tty}
		}
	}

	f = f.WithInput(in).WithOutput(out).WithAccessible(io.Accessible()).WithTheme(FormTheme())

	if !io.Accessible() {
		f = f.WithProgramOptions(append(programOptions(in, out), tea.WithFilter(refocusRefusedField(f)))...)
	}

	return f, answers
}

// FormTheme is the theme every wizard renders with: huh's Charm theme with the
// form set in from the terminal's left edge by two columns and one line, the
// way huh's own examples render. A form against the edge reads as a stray
// print; the margin is what makes it read as a dialogue. Every framework
// wizard gets it through RunForm; a form the gtb CLI runs itself sets it.
func FormTheme() huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		styles := huh.ThemeCharm(isDark)
		styles.Form.Base = styles.Form.Base.PaddingTop(formPaddingTop).PaddingLeft(formPaddingLeft)

		return styles
	})
}

// Promptable reports whether a wizard can run on these streams: stdin is a
// terminal, or accessible line prompts were asked for (--accessible,
// GTB_ACCESSIBLE=true, TERM=dumb), which read any stdin. It is the one rule
// every prompt gate in the framework and the gtb CLI applies; a gate that
// demanded a terminal refused a piped stdin that had asked for line prompts.
func Promptable(io props.IO) bool {
	return io.Interactive() || io.Accessible()
}

// lineReader hands out one line per Read, however many the underlying
// reader would give at once. See RunFormOn. It takes the line a byte at a
// time: every form gets its own lineReader over the same stdin, and one that
// buffered ahead would keep the next form's answers.
type lineReader struct {
	r       io.Reader
	pending []byte
	// unterminated is a last answer given without a newline; the read that
	// finds the end after it is the prompt finishing that answer, not a
	// prompt left without one.
	unterminated bool
	// ranOut is a prompt asking for a line after the input ended. huh then
	// takes the default without validating it (huh v2.0.3), so RunFormOn
	// turns it into ErrInputEnded.
	ranOut bool
}

func newLineReader(r io.Reader) *lineReader {
	return &lineReader{r: r}
}

func (l *lineReader) Read(p []byte) (int, error) {
	if len(l.pending) == 0 {
		line, err := l.readLine()
		if len(line) == 0 {
			if errors.Is(err, io.EOF) {
				l.ranOut = l.ranOut || !l.unterminated
				l.unterminated = false
			}

			return 0, err
		}

		l.pending = line
		l.unterminated = line[len(line)-1] != '\n'
	}

	n := copy(p, l.pending)
	l.pending = l.pending[n:]

	return n, nil
}

// terminalLineReader is a lineReader over a terminal, exposing its descriptor
// for the reads huh makes directly.
type terminalLineReader struct {
	*lineReader
	tty *os.File
}

// Fd is the terminal's descriptor.
func (t terminalLineReader) Fd() uintptr {
	return t.tty.Fd()
}

// terminal reports whether r is a terminal, the rule StdIO.Interactive
// applies.
func terminal(r io.Reader) (*os.File, bool) {
	f, ok := r.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return nil, false
	}

	return f, true
}

// SecretEchoMode is the echo mode for a field that takes a secret. huh hides
// an accessible password by reading the terminal, which a piped stdin does
// not have; there it is read as a plain line, since nothing echoes on a pipe.
func SecretEchoMode(io props.IO) huh.EchoMode {
	if !io.Accessible() {
		return huh.EchoModePassword // asked before In: an IO may hand each read its own input
	}

	if _, ok := terminal(io.In()); ok {
		return huh.EchoModePassword
	}

	return huh.EchoModeNormal
}

// readLine reads up to and including the next newline, or to the end of the
// input.
func (l *lineReader) readLine() ([]byte, error) {
	var (
		line []byte
		b    [1]byte
	)

	for {
		n, err := l.r.Read(b[:])
		if n == 1 {
			line = append(line, b[0])

			if b[0] == '\n' {
				return line, nil
			}
		}

		if err != nil {
			return line, err
		}
	}
}

// The margin every wizard renders with: one line above, two columns in.
const (
	formPaddingTop  = 1
	formPaddingLeft = 2
)

const (
	headlessWidth  = 100
	headlessHeight = 40
)

// programOptions are the bubbletea options a TUI form runs with: the IO's
// streams, and no renderer when the output is not a terminal, so a test's
// discard writer does not receive escape sequences and a form on a piped
// output does not paint.
func programOptions(in io.Reader, out io.Writer) []tea.ProgramOption {
	opts := []tea.ProgramOption{tea.WithInput(in), tea.WithOutput(out)}

	if _, isTerminal := out.(interface{ Fd() uintptr }); !isTerminal {
		// No renderer means no WindowSizeMsg, and a field with a placeholder
		// panics on the negative width that leaves; give the form a size.
		opts = append(opts, tea.WithoutRenderer(), tea.WithWindowSize(headlessWidth, headlessHeight),
			tea.WithEnvironment([]string{"TERM=xterm-256color"}))
	}

	return opts
}

// storageModeLabels are the storage-mode selector's option labels; a wizard names
// the credential ("API key", "token") and this names the modes.
var storageModeLabels = credentialposture.ModeLabels{
	Env:      "Environment variable reference",
	Keychain: "OS keychain",
	Literal:  "Literal value in config file (plaintext)",
}

// StorageModeGroup is the one storage-mode selector every credential wizard
// asks (spec 0198 D5): environment variable reference, OS keychain when a
// backend is linked and answers a probe, and a literal in the config file
// unless the process runs under CI. The probe takes the caller's context. The
// current mode is left as the caller set it, or defaulted to the recommended
// one; hide decides whether the page is asked at all.
func StorageModeGroup(ctx context.Context, p *props.Props, mode *credentials.Mode, hide func() bool) *huh.Group {
	probeCtx, cancel := context.WithTimeout(ctx, credentials.KeychainOpTimeout)
	defer cancel()

	choices, defaultMode := credentialposture.StorageModeOptions(probeCtx, p.GetIO(), storageModeLabels)

	options := make([]huh.Option[credentials.Mode], len(choices))
	for i, c := range choices {
		options[i] = huh.NewOption(c.Label, c.Mode)
	}

	if *mode == "" {
		*mode = defaultMode
	}

	return huh.NewGroup(
		huh.NewSelect[credentials.Mode]().
			Key("storage-mode").
			Title("Credential Storage").
			Description(storageModeDescription(credentials.IsCI())).
			Options(options...).
			Value(mode),
	).WithHideFunc(hide)
}

func storageModeDescription(ci bool) string {
	if ci {
		return "CI environment detected: only environment variable references are permitted. Configure the env var via your CI platform's secret injection."
	}

	return "Environment variable references keep secrets out of the config file. Pick literal mode only for throwaway environments."
}
