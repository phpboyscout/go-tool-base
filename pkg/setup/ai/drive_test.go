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
// the real form, on Props.IO, at accessible prompts: option numbers and
// lines.

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
// CI) and the one the cursor starts on, which depends on whether the IO the
// form runs on is a terminal (keychain is only recommended to a human).
func modeChoices(t *testing.T, io props.IO) ([]credentials.Mode, credentials.Mode) {
	t.Helper()

	choices, def := credentialposture.StorageModeOptions(context.Background(), io, credentialposture.ModeLabels{Env: "e", Keychain: "k", Literal: "l"})

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

	// Accessible answers come from a reader that is not a terminal.
	modes, _ := modeChoices(t, props.StdIO{Stdin: strings.NewReader("")})

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

// keyIO answers the wizard at accessible prompts for a mode that ends in the
// key: provider, mode, key (blank keeps the existing one). A piped stdin takes
// the key as a plain line (setup.SecretEchoMode), so this needs no TUI and no
// pacing (#135).
func keyIO(t *testing.T, provider string, mode credentials.Mode, key string) props.IO {
	t.Helper()

	return props.StdIO{
		Stdin:          formtest.Answers(strconv.Itoa(providerNumber(t, provider)), strconv.Itoa(modeNumber(t, mode)), key),
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		AccessibleMode: true,
	}
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
