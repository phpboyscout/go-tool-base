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

	"github.com/phpboyscout/go-tool-base/test/e2e/support"
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

		return ctx, nil
	})

	// --- Given ---
	ctx.Step(`^the gtb binary is built$`, theGTBBinaryIsBuilt)
	ctx.Step(`^a temporary init directory$`, aTemporaryInitDirectory)
	ctx.Step(`^the init directory contains a config file:$`, theInitDirContainsConfigFile)
	ctx.Step(`^an empty config directory$`, anEmptyConfigDirectory)

	// --- When ---
	ctx.Step(`^I set environment variable "([^"]*)" to "([^"]*)"$`, iSetEnvironmentVariable)
	ctx.Step(`^I run gtb with "([^"]*)"$`, iRunGTBWith)

	// --- Then ---
	ctx.Step(`^the exit code is (\d+)$`, theExitCodeIs)
	ctx.Step(`^the exit code is not (\d+)$`, theExitCodeIsNot)
	ctx.Step(`^stdout contains "([^"]*)"$`, stdoutContains)
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

func anEmptyConfigDirectory(ctx context.Context) (context.Context, error) {
	w := getCLIWorld(ctx)

	// Remove the default config file so the directory is truly empty
	cfgPath := filepath.Join(w.configDir, "config.yaml")
	if err := os.Remove(cfgPath); err != nil && !os.IsNotExist(err) {
		return ctx, fmt.Errorf("failed to remove default config: %w", err)
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

	parts := strings.Fields(args)
	// Always pass --ci to skip update checks and interactive prompts
	parts = append(parts, "--ci")
	// Point to the per-scenario temp config so the binary doesn't require a real install
	parts = append(parts, "--config", filepath.Join(w.configDir, "config.yaml"))
	cmd := exec.CommandContext(ctx, w.binaryPath, parts...) //nolint:gosec // test-only: args from Gherkin steps

	if len(w.envVars) > 0 {
		cmd.Env = os.Environ()
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
