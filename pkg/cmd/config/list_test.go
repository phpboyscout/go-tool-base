package config_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockcfg "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func TestCmdList_AllKeysDisplayed(t *testing.T) {
	t.Parallel()

	v := viper.New()
	v.Set("log.level", "info")
	v.Set("tool.name", "myapp")

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().GetViper().Return(v)

	p := &props.Props{Config: mock}
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

	v := viper.New()
	v.Set("log.level", "info")
	v.Set("github.auth.token", "supersecrettoken123")

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().GetViper().Return(v)

	p := &props.Props{Config: mock}
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

	v := viper.New()
	v.Set("github.auth.value", token)

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().GetViper().Return(v)

	p := &props.Props{Config: mock}
	cmd := config.NewCmdList(p, config.NewMasker())

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.NotContains(t, out, token)
}

func TestCmdList_AlphabeticallySorted(t *testing.T) {
	t.Parallel()

	v := viper.New()
	v.Set("z.last", "val")
	v.Set("a.first", "val")
	v.Set("m.middle", "val")

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().GetViper().Return(v)

	p := &props.Props{Config: mock}
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

	v := viper.New()
	v.Set("log.level", "warn")

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().GetViper().Return(v)

	p := &props.Props{Config: mock}

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
