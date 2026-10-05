package setup_test

import (
	"io"
	"strings"
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// TestRunFormOn_AccessibleAnswersOnAPipeAllArrive: huh builds a fresh scanner
// for every accessible prompt, and a scanner over a pipe reads ahead, so
// every answer after the first was lost with the scanner that read it. The
// runner hands the form one line per Read, so each prompt can take no more
// than its own answer.
func TestRunFormOn_AccessibleAnswersOnAPipeAllArrive(t *testing.T) {
	t.Parallel()

	var first, second, third string

	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("First").Value(&first),
		huh.NewInput().Title("Second").Value(&second),
	), huh.NewGroup(
		huh.NewInput().Title("Third").Value(&third),
	))

	pipe := props.StdIO{Stdin: strings.NewReader("one\ntwo\nthree\n"), Stdout: io.Discard, Stderr: io.Discard, AccessibleMode: true}
	require.NoError(t, setup.RunFormOn(t.Context(), pipe, form))

	assert.Equal(t, "one", first)
	assert.Equal(t, "two", second, "the second prompt must see its own line, not an empty read")
	assert.Equal(t, "three", third, "and so must a prompt on a later page")
}

// TestRunFormOn_FormsInTurnOnOnePipeEachGetTheirAnswers: a wizard that runs
// several forms reads one stdin. Each form gets its own line reader, so a
// reader that buffered ahead would keep the next form's answers.
func TestRunFormOn_FormsInTurnOnOnePipeEachGetTheirAnswers(t *testing.T) {
	t.Parallel()

	var first, second string

	pipe := props.StdIO{Stdin: strings.NewReader("one\ntwo\n"), Stdout: io.Discard, Stderr: io.Discard, AccessibleMode: true}

	require.NoError(t, setup.RunFormOn(t.Context(), pipe, huh.NewForm(huh.NewGroup(huh.NewInput().Title("First").Value(&first)))))
	require.NoError(t, setup.RunFormOn(t.Context(), pipe, huh.NewForm(huh.NewGroup(huh.NewInput().Title("Second").Value(&second)))))

	assert.Equal(t, "one", first)
	assert.Equal(t, "two", second, "the second form must find its answer still on the pipe")
}

// TestRunFormOn_InputThatEndsEarlyFails: huh's accessible prompts take their
// default when stdin ends, skipping the validator, and the form reports
// success. Running out of answers is a failure, not a set of defaults.
func TestRunFormOn_InputThatEndsEarlyFails(t *testing.T) {
	t.Parallel()

	required := func(s string) error {
		if s == "" {
			return errors.New("required")
		}

		return nil
	}

	tests := []struct {
		name    string
		input   string
		wantErr bool
		want    [2]string
	}{
		{name: "every answer given", input: "one\ntwo\n", want: [2]string{"one", "two"}},
		{name: "a last answer without a newline", input: "one\ntwo", want: [2]string{"one", "two"}},
		{name: "an empty line takes the default", input: "one\n\n", want: [2]string{"one", "fallback"}},
		{name: "answers that run out", input: "one\n", wantErr: true},
		{name: "no answers at all", input: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			first, second := "", "fallback"
			form := huh.NewForm(huh.NewGroup(
				huh.NewInput().Title("First").Value(&first).Validate(required),
				huh.NewInput().Title("Second").Value(&second),
			))

			pipe := props.StdIO{Stdin: strings.NewReader(tc.input), Stdout: io.Discard, Stderr: io.Discard, AccessibleMode: true}
			err := setup.RunFormOn(t.Context(), pipe, form)

			if tc.wantErr {
				require.ErrorIs(t, err, setup.ErrInputEnded)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, [2]string{first, second})
		})
	}
}

// TestPromptable: one rule for every prompt gate.
func TestPromptable(t *testing.T) {
	t.Parallel()

	pipe := props.StdIO{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard}
	assert.False(t, setup.Promptable(pipe), "a plain pipe cannot be prompted")

	pipe.AccessibleMode = true
	assert.True(t, setup.Promptable(pipe), "a pipe with accessible asked for takes line prompts")
}
