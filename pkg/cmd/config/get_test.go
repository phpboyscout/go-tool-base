package config_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockcfg "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func newTestProps(t *testing.T) (*props.Props, *mockcfg.MockContainable) {
	t.Helper()

	mock := mockcfg.NewMockContainable(t)
	p := &props.Props{Config: mock}

	return p, mock
}

func TestCmdGet_ValueFound(t *testing.T) {
	t.Parallel()

	p, mock := newTestProps(t)
	mock.EXPECT().IsSet("log.level").Return(true)
	mock.EXPECT().Get("log.level").Return("debug")

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

	p, mock := newTestProps(t)
	mock.EXPECT().IsSet("nonexistent.key").Return(false)

	masker := config.NewMasker()
	cmd := config.NewCmdGet(p, masker)
	cmd.SetArgs([]string{"nonexistent.key"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent.key")
}

func TestCmdGet_SensitiveKeyMasked(t *testing.T) {
	t.Parallel()

	p, mock := newTestProps(t)
	mock.EXPECT().IsSet("github.auth.token").Return(true)
	mock.EXPECT().Get("github.auth.token").Return("supersecrettoken")

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

	p, mock := newTestProps(t)
	mock.EXPECT().IsSet("github.auth.token").Return(true)
	mock.EXPECT().Get("github.auth.token").Return("supersecrettoken")

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

	p, mock := newTestProps(t)
	mock.EXPECT().IsSet("github.auth.value").Return(true)
	mock.EXPECT().Get("github.auth.value").Return(token)

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

	p, mock := newTestProps(t)
	mock.EXPECT().IsSet("log.level").Return(true)
	mock.EXPECT().Get("log.level").Return("info")

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
