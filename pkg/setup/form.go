package setup

import (
	"bufio"
	"context"
	"io"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"gitlab.com/phpboyscout/go/credentials"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// ErrNonInteractive is a wizard asked for where nobody can answer it: stdin
// is not a terminal and accessible mode is off.
var ErrNonInteractive = errors.NewSentinel("gtb.setup.non_interactive", "an interactive terminal is needed")

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
	if !io.Interactive() && !io.Accessible() {
		return errors.WithHint(ErrNonInteractive,
			"Run this from a terminal, or non-interactively: pass the answers as flags, or set GTB_ACCESSIBLE=true for line prompts on a piped stdin.")
	}

	// Read once: an IO may hand each form its own input (formtest.TUIForms).
	in, out := io.In(), io.Err()

	if io.Accessible() {
		// huh builds a fresh scanner for every accessible prompt, and a scanner
		// over a pipe reads ahead, so every answer after the first was lost
		// with the scanner that read it. One line per Read means a prompt can
		// take no more than its own answer.
		in = newLineReader(in)
	}

	f = f.WithInput(in).WithOutput(out).WithAccessible(io.Accessible()).WithTheme(FormTheme())

	if !io.Accessible() {
		f = f.WithProgramOptions(programOptions(in, out)...)
	}

	return f.RunWithContext(ctx)
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

// lineReader hands out one line per Read, however many the underlying
// reader would give at once. See RunFormOn.
type lineReader struct {
	r       *bufio.Reader
	pending []byte
}

func newLineReader(r io.Reader) *lineReader {
	return &lineReader{r: bufio.NewReader(r)}
}

func (l *lineReader) Read(p []byte) (int, error) {
	if len(l.pending) == 0 {
		line, err := l.r.ReadBytes('\n')
		if len(line) == 0 {
			return 0, err
		}

		l.pending = line
	}

	n := copy(p, l.pending)
	l.pending = l.pending[n:]

	return n, nil
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
