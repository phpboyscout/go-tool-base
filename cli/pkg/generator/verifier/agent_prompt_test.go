package verifier

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// The query_user prompt ran without the form guard, so piped input that ended
// after an answer not listed panicked, and an empty pipe took the first option
// (#254).
func TestAgentVerifier_AskUserOnPipedInput(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		suggestions []string
		answers     string
		want        string
		ended       bool
	}{
		"a suggestion":                 {suggestions: []string{"keep", "drop"}, answers: "2\n", want: "drop"},
		"other, then text":             {suggestions: []string{"keep", "drop"}, answers: "3\nmy own\n", want: "my own"},
		"nothing":                      {suggestions: []string{"keep", "drop"}, ended: true},
		"one not listed, then nothing": {suggestions: []string{"keep", "drop"}, answers: "9\n", ended: true},
		"other, then nothing":          {suggestions: []string{"keep", "drop"}, answers: "3\n", ended: true},
		"free text":                    {answers: "my own\n", want: "my own"},
		"free text, nothing":           {ended: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			v := NewAgentVerifier(&props.Props{
				Logger: logger.NewNoop(),
				IO:     props.StdIO{Stdin: strings.NewReader(tc.answers), Stdout: io.Discard, Stderr: io.Discard, AccessibleMode: true},
			}, true)

			got, err := v.askUser(t.Context(), "Which?", tc.suggestions)
			if tc.ended {
				require.ErrorIs(t, err, setup.ErrInputEnded)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// pipe is piped input with no terminal and no accessible mode, whatever TERM says.
type pipe struct{ in io.Reader }

func (p pipe) In() io.Reader   { return p.in }
func (pipe) Out() io.Writer    { return io.Discard }
func (pipe) Err() io.Writer    { return io.Discard }
func (pipe) Interactive() bool { return false }
func (pipe) Accessible() bool  { return false }

func TestAgentVerifier_AskUserRefusesWithNobodyToAnswer(t *testing.T) {
	t.Parallel()

	v := NewAgentVerifier(&props.Props{Logger: logger.NewNoop(), IO: pipe{in: strings.NewReader("1\n")}}, true)

	_, err := v.askUser(t.Context(), "Which?", []string{"keep"})
	require.ErrorIs(t, err, setup.ErrNonInteractive)
}
