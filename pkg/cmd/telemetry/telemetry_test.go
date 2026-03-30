package telemetry_test

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
	cmdtelemetry "github.com/phpboyscout/go-tool-base/pkg/cmd/telemetry"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func setupTestProps(t *testing.T) (*props.Props, *viper.Viper) {
	t.Helper()

	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.yaml")

	require.NoError(t, os.WriteFile(cfgFile, []byte("telemetry:\n  enabled: false\n"), 0o600))

	v := viper.New()
	v.SetConfigFile(cfgFile)
	require.NoError(t, v.ReadInConfig())

	mock := mockcfg.NewMockContainable(t)
	mock.On("Set", testifymock.Anything, testifymock.Anything).Run(func(args testifymock.Arguments) {
		v.Set(args.String(0), args.Get(1))
	}).Return().Maybe()
	mock.On("GetViper").Return(v).Maybe()
	mock.On("GetBool", testifymock.Anything).Return(false).Maybe()

	p := &props.Props{
		Tool:   props.Tool{Name: "test-tool"},
		Logger: logger.NewNoop(),
		Config: mock,
	}

	return p, v
}

func TestEnableCmd(t *testing.T) {
	t.Parallel()

	p, v := setupTestProps(t)
	cmd := cmdtelemetry.NewCmdTelemetry(p)

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"enable"})

	require.NoError(t, cmd.Execute())
	assert.True(t, v.GetBool("telemetry.enabled"), "telemetry.enabled should be true after enable")
}

func TestDisableCmd(t *testing.T) {
	t.Parallel()

	p, v := setupTestProps(t)
	v.Set("telemetry.enabled", true)

	cmd := cmdtelemetry.NewCmdTelemetry(p)
	cmd.SetArgs([]string{"disable"})

	require.NoError(t, cmd.Execute())
	assert.False(t, v.GetBool("telemetry.enabled"), "telemetry.enabled should be false after disable")
}

func TestStatusCmd_Disabled(t *testing.T) {
	t.Parallel()

	p, _ := setupTestProps(t)
	buf := logger.NewBuffer()
	p.Logger = buf

	cmd := cmdtelemetry.NewCmdTelemetry(p)
	cmd.SetArgs([]string{"status"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "disabled")
}

func TestStatusCmd_Enabled(t *testing.T) {
	t.Parallel()

	p, _ := setupTestProps(t)

	// Override GetBool to return true for telemetry.enabled
	mock := mockcfg.NewMockContainable(t)
	mock.On("GetBool", "telemetry.enabled").Return(true)
	mock.On("GetBool", "telemetry.local_only").Return(false)
	p.Config = mock

	buf := logger.NewBuffer()
	p.Logger = buf

	cmd := cmdtelemetry.NewCmdTelemetry(p)
	cmd.SetArgs([]string{"status"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "enabled")
}

func TestStatusCmd_LocalOnly(t *testing.T) {
	t.Parallel()

	p, _ := setupTestProps(t)

	mock := mockcfg.NewMockContainable(t)
	mock.On("GetBool", "telemetry.enabled").Return(true)
	mock.On("GetBool", "telemetry.local_only").Return(true)
	p.Config = mock

	buf := logger.NewBuffer()
	p.Logger = buf

	cmd := cmdtelemetry.NewCmdTelemetry(p)
	cmd.SetArgs([]string{"status"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "local-only")
}
