package config_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockcfg "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func TestCmdSet_WritesValue(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.yaml")

	require.NoError(t, os.WriteFile(cfgFile, []byte("log:\n  level: info\n"), 0o600))

	v := viper.New()
	v.SetConfigFile(cfgFile)
	require.NoError(t, v.ReadInConfig())

	mock := mockcfg.NewMockContainable(t)
	mock.On("Set", "log.level", "debug").Run(func(args testifymock.Arguments) {
		v.Set(args.String(0), args.Get(1))
	}).Return()
	mock.EXPECT().GetViper().Return(v)

	p := &props.Props{Config: mock}
	cmd := config.NewCmdSet(p)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"log.level", "debug"})

	require.NoError(t, cmd.Execute())

	assert.Contains(t, buf.String(), "log.level")
	assert.Contains(t, buf.String(), "debug")

	// Verify the value was actually written to disk
	v2 := viper.New()
	v2.SetConfigFile(cfgFile)
	require.NoError(t, v2.ReadInConfig())
	assert.Equal(t, "debug", v2.GetString("log.level"))
}

func TestCmdSet_NilConfig(t *testing.T) {
	t.Parallel()

	p := &props.Props{Config: nil}
	cmd := config.NewCmdSet(p)
	cmd.SetArgs([]string{"log.level", "debug"})

	err := cmd.Execute()
	assert.Error(t, err)
}

func TestCmdSet_CoerceBool(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.yaml")
	require.NoError(t, os.WriteFile(cfgFile, []byte("feature:\n  enabled: false\n"), 0o600))

	v := viper.New()
	v.SetConfigFile(cfgFile)
	require.NoError(t, v.ReadInConfig())

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().Set("feature.enabled", true)
	mock.EXPECT().GetViper().Return(v)

	p := &props.Props{Config: mock}
	cmd := config.NewCmdSet(p)
	cmd.SetArgs([]string{"feature.enabled", "true"})

	require.NoError(t, cmd.Execute())
}

func TestCmdSet_CoerceInt(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.yaml")
	require.NoError(t, os.WriteFile(cfgFile, []byte("server:\n  port: 8080\n"), 0o600))

	v := viper.New()
	v.SetConfigFile(cfgFile)
	require.NoError(t, v.ReadInConfig())

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().Set("server.port", int64(9090))
	mock.EXPECT().GetViper().Return(v)

	p := &props.Props{Config: mock}
	cmd := config.NewCmdSet(p)
	cmd.SetArgs([]string{"server.port", "9090"})

	require.NoError(t, cmd.Execute())
}
