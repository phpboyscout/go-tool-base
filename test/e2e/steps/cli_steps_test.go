package steps_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cucumber/godog"

	"gitlab.com/phpboyscout/go-tool-base/test/e2e/support"
)

type cliWorldKey struct{}

type cliWorld struct {
	binaryPath string
	configDir  string
	initDir    string
	stdout     string
	stderr     string
	exitCode   int
	envVars    map[string]string
	keysDir    string // scratch dir for `gtb keys *` outputs; {keys_dir} placeholder
}

func getCLIWorld(ctx context.Context) *cliWorld {
	return ctx.Value(cliWorldKey{}).(*cliWorld)
}

func initCLISteps(ctx *godog.ScenarioContext) {
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		tmpDir, err := os.MkdirTemp("", "gtb-e2e-config-*")
		if err != nil {
			return ctx, fmt.Errorf("failed to create temp config dir: %w", err)
		}

		cfgPath := filepath.Join(tmpDir, "config.yaml")
		if err := os.WriteFile(cfgPath, []byte("log:\n  level: info\n"), 0o644); err != nil {
			return ctx, fmt.Errorf("failed to write temp config: %w", err)
		}

		w := &cliWorld{configDir: tmpDir}

		return context.WithValue(ctx, cliWorldKey{}, w), nil
	})

	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w := getCLIWorld(ctx)
		if w.configDir != "" {
			_ = os.RemoveAll(w.configDir)
		}

		if w.initDir != "" {
			_ = os.RemoveAll(w.initDir)
		}

		if w.keysDir != "" {
			_ = os.RemoveAll(w.keysDir)
		}

		return ctx, nil
	})

	// --- Given ---
	ctx.Step(`^the gtb binary is built$`, theGTBBinaryIsBuilt)
	ctx.Step(`^the gtb generator binary is built$`, theGTBGeneratorBinaryIsBuilt)
	ctx.Step(`^a temporary init directory$`, aTemporaryInitDirectory)
	ctx.Step(`^the init directory contains a config file:$`, theInitDirContainsConfigFile)
	ctx.Step(`^an empty config directory$`, anEmptyConfigDirectory)
	ctx.Step(`^a temporary directory with a config file:$`, aTemporaryDirectoryWithConfigFile)
	ctx.Step(`^the config file contains:$`, theConfigFileContains)
	ctx.Step(`^a config file with no log\.level key$`, aConfigFileWithNoLogLevelKey)
	ctx.Step(`^a config file exists with:$`, aTemporaryDirectoryWithConfigFile)
	ctx.Step(`^a temporary keys directory$`, aTemporaryKeysDirectory)
	ctx.Step(`^an RSA keypair has been generated as "([^"]*)"$`, anRSAKeypairHasBeenGeneratedAs)

	// --- When ---
	ctx.Step(`^I set environment variable "([^"]*)" to "([^"]*)"$`, iSetEnvironmentVariable)
	ctx.Step(`^I run gtb with "([^"]*)"$`, iRunGTBWith)

	// --- Then ---
	ctx.Step(`^the exit code is (\d+)$`, theExitCodeIs)
	ctx.Step(`^the exit code is not (\d+)$`, theExitCodeIsNot)
	ctx.Step(`^stdout contains "([^"]*)"$`, stdoutContains)
	ctx.Step(`^stdout equals "([^"]*)"$`, stdoutEquals)
	ctx.Step(`^stdout does not contain "([^"]*)"$`, stdoutDoesNotContain)
	ctx.Step(`^stderr contains "([^"]*)"$`, stderrContains)
	ctx.Step(`^stderr does not contain "([^"]*)"$`, stderrDoesNotContain)
	ctx.Step(`^stdout is valid JSON$`, stdoutIsValidJSON)
	ctx.Step(`^the JSON field "([^"]*)" equals "([^"]*)"$`, theJSONFieldEquals)
	ctx.Step(`^the JSON field "([^"]*)" is not empty$`, theJSONFieldIsNotEmpty)
	ctx.Step(`^the JSON field "([^"]*)" is an array with at least (\d+) items$`, theJSONFieldIsArrayWithAtLeast)
	ctx.Step(`^the file "([^"]*)" exists in the init directory$`, theFileExistsInInitDir)
	ctx.Step(`^the config file in the init directory contains "([^"]*)"$`, theInitConfigContains)
	ctx.Step(`^the config file in the init directory does not contain "([^"]*)"$`, theInitConfigDoesNotContain)
	ctx.Step(`^the config file contains "([^"]*)"$`, theScenarioConfigContains)
	ctx.Step(`^the config file does not contain "([^"]*)"$`, theScenarioConfigDoesNotContain)
	ctx.Step(`^the file "([^"]*)" exists in the keys directory$`, theFileExistsInKeysDir)
	ctx.Step(`^the file "([^"]*)" in the keys directory contains "([^"]*)"$`, theFileInKeysDirContains)
}

