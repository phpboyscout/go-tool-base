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
func modeNumber(t *testing.T, mode credentials.Mode) string {
	t.Helper()

	modes, _ := modeChoices(t)

	return strconv.Itoa(indexOf(t, modes, mode) + 1)
}

// modeKeys are the arrow presses that move the selector's cursor from the
// mode it starts on (the recommended one) to mode, then Enter.
func modeKeys(t *testing.T, mode credentials.Mode) []string {
	t.Helper()

	var seqs []string

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

// dualEnvIO answers the dual-credential wizard for env-var mode at accessible
// prompts: the mode, the two variable names (blank keeps the fallback), and a
// throwaway username for the credential page accessible mode asks anyway (its
// password prompt cannot be answered without a terminal and is left blank).
func dualEnvIO(t *testing.T, userEnv, passEnv string) props.IO {
	t.Helper()

	io, _ := answersIO(modeNumber(t, credentials.ModeEnvVar), userEnv, passEnv, "ignored")

	return io
}

// dualCredentialIO drives the dual-credential wizard for a mode that takes
// the username and app password themselves, which only the TUI can do (the
// password field). Paced by time, so a test using it does not call
// t.Parallel.
func dualCredentialIO(t *testing.T, mode credentials.Mode, username, password string) props.IO {
	t.Helper()

	seqs := modeKeys(t, mode)
	seqs = append(seqs, username, formtest.Enter, password, formtest.Enter)

	return formtest.TUI(formtest.Keys(seqs...))
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

// sshGenerateScripts drive the SSH stage to generate a key, one script per
// form it runs: the selector and the passphrase (a password field, so keys
// not answers), then the upload question. Paced by time, so a test using
// them does not call t.Parallel.
func sshGenerateScripts(t *testing.T, p *props.Props, upload bool) []io.Reader {
	t.Helper()

	seqs := sshChoiceKeys(t, p, sshChoiceGenerate)
	seqs = append(seqs, testPassphrase, formtest.Enter)

	answer := formtest.No
	if upload {
		answer = formtest.Yes
	}

	return []io.Reader{formtest.Keys(seqs...), formtest.Keys(answer)}
}

// dualEnvKeys drive the dual-credential form for env-var mode with the
// fallback names: the cursor starts on env-var (the recommended mode) and two
// blank names take the defaults.
func dualEnvKeys(t *testing.T) []string {
	t.Helper()

	return append(modeKeys(t, credentials.ModeEnvVar), formtest.Enter, formtest.Enter)
}
