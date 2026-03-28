package support

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

var (
	binaryPath string
	buildOnce  sync.Once
	buildErr   error
)

// BinaryPath returns the path to the compiled gtb binary,
// building it on first call. The binary is placed in a temp directory
// and reused for all subsequent calls.
func BinaryPath() (string, error) {
	buildOnce.Do(func() {
		tmpDir, err := os.MkdirTemp("", "gtb-e2e-*")
		if err != nil {
			buildErr = fmt.Errorf("failed to create temp dir: %w", err)

			return
		}

		binaryPath = filepath.Join(tmpDir, "gtb")
		cmd := exec.CommandContext(context.Background(), "go", "build", "-o", binaryPath, "./cmd/gtb")
		cmd.Dir = projectRoot()
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			buildErr = fmt.Errorf("failed to build gtb binary: %w", err)

			return
		}
	})

	return binaryPath, buildErr
}

// CleanupBinary removes the temporary binary directory.
func CleanupBinary() {
	if binaryPath != "" {
		_ = os.RemoveAll(filepath.Dir(binaryPath))
	}
}

// projectRoot walks up from the current working directory to find go.mod.
func projectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}

		dir = parent
	}
}
