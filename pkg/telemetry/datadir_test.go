package telemetry_test

import (
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry"
)

func TestResolveDataDir_ConfigDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("key: value\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	result := telemetry.ResolveDataDir(cfgFile)
	if result != dir {
		t.Errorf("expected %q, got %q", dir, result)
	}
}

func TestResolveDataDir_Fallback(t *testing.T) {
	t.Parallel()

	result := telemetry.ResolveDataDir("")
	if result != os.TempDir() {
		t.Errorf("expected %q, got %q", os.TempDir(), result)
	}
}

func TestResolveDataDir_MissingConfigDir(t *testing.T) {
	t.Parallel()

	result := telemetry.ResolveDataDir(filepath.Join(t.TempDir(), "missing", "config.yaml"))
	if result != os.TempDir() {
		t.Errorf("expected %q, got %q", os.TempDir(), result)
	}
}
