package config_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// newTestProps returns Props whose config store resolves the given YAML
// document, matching what a loaded config file provides at runtime.
func newTestProps(t *testing.T, yaml string) *props.Props {
	t.Helper()

	return &props.Props{Config: testutil.StoreFromYAML(t, yaml)}
}

func TestCmdGet_ValueFound(t *testing.T) {
	t.Parallel()

	p := newTestProps(t, "log:\n  level: debug\n")

	masker := config.NewMasker()
	cmd := config.NewCmdGet(p, masker)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"log.level"})

	require.NoError(t, cmd.Execute())
	assert.Equal(t, "debug\n", buf.String())
}

func TestCmdGet_KeyNotFound(t *testing.T) {
	t.Parallel()

	p := newTestProps(t, "log:\n  level: info\n")

	masker := config.NewMasker()
	cmd := config.NewCmdGet(p, masker)
	cmd.SetArgs([]string{"nonexistent.key"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent.key")
}

func TestCmdGet_SensitiveKeyMasked(t *testing.T) {
	t.Parallel()

	p := newTestProps(t, "github:\n  auth:\n    token: supersecrettoken\n")

	masker := config.NewMasker()
	cmd := config.NewCmdGet(p, masker)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"github.auth.token"})

	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.NotContains(t, out, "supersecrettoken")
	assert.True(t, strings.HasSuffix(strings.TrimSpace(out), "oken"))
}

func TestCmdGet_UnmaskFlag(t *testing.T) {
	t.Parallel()

	p := newTestProps(t, "github:\n  auth:\n    token: supersecrettoken\n")

	masker := config.NewMasker()
	cmd := config.NewCmdGet(p, masker)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"github.auth.token", "--unmask"})

	require.NoError(t, cmd.Execute())
	assert.Equal(t, "supersecrettoken\n", buf.String())
}

func TestCmdGet_ValueDetectedAsPAT(t *testing.T) {
	t.Parallel()

	token := "ghp_" + strings.Repeat("A", 36)

	p := newTestProps(t, "github:\n  auth:\n    value: "+token+"\n")

	masker := config.NewMasker()
	cmd := config.NewCmdGet(p, masker)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"github.auth.value"})

	require.NoError(t, cmd.Execute())

	out := strings.TrimSpace(buf.String())
	assert.NotEqual(t, token, out)
	assert.True(t, strings.HasSuffix(out, "AAAA"))
}

func TestCmdGet_JSONOutput(t *testing.T) {
	t.Parallel()

	p := newTestProps(t, "log:\n  level: info\n")

	// --output is a root-level persistent flag; add it to the test command root.
	root := config.NewCmdConfig(p)
	root.PersistentFlags().String("output", "text", "output format")
	root.SetArgs([]string{"get", "log.level", "--output", "json"})

	var buf bytes.Buffer
	root.SetOut(&buf)

	require.NoError(t, root.Execute())

	out := buf.String()
	assert.Contains(t, out, `"value"`)
	assert.Contains(t, out, `"info"`)
}

func TestCmdGet_NilConfig(t *testing.T) {
	t.Parallel()

	p := &props.Props{Config: nil}
	masker := config.NewMasker()
	cmd := config.NewCmdGet(p, masker)
	cmd.SetArgs([]string{"log.level"})

	require.Error(t, cmd.Execute())
}
