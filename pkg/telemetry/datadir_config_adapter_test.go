package telemetry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"

	mockcfg "gitlab.com/phpboyscout/go/config/mocks"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestResolveDataDirFromProps_ConfigDir(t *testing.T) {
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

	result := ResolveDataDirFromProps(&props.Props{Config: mock})
	if result != dir {
		t.Errorf("expected %q, got %q", dir, result)
	}
}

func TestResolveDataDirFromProps_NilConfig(t *testing.T) {
	t.Parallel()

	result := ResolveDataDirFromProps(&props.Props{Config: nil})
	if result != os.TempDir() {
		t.Errorf("expected %q, got %q", os.TempDir(), result)
	}
}
