package forge

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"strings"
	"testing"

	"gitlab.com/phpboyscout/go/credentials"

	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// The wizards are driven the way a person drives them (spec 0198 D3): the
// real forms, on Props.IO, at accessible prompts. What a form shows (the
// display-once token) is read back from the IO's output.

// modeNumber is the 1-based position of mode among the choices the selector
// offers in this environment (keychain only when a backend is linked and
// answers; literal only outside CI).
func modeNumber(t *testing.T, mode credentials.Mode) string {
	t.Helper()

	choices, _ := credentialposture.StorageModeOptions(context.Background(), credentialposture.ModeLabels{Env: "e", Keychain: "k", Literal: "l"})
	for i, c := range choices {
		if c.Mode == mode {
			return strconv.Itoa(i + 1)
		}
	}

	t.Fatalf("mode %q is not offered here (choices: %v)", mode, choices)

	return ""
}

// yn is what a person types at a confirm.
func yn(v bool) string {
	if v {
		return "y"
	}

	return "n"
}

// answersIO answers the given lines at accessible prompts and captures
// everything the forms print.
func answersIO(lines ...string) (props.IO, *bytes.Buffer) {
	out := &bytes.Buffer{}

	return props.StdIO{Stdin: formtest.Answers(lines...), Stdout: out, Stderr: out, AccessibleMode: true}, out
}

// singleAuthIO answers the single-token wizard at accessible prompts. huh's
// accessible mode asks every field whatever the hide functions say (v2.0.3,
// form.go runAccessible), so the env-var name and the fetch decision are
// answered for every mode; the wizard ignores them outside env-var mode. When
// a token is fetched, the display-once page is acknowledged.
func singleAuthIO(t *testing.T, mode credentials.Mode, envVar string, fetch bool) (props.IO, *bytes.Buffer) {
	t.Helper()

	lines := []string{modeNumber(t, mode), envVar, yn(fetch)}

	if mode == credentials.ModeEnvVar && fetch {
		lines = append(lines, "y") // "Have you saved the token?"
	}

	return answersIO(lines...)
}

// nonInteractiveIO is a stdin nobody is typing at: the wizard must refuse.
func nonInteractiveIO() props.IO {
	return props.StdIO{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard}
}
