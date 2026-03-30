package telemetry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"

	mockcfg "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/telemetry"
)

func TestResolveDataDir_ConfigDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("key: value\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	v := viper.New()
	v.SetConfigFile(cfgFile)

	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("read config: %v", err)
	}

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().GetViper().Return(v)

	p := &props.Props{Config: mock}

	result := telemetry.ResolveDataDir(p)
	if result != dir {
		t.Errorf("expected %q, got %q", dir, result)
	}
}

func TestResolveDataDir_Fallback(t *testing.T) {
	t.Parallel()

	p := &props.Props{}

	result := telemetry.ResolveDataDir(p)
	if result != os.TempDir() {
		t.Errorf("expected %q, got %q", os.TempDir(), result)
	}
}

func TestResolveDataDir_NilConfig(t *testing.T) {
	t.Parallel()

	p := &props.Props{Config: nil}

	result := telemetry.ResolveDataDir(p)
	if result != os.TempDir() {
		t.Errorf("expected %q, got %q", os.TempDir(), result)
	}
}
