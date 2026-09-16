package root_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	propstest "gitlab.com/phpboyscout/go-tool-base/pkg/props/test"
)

// TestRootFillsIOFromTheCommand pins spec 0198 D1: a test's SetIn/SetOut on
// the root reach Props.IO before any command runs, including one on the init
// fast path; --accessible is recorded; an IO wired ahead of time is kept.
func TestRootFillsIOFromTheCommand(t *testing.T) {
	t.Setenv("GTB_ACCESSIBLE", "")

	p := propstest.New(propstest.WithTool(props.Tool{Name: "iotool"}))
	cmd := root.NewCmdRoot(p)

	in, out := strings.NewReader("typed"), &bytes.Buffer{}
	cmd.SetIn(in)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"--accessible", "version"})
	require.NoError(t, cmd.Execute())

	io := p.GetIO()
	assert.Same(t, in, io.In(), "stdin is the command's")
	assert.True(t, io.Accessible(), "--accessible is recorded on the IO")
	assert.False(t, io.Interactive(), "a strings.Reader is not a terminal")

	wired := props.StdIO{Stdin: strings.NewReader("mine")}
	p2 := propstest.New(propstest.WithTool(props.Tool{Name: "iotool"}))
	p2.IO = wired
	cmd2 := root.NewCmdRoot(p2)
	cmd2.SetArgs([]string{"version"})
	require.NoError(t, cmd2.Execute())
	assert.Equal(t, wired, p2.IO, "an IO wired ahead of the root is kept")
}
