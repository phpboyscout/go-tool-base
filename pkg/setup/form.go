package setup

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"gitlab.com/phpboyscout/go/errors"

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
	io := p.GetIO()

	if !io.Interactive() && !io.Accessible() {
		return errors.WithHint(ErrNonInteractive,
			"Run this from a terminal, or non-interactively: pass the answers as flags, or set GTB_ACCESSIBLE=true for line prompts on a piped stdin.")
	}

	f = f.WithInput(io.In()).WithOutput(io.Err()).WithAccessible(io.Accessible())

	if !io.Accessible() {
		f = f.WithProgramOptions(programOptions(io)...)
	}

	return f.RunWithContext(ctx)
}

// programOptions are the bubbletea options a TUI form runs with: the IO's
// streams, and no renderer when the output is not a terminal, so a test's
// discard writer does not receive escape sequences and a form on a piped
// output does not paint.
func programOptions(io props.IO) []tea.ProgramOption {
	opts := []tea.ProgramOption{tea.WithInput(io.In()), tea.WithOutput(io.Err())}

	if _, isTerminal := io.Err().(interface{ Fd() uintptr }); !isTerminal {
		opts = append(opts, tea.WithoutRenderer(), tea.WithEnvironment([]string{"TERM=xterm-256color"}))
	}

	return opts
}
