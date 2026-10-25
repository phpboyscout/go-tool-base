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

// modeChoices are the storage modes the selector offers in this environment
// (keychain only when a backend is linked and answers; literal only outside
// CI) and the one the cursor starts on.
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
func modeNumber(t *testing.T, mode credentials.Mode) string {
	t.Helper()

	// Accessible answers come from a reader that is not a terminal.
	modes, _ := modeChoices(t, props.StdIO{Stdin: strings.NewReader("")})

	return strconv.Itoa(indexOf(t, modes, mode) + 1)
}

// modeKeys are the arrow presses that move the selector's cursor from the
// mode it starts on (the recommended one) to mode, then Enter.
func modeKeys(t *testing.T, mode credentials.Mode) []string {
	t.Helper()

	var seqs []string

	// The key route is a terminal, where keychain (when usable) is the
	// recommended mode the cursor starts on.
	modes, def := modeChoices(t, formtest.TUI(strings.NewReader("")))
	for delta := indexOf(t, modes, mode) - indexOf(t, modes, def); delta != 0; {
		if delta > 0 {
			seqs = append(seqs, formtest.Down)
			delta--
		} else {
			seqs = append(seqs, formtest.Up)
			delta++
		}
	}

	return append(seqs, formtest.Enter)
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

// singleAuthIO answers the single-token wizard at accessible prompts: the
// mode, and in env-var mode the variable name, the fetch decision, and the
// display-once acknowledgement when a token is fetched. The other modes
// never see those pages.
func singleAuthIO(t *testing.T, mode credentials.Mode, envVar string, fetch bool) (props.IO, *bytes.Buffer) {
	t.Helper()

	lines := []string{modeNumber(t, mode)}

	if mode == credentials.ModeEnvVar {
		lines = append(lines, envVar, yn(fetch))

		if fetch {
			lines = append(lines, "y") // "Have you saved the token?"
		}
	}

	return answersIO(lines...)
}

// nonInteractiveIO is a stdin nobody is typing at: the wizard must refuse.
func nonInteractiveIO() props.IO {
	return props.StdIO{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard}
}

// dualEnvIO answers the dual-credential wizard for env-var mode at accessible
// prompts: the mode and the two variable names (blank keeps the fallback).
func dualEnvIO(t *testing.T, userEnv, passEnv string) props.IO {
	t.Helper()

	io, _ := answersIO(modeNumber(t, credentials.ModeEnvVar), userEnv, passEnv)

	return io
}

// dualCredentialIO answers the dual-credential wizard at accessible prompts
// for a mode that takes the username and app password themselves. A piped
// stdin takes the password as a plain line (setup.SecretEchoMode).
func dualCredentialIO(t *testing.T, mode credentials.Mode, username, password string) props.IO {
	t.Helper()

	io, _ := answersIO(modeNumber(t, mode), username, password)

	return io
}

// testPassphrase is long enough for the generated key's passphrase rule.
const testPassphrase = "correct-horse-battery-staple"

// sshChoiceIndex is choice's position in the key selector, after the keys
// discovered under p's ~/.ssh.
func sshChoiceIndex(t *testing.T, p *props.Props, choice string) int {
	t.Helper()

	keys, err := discoverSSHKeys(p)
	if err != nil {
		t.Fatalf("discovering keys: %v", err)
	}

	for i, k := range keys {
		if k.Value == choice {
			return i
		}
	}

	sentinels := map[string]int{sshChoiceGenerate: 0, sshChoiceAgent: 1, sshChoiceOther: 2}

	offset, ok := sentinels[choice]
	if !ok {
		t.Fatalf("choice %q is neither a discovered key nor a sentinel", choice)
	}

	return len(keys) + offset
}

// sshChoiceNumber is what an accessible prompt takes for choice.
func sshChoiceNumber(t *testing.T, p *props.Props, choice string) string {
	t.Helper()

	return strconv.Itoa(sshChoiceIndex(t, p, choice) + 1)
}

// sshChoiceKeys move the selector's cursor from the first option to choice,
// then Enter.
func sshChoiceKeys(t *testing.T, p *props.Props, choice string) []string {
	t.Helper()

	var seqs []string

	for range sshChoiceIndex(t, p, choice) {
		seqs = append(seqs, formtest.Down)
	}

	return append(seqs, formtest.Enter)
}

// sshGenerateAnswers answer the SSH stage at accessible prompts to generate
// a key: the selector, the passphrase, then the upload question. The
// passphrase page shows because the caller starts an agent and links no
// keychain.
func sshGenerateAnswers(t *testing.T, p *props.Props, upload bool) []string {
	t.Helper()

	return []string{sshChoiceNumber(t, p, sshChoiceGenerate), testPassphrase, yn(upload)}
}
