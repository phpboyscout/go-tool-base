package output

import (
	"context"
	"fmt"
	"io"
	"os"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/cockroachdb/errors"
)

// spinnerStyle is the default spinner appearance.
var spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

// Spin shows a spinner with a message while fn executes.
// In interactive terminals, displays an animated spinner.
// In non-interactive environments (CI, piped output), prints plain text.
// The context is passed to fn and used for cancellation.
//
// If the user interrupts an active spinner with Ctrl-C, the context passed to
// fn is cancelled and Spin returns context.Canceled rather than reporting a
// false success.
func Spin(ctx context.Context, msg string, fn func(ctx context.Context) error) error {
	_, err := SpinWithResult(ctx, msg, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	})

	return err
}

// SpinWithResult is like Spin but returns a value alongside the error.
//
// As with Spin, a Ctrl-C interrupt while the spinner is active cancels fn's
// context and returns (zero, context.Canceled).
func SpinWithResult[T any](ctx context.Context, msg string, fn func(ctx context.Context) (T, error)) (T, error) {
	if !IsInteractive() {
		return spinPlain(ctx, os.Stderr, msg, fn)
	}

	return spinTUI(ctx, msg, fn)
}

// spinPlain prints plain status messages for non-interactive environments.
func spinPlain[T any](ctx context.Context, w io.Writer, msg string, fn func(ctx context.Context) (T, error)) (T, error) {
	_, _ = fmt.Fprintf(w, "%s...\n", msg)

	result, err := fn(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(w, "%s... failed\n", msg)
	} else {
		_, _ = fmt.Fprintf(w, "%s... done\n", msg)
	}

	return result, err
}

// spinnerModel is the Bubble Tea model for the animated spinner.
type spinnerModel[T any] struct {
	spinner spinner.Model
	msg     string
	ctx     context.Context
	cancel  context.CancelFunc
	fn      func(ctx context.Context) (T, error)
	result  T
	err     error
	done    bool
}

type spinDoneMsg[T any] struct {
	result T
	err    error
}

func newSpinnerModel[T any](ctx context.Context, msg string, fn func(ctx context.Context) (T, error)) spinnerModel[T] {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle

	// Wrap the caller's context so that a Ctrl-C interrupt while the spinner is
	// active cancels the in-flight work rather than leaving it running.
	runCtx, cancel := context.WithCancel(ctx)

	return spinnerModel[T]{
		spinner: s,
		msg:     msg,
		ctx:     runCtx,
		cancel:  cancel,
		fn:      fn,
	}
}

func (m spinnerModel[T]) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.runFn())
}

func (m spinnerModel[T]) runFn() tea.Cmd {
	return func() tea.Msg {
		result, err := m.fn(m.ctx)

		return spinDoneMsg[T]{result: result, err: err}
	}
}

func (m spinnerModel[T]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinDoneMsg[T]:
		m.result = msg.result
		m.err = msg.err
		m.done = true

		return m, tea.Quit
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			// Cancel the in-flight work and surface the interruption as an
			// error rather than reporting a (zero, nil) false success.
			if m.cancel != nil {
				m.cancel()
			}

			var zero T

			m.result = zero
			m.err = context.Canceled
			m.done = true

			return m, tea.Quit
		}
	}

	var cmd tea.Cmd

	m.spinner, cmd = m.spinner.Update(msg)

	return m, cmd
}

func (m spinnerModel[T]) View() tea.View {
	if m.done {
		return tea.NewView("")
	}

	return tea.NewView(fmt.Sprintf("%s %s\n", m.spinner.View(), m.msg))
}

// spinTUI runs the Bubble Tea spinner program.
func spinTUI[T any](ctx context.Context, msg string, fn func(ctx context.Context) (T, error)) (T, error) {
	model := newSpinnerModel(ctx, msg, fn)
	// Release the wrapped context regardless of how the program exits.
	defer model.cancel()

	p := tea.NewProgram(model, tea.WithOutput(os.Stderr))

	finalModel, err := p.Run()
	if err != nil {
		// Bubble Tea failed — fall back to plain
		return spinPlain(ctx, os.Stderr, msg, fn)
	}

	final, ok := finalModel.(spinnerModel[T])
	if !ok {
		var zero T

		return zero, errors.New("unexpected model type from spinner")
	}

	return final.result, final.err
}
