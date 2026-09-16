package formtest_test

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
)

func readAll(t *testing.T, r io.Reader) []string {
	t.Helper()

	var chunks []string
	buf := make([]byte, 64)

	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunks = append(chunks, string(buf[:n]))
		}

		if err == io.EOF {
			return chunks
		}

		require.NoError(t, err)
	}
}

// TestAnswers_OneLinePerRead pins what huh's accessible mode needs: each
// field's fresh buffered reader sees exactly one answer.
func TestAnswers_OneLinePerRead(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"2\n", "MY_VAR\n", "y\n"}, readAll(t, formtest.Answers("2", "MY_VAR", "y")))
}

// TestKeys_OneSequencePerRead pins what the key parser needs: a named
// sequence arrives whole, typed text arrives a rune at a time.
func TestKeys_OneSequencePerRead(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"\x1b[B", "\r", "h", "é", "\r"}, readAll(t, formtest.Keys(formtest.Down, formtest.Enter, "hé", formtest.Enter)))
}

// TestTUI: an IO that runs a form headless on the key route.
func TestTUI(t *testing.T) {
	t.Parallel()

	in := formtest.Keys(formtest.Enter)
	io := formtest.TUI(in)

	assert.Same(t, in, io.In())
	assert.True(t, io.Interactive(), "the form must think a person is there")
	assert.False(t, io.Accessible(), "and take the TUI route")

	_, err := io.Out().Write([]byte("x"))
	require.NoError(t, err)
	_, err = io.Err().Write([]byte("x"))
	require.NoError(t, err)

	assert.Len(t, formtest.ProgramOptions(in), 5)
}

func TestTUIForms_HandsEachFormItsOwnScript(t *testing.T) {
	t.Parallel()

	io := formtest.TUIForms(formtest.Keys("a"), formtest.Keys("b"))

	assert.Equal(t, []string{"a"}, readAll(t, io.In()))
	assert.Equal(t, []string{"b"}, readAll(t, io.In()))
	assert.Empty(t, readAll(t, io.In()), "a form past the last script reads nothing")

	assert.True(t, io.Interactive())
	assert.False(t, io.Accessible())
}
