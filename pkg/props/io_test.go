package props_test

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestStdIO pins spec 0198 D1: the zero value is the process's streams, a
// supplied stream wins, and interactivity means "a terminal", not "a
// character device" (which /dev/null is).
func TestStdIO(t *testing.T) {
	t.Parallel()

	var zero props.StdIO
	assert.Same(t, os.Stdin, zero.In())
	assert.Same(t, os.Stdout, zero.Out())
	assert.Same(t, os.Stderr, zero.Err())

	in, out := strings.NewReader("x"), &bytes.Buffer{}
	given := props.StdIO{Stdin: in, Stdout: out, Stderr: io.Discard}
	assert.Same(t, in, given.In())
	assert.Same(t, out, given.Out())
	assert.Equal(t, io.Discard, given.Err())
	assert.False(t, given.Interactive(), "a strings.Reader is not a terminal")

	devnull, err := os.Open(os.DevNull)
	require.NoError(t, err)
	defer func() { _ = devnull.Close() }()

	assert.False(t, props.StdIO{Stdin: devnull}.Interactive(), "/dev/null is a character device but not a terminal")
}

// TestStdIO_Accessible: the explicit switch or huh's own TERM=dumb rule.
func TestStdIO_Accessible(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	assert.False(t, props.StdIO{}.Accessible())
	assert.True(t, props.StdIO{AccessibleMode: true}.Accessible())

	t.Setenv("TERM", "dumb")
	assert.True(t, props.StdIO{}.Accessible(), "TERM=dumb is what huh itself honours")
}

// TestProps_GetIO: a hand-built Props with no IO reads as the process's.
func TestProps_GetIO(t *testing.T) {
	t.Parallel()

	var p props.Props
	require.NotNil(t, p.GetIO())
	assert.Same(t, os.Stdin, p.GetIO().In())

	p.IO = props.StdIO{Stdin: strings.NewReader("")}
	assert.NotSame(t, os.Stdin, p.GetIO().In())
}
