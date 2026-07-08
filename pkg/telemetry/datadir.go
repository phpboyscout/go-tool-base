package telemetry

import (
	"os"
	"path/filepath"
)

// ResolveDataDir determines the directory for telemetry data files (spill files,
// local-only logs). Uses configFile's directory if it exists and is writable,
// otherwise falls back to [os.TempDir].
func ResolveDataDir(configFile string) string {
	if dir, ok := configDataDir(configFile); ok {
		return dir
	}

	return os.TempDir()
}

func configDataDir(configFile string) (string, bool) {
	if configFile == "" {
		return "", false
	}

	dir := filepath.Dir(configFile)

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
