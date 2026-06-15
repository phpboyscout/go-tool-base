package telemetry

import (
	"os"
	"path/filepath"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// ResolveDataDir determines the directory for telemetry data files (spill files,
// local-only logs). Uses the config directory if it exists and is writable,
// otherwise falls back to os.TempDir().
func ResolveDataDir(p props.ConfigProvider) string {
	if dir, ok := configDataDir(p); ok {
		return dir
	}

	return os.TempDir()
}

func configDataDir(p props.ConfigProvider) (string, bool) {
	cfg := p.GetConfig()
	if cfg == nil {
		return "", false
	}

	cfgFile := cfg.GetViper().ConfigFileUsed()
	if cfgFile == "" {
		return "", false
	}

	dir := filepath.Dir(cfgFile)

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", false
	}

	testFile := filepath.Join(dir, ".telemetry-write-test")

	f, err := os.Create(testFile)
	if err != nil {
		return "", false
	}

	_ = f.Close()
	_ = os.Remove(testFile)

	return dir, true
}
