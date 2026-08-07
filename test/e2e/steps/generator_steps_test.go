package steps_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cucumber/godog"

	"gitlab.com/phpboyscout/go-tool-base/test/e2e/support"
)

type generatorWorldKey struct{}

type generatorWorld struct {
	binaryPath string
	projectDir string
	// configDir isolates HOME for every gtb invocation in the scenario.
	configDir string
	stdout    string
	stderr    string
	exitCode  int
}

// isolatedEnv returns the environment for a gtb invocation: the real
// environment with HOME redirected at the scenario's own directory.
//
// Without this the generator steps resolve the *developer's* gtb config, and
// that silently changes which code path runs. `GenerateDocs` branches on
// whether an AI chat provider is configured: with none (CI) it writes
// boilerplate through writeDocFile and updates the commands index; with one
// configured but unreachable (a dev machine) the AI call fails and the legacy
// fallback runs instead, writing elsewhere and never touching the index. A
// defect on the first path is then invisible locally and red in CI — which is
// exactly how the commands-index hash bug reached a green `just ci` and failed
// the pipeline (!371). The cli and signal steps have always isolated HOME; the
// generator steps had not.
//
// Only gtb's *configuration* is isolated. Every build cache is pinned back to
// the real one, because they all live under HOME and letting them move makes
// each scenario pay for a cold cache:
//
//   - GOMODCACHE — every scaffolded project re-downloads its dependencies;
//   - GOCACHE — every compile starts from nothing;
//   - GOLANGCI_LINT_CACHE — the generator runs `golangci-lint run --fix` as
//     post-processing, and a cold lint cache costs ~11s *per scenario*. This one
//     is easy to miss: the Go caches are obvious, and lint is invisible until
//     the suite quietly takes four times as long.
//
// Measured: isolating HOME without pinning these took the generator suite from
// 193s to 809s, and the cost landed on every scenario that regenerates and none
// of the pure-CLI ones.
func (w *generatorWorld) isolatedEnv() []string {
	env := append(os.Environ(), "HOME="+w.configDir)

	for name, value := range sharedCaches() {
		env = append(env, name+"="+value)
	}

	return env
}

// sharedCaches resolves the cache locations once per test binary; resolving them
// per invocation would spawn `go env` for every gtb command the suite runs.
var sharedCaches = sync.OnceValue(func() map[string]string {
	resolved := map[string]string{}

	for _, name := range []string{"GOMODCACHE", "GOCACHE"} {
		out, err := exec.Command("go", "env", name).Output() //nolint:gosec // test-only: name is a package constant
		if err != nil {
			continue
		}

		if v := strings.TrimSpace(string(out)); v != "" {
			resolved[name] = v
		}
	}

	// golangci-lint has no `go env` equivalent: honour an explicit override,
	// otherwise its documented default under the real HOME.
	if v := os.Getenv("GOLANGCI_LINT_CACHE"); v != "" {
		resolved["GOLANGCI_LINT_CACHE"] = v
	} else if home, err := os.UserHomeDir(); err == nil {
		resolved["GOLANGCI_LINT_CACHE"] = filepath.Join(home, ".cache", "golangci-lint")
	}

	return resolved
})

func getGeneratorWorld(ctx context.Context) *generatorWorld {
	return ctx.Value(generatorWorldKey{}).(*generatorWorld)
}

func initGeneratorSteps(ctx *godog.ScenarioContext) {
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		configDir, err := os.MkdirTemp("", "gtb-e2e-home-*")
		if err != nil {
			return ctx, fmt.Errorf("create isolated HOME: %w", err)
		}

		return context.WithValue(ctx, generatorWorldKey{}, &generatorWorld{configDir: configDir}), nil
	})

	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w := getGeneratorWorld(ctx)
		if w.projectDir != "" {
			_ = os.RemoveAll(w.projectDir)
		}

		if w.configDir != "" {
			_ = os.RemoveAll(w.configDir)
		}

		return ctx, nil
	})

	ctx.Step(`^a gtb project with a "([^"]*)" command that has aliases, a required shorthand flag, and a pre-run hook$`,
		aGTBProjectWithACommandWithMetadata)
	ctx.Step(`^a freshly generated gtb project$`, aFreshlyGeneratedGTBProject)
	ctx.Step(`^I generate a gtb project with features "([^"]*)"$`, iGenerateAGTBProjectWithFeatures)
	ctx.Step(`^a gtb project with a "([^"]*)" command$`, aGTBProjectWithACommand)
	ctx.Step(`^I run gtb in the project with "([^"]*)"$`, iRunGTBInTheProjectWith)
	ctx.Step(`^the project exit code is (\d+)$`, theProjectExitCodeIs)
	ctx.Step(`^the project exit code is not zero$`, theProjectExitCodeIsNotZero)
	ctx.Step(`^the generated "([^"]*)" file contains "([^"]*)"$`, theGeneratedFileContains)
	ctx.Step(`^the generated "([^"]*)" file does not contain "([^"]*)"$`, theGeneratedFileDoesNotContain)
	ctx.Step(`^the generated "([^"]*)" file exists$`, theGeneratedFileExists)
	ctx.Step(`^the generated "([^"]*)" file does not exist$`, theGeneratedFileDoesNotExist)
	ctx.Step(`^the project output contains "([^"]*)"$`, theProjectOutputContains)
	ctx.Step(`^the project output does not contain "([^"]*)"$`, theProjectOutputDoesNotContain)
	ctx.Step(`^the project manifest contains "([^"]*)"$`, theProjectManifestContains)
	ctx.Step(`^the project manifest does not contain "([^"]*)"$`, theProjectManifestDoesNotContain)
	ctx.Step(`^a local template overlay directory "([^"]*)" providing a "([^"]*)" file$`, aLocalTemplateOverlayDirectory)
	ctx.Step(`^I hand-edit the generated "([^"]*)" file$`, iHandEditTheGeneratedFile)
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
	if err := scaffoldProject(ctx, "gtb-e2e-feat-*"); err != nil {
		return ctx, err
	}

	w := getGeneratorWorld(ctx)
	if w.exitCode != 0 {
		return ctx, fmt.Errorf("generate project exited %d\nstdout: %s\nstderr: %s", w.exitCode, w.stdout, w.stderr)
	}

	return ctx, nil
}

