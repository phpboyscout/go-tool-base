package ai

import (
	"context"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	gochat "gitlab.com/phpboyscout/go/chat"
	"gitlab.com/phpboyscout/go/credentials"

	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// The wizard is driven the way a person drives it (spec 0198 D3): through
// the real form, on Props.IO. Accessible prompts take option numbers and
// lines; a masked key needs the TUI route, which takes keys.

// providerNumber is the 1-based position of provider in the select every
// provider linked (the test binary registers none, so all are offered).
func providerNumber(t *testing.T, provider string) int {
	t.Helper()

	for i, d := range chat.ProviderDisplays() {
		if string(d.ID) == provider {
			return i + 1
		}
	}

	t.Fatalf("provider %q is not offered", provider)

	return 0
}

// modeChoices are the storage modes the selector offers in this environment
// (keychain only when a backend is linked and answers; literal only outside
// CI) and the one the cursor starts on.
func modeChoices(t *testing.T) ([]credentials.Mode, credentials.Mode) {
	t.Helper()

	choices, def := credentialposture.StorageModeOptions(context.Background(), credentialposture.ModeLabels{Env: "e", Keychain: "k", Literal: "l"})

	modes := make([]credentials.Mode, len(choices))
	for i, c := range choices {
		modes[i] = c.Mode
	}

	return modes, def
}

func indexOf(t *testing.T, modes []credentials.Mode, mode credentials.Mode) int {
	t.Helper()

	for i, m := range modes {
		if m == mode {
			return i
		}
	}

	t.Fatalf("mode %q is not offered here (choices: %v)", mode, modes)

	return 0
}

// modeNumber is the 1-based position of mode among the offered choices, the
// number an accessible prompt takes.
func modeNumber(t *testing.T, mode credentials.Mode) int {
	t.Helper()

	modes, _ := modeChoices(t)

	return indexOf(t, modes, mode) + 1
}

// envVarIO answers the wizard for env-var mode at accessible prompts:
// provider, mode, variable name (blank keeps the default).
func envVarIO(t *testing.T, provider, envVar string) props.IO {
	t.Helper()

	return props.StdIO{
		Stdin:          formtest.Answers(strconv.Itoa(providerNumber(t, provider)), strconv.Itoa(modeNumber(t, credentials.ModeEnvVar)), envVar),
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		AccessibleMode: true,
	}
}

// keyIO drives the wizard for a mode that ends in the masked key input, which
// accessible mode cannot script, so this is the TUI route: arrow to the
// provider and the mode, type the key. Paced by time, so a test using it does
// not call t.Parallel.
func keyIO(t *testing.T, provider string, mode credentials.Mode, key string) props.IO {
	t.Helper()

	var seqs []string

	for i := 1; i < providerNumber(t, provider); i++ {
		seqs = append(seqs, formtest.Down)
	}

	seqs = append(seqs, formtest.Enter)

	// The mode cursor starts on the recommended mode, not the first.
	modes, def := modeChoices(t)
	for delta := indexOf(t, modes, mode) - indexOf(t, modes, def); delta != 0; {
		if delta > 0 {
			seqs = append(seqs, formtest.Down)
			delta--
		} else {
			seqs = append(seqs, formtest.Up)
			delta++
		}
	}

	seqs = append(seqs, formtest.Enter)

	if key != "" {
		seqs = append(seqs, key)
	}

	seqs = append(seqs, formtest.Enter)

	return formtest.TUI(formtest.Keys(seqs...))
}

// localIO answers for a provider that needs no credential: the wizard ends
// at the provider.
func localIO(t *testing.T, provider string) props.IO {
	t.Helper()

	return props.StdIO{Stdin: formtest.Answers(strconv.Itoa(providerNumber(t, provider))), Stdout: io.Discard, Stderr: io.Discard, AccessibleMode: true}
}

// nonInteractiveIO is a stdin nobody is typing at: the wizard must refuse.
func nonInteractiveIO() props.IO {
	return props.StdIO{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard}
}

var allLinked = func(gochat.Provider) bool { return true }

// requireRefusedAsNonInteractive is what a wizard does with nobody at the
// terminal: refuses before the form opens.
func requireRefusedAsNonInteractive(t *testing.T, err error) {
	t.Helper()

	require.ErrorIs(t, err, setup.ErrNonInteractive, "got %v", err)
}
