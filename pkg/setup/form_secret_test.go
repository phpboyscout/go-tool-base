package setup_test

import (
	"io"
	"strings"
	"testing"
	"time"

	"charm.land/huh/v2"
	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// huh reads an accessible password straight from the terminal, so the input
// it is handed must expose the terminal's descriptor; the line reader that
// wraps stdin for piped answers hid it, and the secret came back empty (#108).
func TestRunFormOn_AccessibleSecretAtATerminal(t *testing.T) {
	t.Parallel()

	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no pseudo-terminal here: %v", err)
	}

	t.Cleanup(func() {
		_ = ptmx.Close()
		_ = tty.Close()
	})

	var secret string

	form := huh.NewForm(huh.NewGroup(huh.NewInput().Title("Secret").EchoMode(huh.EchoModePassword).Value(&secret)))
	terminal := props.StdIO{Stdin: tty, Stdout: io.Discard, Stderr: io.Discard, AccessibleMode: true}

	done := make(chan error, 1)

	go func() { done <- setup.RunFormOn(t.Context(), terminal, form) }()

	time.Sleep(200 * time.Millisecond) // let the prompt put the terminal in raw mode
	_, err = ptmx.Write([]byte("hunter2\r"))
	require.NoError(t, err)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the secret prompt did not return")
	}

	assert.Equal(t, "hunter2", secret)
}

func TestSecretEchoMode(t *testing.T) {
	t.Parallel()

	pipe := props.StdIO{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard}
	assert.Equal(t, huh.EchoModePassword, setup.SecretEchoMode(pipe), "the TUI hides a secret itself")

	pipe.AccessibleMode = true
	assert.Equal(t, huh.EchoModeNormal, setup.SecretEchoMode(pipe), "nothing echoes on a pipe, and huh cannot hide it there")

	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no pseudo-terminal here: %v", err)
	}

	t.Cleanup(func() {
		_ = ptmx.Close()
		_ = tty.Close()
	})

	terminal := props.StdIO{Stdin: tty, Stdout: io.Discard, Stderr: io.Discard, AccessibleMode: true}
	assert.Equal(t, huh.EchoModePassword, setup.SecretEchoMode(terminal), "a terminal hides it")
}