// --- Given implementations ---

func theGTBBinaryIsBuilt(ctx context.Context) (context.Context, error) {
	w := getCLIWorld(ctx)
	path, err := support.BinaryPath()
	if err != nil {
		return ctx, fmt.Errorf("failed to build gtb binary: %w", err)
	}
	w.binaryPath = path
	return ctx, nil
}

// theGTBGeneratorBinaryIsBuilt selects the real gtb binary (./cmd/gtb) for the
// CLI world, rather than the feature-flag-driven e2e binary (./cmd/e2e). The
// generate/regenerate/remove/template commands are GTB-specific commands
// registered only on the real binary (internal/cmd/root), so scenarios that
// exercise the generator CLI surface must drive ./cmd/gtb. The real binary
// disables InitCmd, so it bootstraps without requiring an external config file.
func theGTBGeneratorBinaryIsBuilt(ctx context.Context) (context.Context, error) {
	w := getCLIWorld(ctx)
	path, err := support.GeneratorBinaryPath()
	if err != nil {
		return ctx, fmt.Errorf("failed to build gtb generator binary: %w", err)
	}
	w.binaryPath = path
	return ctx, nil
}

func anEmptyConfigDirectory(ctx context.Context) (context.Context, error) {
	w := getCLIWorld(ctx)

	// Remove the default config file so the directory is truly empty
	cfgPath := filepath.Join(w.configDir, "config.yaml")
	if err := os.Remove(cfgPath); err != nil && !os.IsNotExist(err) {
		return ctx, fmt.Errorf("failed to remove default config: %w", err)
	}

	return ctx, nil
}

// aTemporaryDirectoryWithConfigFile writes the docstring as the scenario's
// config.yaml, overwriting the default baseline from the Before hook.
func aTemporaryDirectoryWithConfigFile(ctx context.Context, content *godog.DocString) (context.Context, error) {
	w := getCLIWorld(ctx)

	cfgPath := filepath.Join(w.configDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(content.Content), 0o644); err != nil {
		return ctx, fmt.Errorf("failed to write config file: %w", err)
	}

	return ctx, nil
}

// theConfigFileContains rewrites the scenario's config.yaml with new content.
func theConfigFileContains(ctx context.Context, content *godog.DocString) (context.Context, error) {
	return aTemporaryDirectoryWithConfigFile(ctx, content)
}

// aConfigFileWithNoLogLevelKey writes a config file that omits the required
// log.level key, used to exercise validation failure scenarios.
func aConfigFileWithNoLogLevelKey(ctx context.Context) (context.Context, error) {
	w := getCLIWorld(ctx)

	cfgPath := filepath.Join(w.configDir, "config.yaml")
	// Minimal valid YAML that does not define log.level
	if err := os.WriteFile(cfgPath, []byte("other:\n  key: value\n"), 0o644); err != nil {
		return ctx, fmt.Errorf("failed to write config file: %w", err)
	}

	return ctx, nil
}

