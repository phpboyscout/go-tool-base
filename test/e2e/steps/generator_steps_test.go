package steps_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cucumber/godog"

	"gitlab.com/phpboyscout/go-tool-base/test/e2e/support"
)

type generatorWorldKey struct{}

type generatorWorld struct {
	binaryPath string
	projectDir string
	stdout     string
	stderr     string
	exitCode   int
}

func getGeneratorWorld(ctx context.Context) *generatorWorld {
	return ctx.Value(generatorWorldKey{}).(*generatorWorld)
}

func initGeneratorSteps(ctx *godog.ScenarioContext) {
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, generatorWorldKey{}, &generatorWorld{}), nil
	})

	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w := getGeneratorWorld(ctx)
		if w.projectDir != "" {
			_ = os.RemoveAll(w.projectDir)
		}

		return ctx, nil
	})

	ctx.Step(`^a gtb project with a "([^"]*)" command that has aliases, a required shorthand flag, and a pre-run hook$`,
		aGTBProjectWithACommandWithMetadata)
	ctx.Step(`^a freshly generated gtb project$`, aFreshlyGeneratedGTBProject)
	ctx.Step(`^I run gtb in the project with "([^"]*)"$`, iRunGTBInTheProjectWith)
	ctx.Step(`^the project exit code is (\d+)$`, theProjectExitCodeIs)
	ctx.Step(`^the project exit code is not zero$`, theProjectExitCodeIsNotZero)
	ctx.Step(`^the generated "([^"]*)" file contains "([^"]*)"$`, theGeneratedFileContains)
	ctx.Step(`^the generated "([^"]*)" file does not contain "([^"]*)"$`, theGeneratedFileDoesNotContain)
	ctx.Step(`^the generated "([^"]*)" file exists$`, theGeneratedFileExists)
	ctx.Step(`^the generated "([^"]*)" file does not exist$`, theGeneratedFileDoesNotExist)
	ctx.Step(`^the project output contains "([^"]*)"$`, theProjectOutputContains)
	ctx.Step(`^the project manifest contains "([^"]*)"$`, theProjectManifestContains)
	ctx.Step(`^the project manifest does not contain "([^"]*)"$`, theProjectManifestDoesNotContain)
	ctx.Step(`^a local template overlay directory "([^"]*)" providing a "([^"]*)" file$`, aLocalTemplateOverlayDirectory)
}

// aGTBProjectWithACommandWithMetadata scaffolds a minimal-but-valid gtb
// project on disk whose `.gtb/manifest.yaml` describes a `deploy` command
// carrying aliases, a required shorthand flag, and a persistent pre-run hook.
// A deliberately stale cmd.go hash lets the scenario assert the hash is
// rewritten after add-flag regeneration.
func aGTBProjectWithACommandWithMetadata(ctx context.Context, command string) (context.Context, error) {
	w := getGeneratorWorld(ctx)

	dir, err := os.MkdirTemp("", "gtb-e2e-gen-*")
	if err != nil {
		return ctx, fmt.Errorf("create project dir: %w", err)
	}

	w.projectDir = dir

	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/test-project\n\ngo 1.24\n"), 0o644); err != nil {
		return ctx, fmt.Errorf("write go.mod: %w", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "pkg", "cmd", "root"), 0o755); err != nil {
		return ctx, fmt.Errorf("mkdir root: %w", err)
	}

	rootCmd := `package root

import (
	"github.com/spf13/cobra"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func NewCmdRoot(props *props.Props) *cobra.Command {
	return &cobra.Command{Use: "test-project"}
}
`
	if err := os.WriteFile(filepath.Join(dir, "pkg", "cmd", "root", "cmd.go"), []byte(rootCmd), 0o644); err != nil {
		return ctx, fmt.Errorf("write root cmd.go: %w", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, ".gtb"), 0o755); err != nil {
		return ctx, fmt.Errorf("mkdir .gtb: %w", err)
	}

	manifest := fmt.Sprintf(`properties:
  name: test-project
github:
  org: test-org
  repo: test-project
version:
  gtb: v0.0.0
commands:
- name: %s
  description: Deploy command
  aliases:
  - dep
  - ship
  persistent_pre_run: true
  hashes:
    cmd.go: stalehash
  flags:
  - name: target
    type: string
    description: Deployment target
    shorthand: t
    required: true
`, command)

	if err := os.WriteFile(filepath.Join(dir, ".gtb", "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		return ctx, fmt.Errorf("write manifest: %w", err)
	}

	// Generator scenarios drive the real gtb binary (./cmd/gtb), which disables
	// InitCmd and therefore runs generate/add-flag without an external config
	// file — unlike the InitCmd-enabled e2e framework binary.
	path, err := support.GeneratorBinaryPath()
	if err != nil {
		return ctx, fmt.Errorf("build gtb binary: %w", err)
	}

	w.binaryPath = path

	return ctx, nil
}

