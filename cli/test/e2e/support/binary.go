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

	generatorBinaryPath string
	generatorBuildOnce  sync.Once
	generatorBuildErr   error
)

// BinaryPath returns the path to the compiled e2e test binary (./cmd/e2e),
// building it on first call. The binary is placed in a temp directory
// and reused for all subsequent calls.
//
// The e2e binary enables every feature flag (including InitCmd) so that
// framework scenarios — init, doctor, config, chat, controls — can be
// exercised. CLI scenarios that drive it must therefore provide a config
// file, because an InitCmd-enabled tool requires one at bootstrap.
func BinaryPath() (string, error) {
	buildOnce.Do(func() {
		binaryPath, buildErr = buildBinary("gtb-e2e", "./cmd/e2e")
	})

	return binaryPath, buildErr
}

// GeneratorBinaryPath returns the path to the compiled real gtb binary
// (./cmd/gtb), building it on first call.
//
// Generator scenarios must use this binary, not the e2e binary: the real
// gtb tool disables InitCmd (internal/cmd/root), so its bootstrap treats a
// missing config as allow-empty and the generate/regenerate/remove commands
// run without an external config file — exactly as they do in production.
// Driving them through the InitCmd-enabled e2e binary would instead demand a
// config file and misrepresent how the generator actually runs.
func GeneratorBinaryPath() (string, error) {
	generatorBuildOnce.Do(func() {
		generatorBinaryPath, generatorBuildErr = buildBinary("gtb", "./cmd/gtb")
	})

	return generatorBinaryPath, generatorBuildErr
}

// buildBinary compiles target into a fresh temp directory and returns the
// resulting executable path.
func buildBinary(name, target string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "gtb-e2e-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	out := filepath.Join(tmpDir, name)
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", out, target)
	cmd.Dir = projectRoot()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to build %s: %w", target, err)
	}

	return out, nil
}

// CleanupBinary removes the temporary binary directories.
func CleanupBinary() {
	if binaryPath != "" {
		_ = os.RemoveAll(filepath.Dir(binaryPath))
	}

	if generatorBinaryPath != "" {
		_ = os.RemoveAll(filepath.Dir(generatorBinaryPath))
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
