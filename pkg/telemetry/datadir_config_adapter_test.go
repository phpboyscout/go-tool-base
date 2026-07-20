package telemetry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestResolveDataDirFromProps_ConfigDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgFile, []byte("key: value\n"), 0o600))

	store, err := config.NewStore(t.Context(), config.WithFiles(config.OS(), cfgFile))
	require.NoError(t, err)

	result := ResolveDataDirFromProps(&props.Props{Config: store})
	if result != dir {
		t.Errorf("expected %q, got %q", dir, result)
	}
}

// TestResolveDataDirFromProps_LastFileWins pins the Viper-parity rule the
// adapter preserves: ConfigFileUsed() reported the last file loaded, so the
// highest-precedence file layer decides the data directory.
func TestResolveDataDirFromProps_LastFileWins(t *testing.T) {
	t.Parallel()

	first := t.TempDir()
	second := t.TempDir()
	firstFile := filepath.Join(first, "config.yaml")
	secondFile := filepath.Join(second, "config.yaml")
	require.NoError(t, os.WriteFile(firstFile, []byte("a: 1\n"), 0o600))
	require.NoError(t, os.WriteFile(secondFile, []byte("b: 2\n"), 0o600))

	store, err := config.NewStore(t.Context(), config.WithFiles(config.OS(), firstFile, secondFile))
	require.NoError(t, err)

	result := ResolveDataDirFromProps(&props.Props{Config: store})
	if result != second {
		t.Errorf("expected %q, got %q", second, result)
	}
}

func TestResolveDataDirFromProps_NilConfig(t *testing.T) {
	t.Parallel()

	result := ResolveDataDirFromProps(&props.Props{Config: nil})
	if result != os.TempDir() {
		t.Errorf("expected %q, got %q", os.TempDir(), result)
	}
}