// aFreshlyGeneratedGTBProject scaffolds a real project with the gtb generator
// (the same binary the enable/disable verbs run against), so the manifest and
// generated root command are exactly what a user starts from.
func aFreshlyGeneratedGTBProject(ctx context.Context) (context.Context, error) {
	w := getGeneratorWorld(ctx)

	path, err := support.GeneratorBinaryPath()
	if err != nil {
		return ctx, fmt.Errorf("build gtb binary: %w", err)
	}

	w.binaryPath = path

	dir, err := os.MkdirTemp("", "gtb-e2e-feat-*")
	if err != nil {
		return ctx, fmt.Errorf("create project dir: %w", err)
	}

	w.projectDir = dir

	cmd := exec.CommandContext(ctx, path,
		"generate", "project",
		"--name", "feattool",
		"--repo", "acme/feattool",
		"--path", dir,
		"--no-git",
		"--ci",
	) //nolint:gosec // test-only: fixed args
	cmd.Dir = dir

	var stdout, stderr strings.Builder

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return ctx, fmt.Errorf("generate project: %w\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	return ctx, nil
}

func iRunGTBInTheProjectWith(ctx context.Context, args string) context.Context {
	w := getGeneratorWorld(ctx)

	parts := strings.Fields(args)
	parts = append(parts, "--ci")

	cmd := exec.CommandContext(ctx, w.binaryPath, parts...) //nolint:gosec // test-only: args from Gherkin steps
	cmd.Dir = w.projectDir

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	w.stdout = stdout.String()
	w.stderr = stderr.String()

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			w.exitCode = exitErr.ExitCode()
		} else {
			w.exitCode = -1
		}
	} else {
		w.exitCode = 0
	}

	return ctx
}

func theProjectExitCodeIs(ctx context.Context, expected int) error {
	w := getGeneratorWorld(ctx)
	if w.exitCode != expected {
		return fmt.Errorf("expected exit code %d, got %d\nstdout: %s\nstderr: %s",
			expected, w.exitCode, w.stdout, w.stderr)
	}

	return nil
}

func theProjectExitCodeIsNotZero(ctx context.Context) error {
	w := getGeneratorWorld(ctx)
	if w.exitCode == 0 {
		return fmt.Errorf("expected non-zero exit code, got 0\nstdout: %s\nstderr: %s", w.stdout, w.stderr)
	}

	return nil
}

func theGeneratedFileContains(ctx context.Context, relPath, substr string) error {
	w := getGeneratorWorld(ctx)

	content, err := os.ReadFile(filepath.Join(w.projectDir, relPath))
	if err != nil {
		return fmt.Errorf("read %s: %w", relPath, err)
	}

	if !strings.Contains(string(content), substr) {
		return fmt.Errorf("generated %q does not contain %q\ncontent:\n%s", relPath, substr, content)
	}

	return nil
}

func theGeneratedFileDoesNotContain(ctx context.Context, relPath, substr string) error {
	w := getGeneratorWorld(ctx)

	content, err := os.ReadFile(filepath.Join(w.projectDir, relPath))
	if err != nil {
		return fmt.Errorf("read %s: %w", relPath, err)
	}

	if strings.Contains(string(content), substr) {
		return fmt.Errorf("generated %q unexpectedly contains %q\ncontent:\n%s", relPath, substr, content)
	}

	return nil
}

func theGeneratedFileExists(ctx context.Context, relPath string) error {
	w := getGeneratorWorld(ctx)

	if _, err := os.Stat(filepath.Join(w.projectDir, relPath)); err != nil {
		return fmt.Errorf("expected generated %q to exist: %w", relPath, err)
	}

	return nil
}

func theGeneratedFileDoesNotExist(ctx context.Context, relPath string) error {
	w := getGeneratorWorld(ctx)

	_, err := os.Stat(filepath.Join(w.projectDir, relPath))
	if err == nil {
		return fmt.Errorf("expected generated %q not to exist, but it does", relPath)
	}

	if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", relPath, err)
	}

	return nil
}

func theProjectOutputContains(ctx context.Context, substr string) error {
	w := getGeneratorWorld(ctx)

	if !strings.Contains(w.stdout, substr) && !strings.Contains(w.stderr, substr) {
		return fmt.Errorf("expected project output to contain %q\nstdout: %s\nstderr: %s", substr, w.stdout, w.stderr)
	}

	return nil
}

// aLocalTemplateOverlayDirectory writes a bare local template-overlay folder
// (no gtb-template.yaml needed) inside the project at the given relative dir,
// containing a single templated file. The project can then add it as a
// local-folder source via `template add ./<dir>`.
func aLocalTemplateOverlayDirectory(ctx context.Context, dir, file string) error {
	w := getGeneratorWorld(ctx)

	overlayDir := filepath.Join(w.projectDir, dir)
	if err := os.MkdirAll(overlayDir, 0o755); err != nil {
		return fmt.Errorf("mkdir overlay %s: %w", dir, err)
	}

	if err := os.WriteFile(filepath.Join(overlayDir, file), []byte("overlay for {{ .Name }}\n"), 0o644); err != nil {
		return fmt.Errorf("write overlay file %s: %w", file, err)
	}

	return nil
}

func theProjectManifestContains(ctx context.Context, substr string) error {
	return manifestContains(ctx, substr, true)
}

func theProjectManifestDoesNotContain(ctx context.Context, substr string) error {
	return manifestContains(ctx, substr, false)
}

func manifestContains(ctx context.Context, substr string, want bool) error {
	w := getGeneratorWorld(ctx)

	content, err := os.ReadFile(filepath.Join(w.projectDir, ".gtb", "manifest.yaml"))
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	got := strings.Contains(string(content), substr)
	if got != want {
		return fmt.Errorf("manifest contains(%q)=%v, want %v\nmanifest:\n%s", substr, got, want, content)
	}

	return nil
}