func iSetEnvironmentVariable(ctx context.Context, key, value string) (context.Context, error) {
	w := getCLIWorld(ctx)

	if w.envVars == nil {
		w.envVars = make(map[string]string)
	}

	w.envVars[key] = value

	return ctx, nil
}

// --- When implementations ---

func iRunGTBWith(ctx context.Context, args string) context.Context {
	w := getCLIWorld(ctx)

	// Substitute {init_dir} placeholder with the actual temp init directory
	if w.initDir != "" {
		args = strings.ReplaceAll(args, "{init_dir}", w.initDir)
	}

	// Substitute {keys_dir} placeholder with the keys scratch directory.
	// Used by features/cli/keys.feature for outputs of `gtb keys generate`
	// / `keys mint` / `keys wkd`.
	if w.keysDir != "" {
		args = strings.ReplaceAll(args, "{keys_dir}", w.keysDir)
	}

	parts := strings.Fields(args)
	// Always pass --ci to skip update checks and interactive prompts
	parts = append(parts, "--ci")
	// Point to the per-scenario temp config so the binary doesn't require a real install
	parts = append(parts, "--config", filepath.Join(w.configDir, "config.yaml"))
	cmd := exec.CommandContext(ctx, w.binaryPath, parts...) //nolint:gosec // test-only: args from Gherkin steps

	// Give the child a piped (non-TTY) stdin so utils.IsInteractive() reports
	// false deterministically. Without this the child inherits /dev/null, which
	// is a character device and reads as "interactive", causing commands with a
	// prompt fallback (e.g. `update`'s multi-install target selection) to open
	// /dev/tty and fail — flakily, depending on the host's installed binaries.
	cmd.Stdin = strings.NewReader("")

	// Always isolate HOME to the scenario's temp directory so commands like
	// `telemetry enable/disable` that persist to ~/.<tool>/ don't leak across
	// scenarios or into the developer's real config.
	cmd.Env = append(os.Environ(), "HOME="+w.configDir)

	if len(w.envVars) > 0 {
		for k, v := range w.envVars {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

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

// --- Then implementations ---

func theExitCodeIs(ctx context.Context, expected int) error {
	w := getCLIWorld(ctx)
	if w.exitCode != expected {
		return fmt.Errorf("expected exit code %d, got %d\nstdout: %s\nstderr: %s", expected, w.exitCode, w.stdout, w.stderr)
	}
	return nil
}

func theExitCodeIsNot(ctx context.Context, unexpected int) error {
	w := getCLIWorld(ctx)
	if w.exitCode == unexpected {
		return fmt.Errorf("expected exit code to not be %d\nstdout: %s\nstderr: %s", unexpected, w.stdout, w.stderr)
	}
	return nil
}

func stdoutContains(ctx context.Context, substr string) error {
	w := getCLIWorld(ctx)
	if !strings.Contains(w.stdout, substr) {
		return fmt.Errorf("stdout does not contain %q\nstdout:\n%s", substr, w.stdout)
	}
	return nil
}

func stdoutEquals(ctx context.Context, want string) error {
	w := getCLIWorld(ctx)
	if got := strings.TrimSpace(w.stdout); got != want {
		return fmt.Errorf("expected stdout to equal %q, got %q", want, got)
	}
	return nil
}

func stdoutDoesNotContain(ctx context.Context, substr string) error {
	w := getCLIWorld(ctx)
	if strings.Contains(w.stdout, substr) {
		return fmt.Errorf("stdout should not contain %q\nstdout:\n%s", substr, w.stdout)
	}

	return nil
}

func stderrContains(ctx context.Context, substr string) error {
	w := getCLIWorld(ctx)
	if !strings.Contains(w.stderr, substr) {
		return fmt.Errorf("stderr does not contain %q\nstderr:\n%s", substr, w.stderr)
	}

	return nil
}

func stderrDoesNotContain(ctx context.Context, substr string) error {
	w := getCLIWorld(ctx)
	if strings.Contains(w.stderr, substr) {
		return fmt.Errorf("stderr should not contain %q\nstderr:\n%s", substr, w.stderr)
	}

	return nil
}

func stdoutIsValidJSON(ctx context.Context) error {
	w := getCLIWorld(ctx)
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(w.stdout), &raw); err != nil {
		return fmt.Errorf("stdout is not valid JSON: %w\nstdout:\n%s", err, w.stdout)
	}
	return nil
}

func theJSONFieldEquals(ctx context.Context, path, expected string) error {
	w := getCLIWorld(ctx)
	val, err := jsonFieldAt(w.stdout, path)
	if err != nil {
		return err
	}

	str, ok := val.(string)
	if !ok {
		return fmt.Errorf("JSON field %q is not a string: %v", path, val)
	}

	if str != expected {
		return fmt.Errorf("JSON field %q = %q, expected %q", path, str, expected)
	}
	return nil
}

func theJSONFieldIsNotEmpty(ctx context.Context, path string) error {
	w := getCLIWorld(ctx)
	val, err := jsonFieldAt(w.stdout, path)
	if err != nil {
		return err
	}

	switch v := val.(type) {
	case string:
		if v == "" {
			return fmt.Errorf("JSON field %q is empty string", path)
		}
	case nil:
		return fmt.Errorf("JSON field %q is null", path)
	}
	return nil
}

func theJSONFieldIsArrayWithAtLeast(ctx context.Context, path string, minItems int) error {
	w := getCLIWorld(ctx)
	val, err := jsonFieldAt(w.stdout, path)
	if err != nil {
		return err
	}

	arr, ok := val.([]any)
	if !ok {
		return fmt.Errorf("JSON field %q is not an array: %T", path, val)
	}

	if len(arr) < minItems {
		return fmt.Errorf("JSON field %q has %d items, expected at least %d", path, len(arr), minItems)
	}
	return nil
}

// jsonFieldAt navigates a dot-separated path into a JSON object.
func jsonFieldAt(jsonStr, path string) (any, error) {
	var data any
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	parts := strings.Split(path, ".")
	current := data

	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("cannot navigate to %q: not an object at %q", path, part)
		}

		val, exists := m[part]
		if !exists {
			return nil, fmt.Errorf("JSON field %q not found (available: %v)", path, keys(m))
		}

		current = val
	}

	return current, nil
}

