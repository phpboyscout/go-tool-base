package config_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestCmdList_AllKeysDisplayed(t *testing.T) {
	t.Parallel()

	p := newTestProps(t, "log:\n  level: info\ntool:\n  name: myapp\n")
	cmd := config.NewCmdList(p, config.NewMasker())

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.Contains(t, out, "log.level")
	assert.Contains(t, out, "info")
	assert.Contains(t, out, "tool.name")
	assert.Contains(t, out, "myapp")
}

func TestCmdList_SensitiveValueMasked(t *testing.T) {
	t.Parallel()

	p := newTestProps(t, "log:\n  level: info\ngithub:\n  auth:\n    token: supersecrettoken123\n")
	cmd := config.NewCmdList(p, config.NewMasker())

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.Contains(t, out, "log.level")
	assert.Contains(t, out, "github.auth.token")
	assert.NotContains(t, out, "supersecrettoken123")
}

func TestCmdList_GithubPATMaskedByValue(t *testing.T) {
	t.Parallel()

	token := "ghp_" + strings.Repeat("Z", 36)

	p := newTestProps(t, "github:\n  auth:\n    value: "+token+"\n")
	cmd := config.NewCmdList(p, config.NewMasker())

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.NotContains(t, out, token)
}

func TestCmdList_AlphabeticallySorted(t *testing.T) {
	t.Parallel()

	p := newTestProps(t, "z:\n  last: val\na:\n  first: val\nm:\n  middle: val\n")
	cmd := config.NewCmdList(p, config.NewMasker())

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())

	out := buf.String()
	posA := strings.Index(out, "a.first")
	posM := strings.Index(out, "m.middle")
	posZ := strings.Index(out, "z.last")

	assert.Less(t, posA, posM)
	assert.Less(t, posM, posZ)
}

func TestCmdList_NilConfig(t *testing.T) {
	t.Parallel()

	p := &props.Props{Config: nil}
	cmd := config.NewCmdList(p, config.NewMasker())

	require.Error(t, cmd.Execute())
}

func TestCmdList_JSONOutput(t *testing.T) {
	t.Parallel()

	p := newTestProps(t, "log:\n  level: warn\n")

	// --output is a root-level persistent flag; add it to the test command root.
	root := config.NewCmdConfig(p)
	root.PersistentFlags().String("output", "text", "output format")
	root.SetArgs([]string{"list", "--output", "json"})

	var buf bytes.Buffer
	root.SetOut(&buf)

	require.NoError(t, root.Execute())

	out := buf.String()
	assert.Contains(t, out, `"key"`)
	assert.Contains(t, out, `"value"`)
	assert.Contains(t, out, "log.level")
	assert.Contains(t, out, "warn")
}
