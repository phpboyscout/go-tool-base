package setup_test

import (
	"io"
	"strings"
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