func keys(m map[string]any) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}

	return result
}

// --- Init step implementations ---

func aTemporaryInitDirectory(ctx context.Context) (context.Context, error) {
	w := getCLIWorld(ctx)

	tmpDir, err := os.MkdirTemp("", "gtb-e2e-init-*")
	if err != nil {
		return ctx, fmt.Errorf("failed to create temp init dir: %w", err)
	}

	w.initDir = tmpDir

	return ctx, nil
}

func theInitDirContainsConfigFile(ctx context.Context, content *godog.DocString) (context.Context, error) {
	w := getCLIWorld(ctx)

	cfgPath := filepath.Join(w.initDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(content.Content), 0o644); err != nil {
		return ctx, fmt.Errorf("failed to write config file: %w", err)
	}

	return ctx, nil
}

func theFileExistsInInitDir(ctx context.Context, filename string) error {
	w := getCLIWorld(ctx)

	path := filepath.Join(w.initDir, filename)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("file %q does not exist in init directory: %w", filename, err)
	}

	return nil
}

func theInitConfigContains(ctx context.Context, substr string) error {
	w := getCLIWorld(ctx)

	content, err := os.ReadFile(filepath.Join(w.initDir, "config.yaml"))
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if !strings.Contains(string(content), substr) {
		return fmt.Errorf("config file does not contain %q\nconfig:\n%s", substr, content)
	}

	return nil
}

func theInitConfigDoesNotContain(ctx context.Context, substr string) error {
	w := getCLIWorld(ctx)

	content, err := os.ReadFile(filepath.Join(w.initDir, "config.yaml"))
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if strings.Contains(string(content), substr) {
		return fmt.Errorf("config file should not contain %q\nconfig:\n%s", substr, content)
	}

	return nil
}

