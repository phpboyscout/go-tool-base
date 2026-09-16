package setup

import (
	"context"

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

const (
	headlessWidth  = 100
	headlessHeight = 40
)

// programOptions are the bubbletea options a TUI form runs with: the IO's
// streams, and no renderer when the output is not a terminal, so a test's
// discard writer does not receive escape sequences and a form on a piped
// output does not paint.
func programOptions(io props.IO) []tea.ProgramOption {
	opts := []tea.ProgramOption{tea.WithInput(io.In()), tea.WithOutput(io.Err())}

	if _, isTerminal := io.Err().(interface{ Fd() uintptr }); !isTerminal {
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
func StorageModeGroup(ctx context.Context, mode *credentials.Mode, hide func() bool) *huh.Group {
	probeCtx, cancel := context.WithTimeout(ctx, credentials.KeychainOpTimeout)
	defer cancel()

	choices, defaultMode := credentialposture.StorageModeOptions(probeCtx, storageModeLabels)

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
