package telemetry_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	cmdtelemetry "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/telemetry"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// setupTestProps builds Props whose store is backed by a real temp config
// file, so enable/disable exercise the Apply write path end to end. Props.FS
// is a MemMapFs: the default-config-dir ensure lands there harmlessly rather
// than in the developer's real home directory.
func setupTestProps(t *testing.T) (*props.Props, string) {
	t.Helper()

	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.yaml")

	require.NoError(t, os.WriteFile(cfgFile, []byte("telemetry:\n  enabled: false\n"), 0o600))

	store, err := config.NewStore(t.Context(), config.WithFiles(config.OS(), cfgFile))
	require.NoError(t, err)

	p := &props.Props{
		Tool:   props.Tool{Name: "test-tool"},
		Logger: logger.NewNoop(),
		Config: store,
		FS:     afero.NewMemMapFs(),
		// These tests exercise commands directly, bypassing the root bootstrap
		// that would otherwise default the collector; set the noop explicitly to
		// honour the always-non-nil Collector invariant.
		Collector: props.NoopCollector{},
	}

	return p, cfgFile
}

func TestEnableCmd(t *testing.T) {
	t.Parallel()

	p, cfgFile := setupTestProps(t)
	cmd := cmdtelemetry.NewCmdTelemetry(p)

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"enable"})

	require.NoError(t, cmd.Execute())
	assert.True(t, p.Config.View().GetBool("telemetry.enabled"), "telemetry.enabled should be true after enable")

	// The write edited the target document in place.
	data, err := os.ReadFile(cfgFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "enabled: true")
}

func TestDisableCmd(t *testing.T) {
	t.Parallel()

	p, cfgFile := setupTestProps(t)
	require.NoError(t, os.WriteFile(cfgFile, []byte("telemetry:\n  enabled: true\n"), 0o600))
	require.NoError(t, p.Config.Reload(t.Context()))

	cmd := cmdtelemetry.NewCmdTelemetry(p)
	cmd.SetArgs([]string{"disable"})

	require.NoError(t, cmd.Execute())
	assert.False(t, p.Config.View().GetBool("telemetry.enabled"), "telemetry.enabled should be false after disable")
}

func TestStatusCmd_Disabled(t *testing.T) {
	t.Parallel()

	p, _ := setupTestProps(t)

	cmd := cmdtelemetry.NewCmdTelemetry(p)

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"status"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "disabled")
}

func TestStatusCmd_Enabled(t *testing.T) {
	t.Parallel()

	p, _ := setupTestProps(t)
	p.Config = testutil.StoreFromYAML(t, "telemetry:\n  enabled: true\n  local_only: false\n")

	cmd := cmdtelemetry.NewCmdTelemetry(p)

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"status"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "enabled")
}

func TestStatusCmd_LocalOnly(t *testing.T) {
	t.Parallel()

	p, _ := setupTestProps(t)
	p.Config = testutil.StoreFromYAML(t, "telemetry:\n  enabled: true\n  local_only: true\n")

	cmd := cmdtelemetry.NewCmdTelemetry(p)

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"status"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "local-only")
}