// theScenarioConfigContains reads the scenario's --config file (the one
// iRunGTBWith always points at) and asserts a substring is present.
// Distinct from theInitConfigContains, which targets the --dir path
// used by the init command.
func theScenarioConfigContains(ctx context.Context, substr string) error {
	w := getCLIWorld(ctx)

	content, err := os.ReadFile(filepath.Join(w.configDir, "config.yaml"))
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if !strings.Contains(string(content), substr) {
		return fmt.Errorf("config file does not contain %q\nconfig:\n%s", substr, content)
	}

	return nil
}

func theScenarioConfigDoesNotContain(ctx context.Context, substr string) error {
	w := getCLIWorld(ctx)

	content, err := os.ReadFile(filepath.Join(w.configDir, "config.yaml"))
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if strings.Contains(string(content), substr) {
		return fmt.Errorf("config file should not contain %q\nconfig:\n%s", substr, content)
	}

	return nil
}

// --- `gtb keys` helpers ---------------------------------------------

// aTemporaryKeysDirectory provisions an isolated scratch dir for
// `gtb keys *` outputs. Referenced by the `{keys_dir}` placeholder in
// `I run gtb with "..."` steps. Cleaned up in the scenario After hook.
func aTemporaryKeysDirectory(ctx context.Context) (context.Context, error) {
	w := getCLIWorld(ctx)

	tmpDir, err := os.MkdirTemp("", "gtb-e2e-keys-*")
	if err != nil {
		return ctx, fmt.Errorf("failed to create temp keys dir: %w", err)
	}

	w.keysDir = tmpDir

	return ctx, nil
}

// theFileExistsInKeysDir asserts a file (or nested file) exists under
// the keys scratch dir. The path is joined onto keysDir so callers can
// use either a bare filename ("release.asc") or a relative path
// ("wkd-staging/.well-known/openpgpkey/example.org/policy").
func theFileExistsInKeysDir(ctx context.Context, path string) error {
	w := getCLIWorld(ctx)

	full := filepath.Join(w.keysDir, path)
	if _, err := os.Stat(full); err != nil {
		return fmt.Errorf("file %q does not exist in keys directory: %w", path, err)
	}

	return nil
}

// theFileInKeysDirContains asserts the file's bytes contain substr.
// Used to spot-check OpenPGP armor headers without parsing the
// underlying packets — the unit tests cover parsing correctness.
func theFileInKeysDirContains(ctx context.Context, path, substr string) error {
	w := getCLIWorld(ctx)

	content, err := os.ReadFile(filepath.Join(w.keysDir, path))
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	if !strings.Contains(string(content), substr) {
		return fmt.Errorf("file %q should contain %q\ncontent:\n%s", path, substr, content)
	}

	return nil
}

// anRSAKeypairHasBeenGeneratedAs invokes `gtb keys generate
// --algorithm rsa` to seed a pair of `<basename>.asc` / `<basename>.pem`
// files for subsequent mint / wkd scenarios. Smaller key (2048) for
// test speed; cryptographic-strength behaviour belongs in unit tests.
func anRSAKeypairHasBeenGeneratedAs(ctx context.Context, basename string) (context.Context, error) {
	w := getCLIWorld(ctx)

	parts := []string{
		"keys", "generate",
		"--algorithm", "rsa",
		"--rsa-bits", "2048",
		"--name", "Test",
		"--email", "test@example.org",
		"--output", filepath.Join(w.keysDir, basename+".asc"),
		"--private-output", filepath.Join(w.keysDir, basename+".pem"),
		"--ci",
		"--config", filepath.Join(w.configDir, "config.yaml"),
	}

	cmd := exec.CommandContext(ctx, w.binaryPath, parts...) //nolint:gosec // test-only: args derived from Gherkin step

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return ctx, fmt.Errorf("seed RSA keypair %q failed: %w\nstdout: %s\nstderr: %s", basename, err, stdout.String(), stderr.String())
	}

	return ctx, nil
}