// iGenerateAGTBProjectWithFeatures scaffolds into a fresh directory with an
// explicit --features set, leaving the exit code for the scenario to assert.
// Unlike aFreshlyGeneratedGTBProject this is a When, not a Given: the generate
// invocation is the behaviour under test, so a rejected feature set has to reach
// the exit-code assertion instead of aborting the scenario.
func iGenerateAGTBProjectWithFeatures(ctx context.Context, features string) (context.Context, error) {
	return ctx, scaffoldProject(ctx, "gtb-e2e-featsel-*", "--features", features)
}

// scaffoldProject runs `generate project` into a fresh temp directory and
// records the binary path, project dir, output and exit code on the world. It
// returns an error only for a harness failure (binary build, temp dir) — a
// non-zero generate exit is recorded, not raised, so a caller can either assert
// on it or treat it as fatal.
func scaffoldProject(ctx context.Context, dirPattern string, extraArgs ...string) error {
	w := getGeneratorWorld(ctx)

	path, err := support.GeneratorBinaryPath()
	if err != nil {
		return fmt.Errorf("build gtb binary: %w", err)
	}

	w.binaryPath = path

	dir, err := os.MkdirTemp("", dirPattern)
	if err != nil {
		return fmt.Errorf("create project dir: %w", err)
	}

	w.projectDir = dir

	args := append([]string{
		"generate", "project",
		"--name", "feattool",
		"--repo", "acme/feattool",
		"--path", dir,
		"--no-git",
		"--ci",
	}, extraArgs...)

	cmd := exec.CommandContext(ctx, path, args...) //nolint:gosec // test-only: args from Gherkin steps
	cmd.Dir = dir
	cmd.Env = w.isolatedEnv()

	recordRun(w, cmd)

	return nil
}

// recordRun executes cmd and folds its result onto the world: stdout, stderr and
// an exit code (-1 for a failure that carries none, e.g. the binary not running
// at all).
func recordRun(w *generatorWorld, cmd *exec.Cmd) {
	var stdout, stderr strings.Builder

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	w.stdout = stdout.String()
	w.stderr = stderr.String()

	var exitErr *exec.ExitError

	switch {
	case err == nil:
		w.exitCode = 0
	case errors.As(err, &exitErr):
		w.exitCode = exitErr.ExitCode()
	default:
		w.exitCode = -1
	}
}

// aGTBProjectWithACommand scaffolds a minimal-but-valid gtb project whose
// manifest describes a single, plain command (no aliases/flags/hooks). It is
// the lightweight fixture for MCP-exposure scenarios, which only need a command
// to gate and re-render.
func aGTBProjectWithACommand(ctx context.Context, command string) (context.Context, error) {
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
  description: Publish command
`, command)

	if err := os.WriteFile(filepath.Join(dir, ".gtb", "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		return ctx, fmt.Errorf("write manifest: %w", err)
	}

	path, err := support.GeneratorBinaryPath()
	if err != nil {
		return ctx, fmt.Errorf("build gtb binary: %w", err)
	}

	w.binaryPath = path

	return ctx, nil
}

// iHandEditTheGeneratedFile appends a marker comment to a generated file so its
// on-disk content no longer matches the hash the manifest recorded — the drift
// that used to make `regenerate project` unable to finish (issue #13).
func iHandEditTheGeneratedFile(ctx context.Context, relPath string) error {
	w := getGeneratorWorld(ctx)

	full := filepath.Join(w.projectDir, relPath)

	content, err := os.ReadFile(full) //nolint:gosec // test-only: path from a Gherkin step
	if err != nil {
		return fmt.Errorf("read %s: %w", relPath, err)
	}

	//nolint:gosec // test-only: full is derived from the scenario's own temp project dir
	return os.WriteFile(full, append(content, []byte("\n// hand-edited, do not clobber\n")...), 0o644)
}

func iRunGTBInTheProjectWith(ctx context.Context, args string) context.Context {
	w := getGeneratorWorld(ctx)

	parts := strings.Fields(args)
	parts = append(parts, "--ci")

	cmd := exec.CommandContext(ctx, w.binaryPath, parts...) //nolint:gosec // test-only: args from Gherkin steps
	cmd.Dir = w.projectDir
	cmd.Env = w.isolatedEnv()

	recordRun(w, cmd)

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

func theProjectOutputDoesNotContain(ctx context.Context, substr string) error {
	w := getGeneratorWorld(ctx)

	if strings.Contains(w.stdout, substr) || strings.Contains(w.stderr, substr) {
		return fmt.Errorf("expected project output not to contain %q\nstdout: %s\nstderr: %s", substr, w.stdout, w.stderr)
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
