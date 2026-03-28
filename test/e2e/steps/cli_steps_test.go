package steps_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cucumber/godog"

	"github.com/phpboyscout/go-tool-base/test/e2e/support"
)

type cliWorldKey struct{}

type cliWorld struct {
	binaryPath string
	stdout     string
	stderr     string
	exitCode   int
}

func getCLIWorld(ctx context.Context) *cliWorld {
	return ctx.Value(cliWorldKey{}).(*cliWorld)
}

func initCLISteps(ctx *godog.ScenarioContext) {
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		w := &cliWorld{}
		return context.WithValue(ctx, cliWorldKey{}, w), nil
	})

	// --- Given ---
	ctx.Step(`^the gtb binary is built$`, theGTBBinaryIsBuilt)

	// --- When ---
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

// --- When implementations ---

func iRunGTBWith(ctx context.Context, args string) context.Context {
	w := getCLIWorld(ctx)

	parts := strings.Fields(args)
	// Always pass --ci to skip update checks and interactive prompts in E2E tests
	parts = append(parts, "--ci")
	cmd := exec.CommandContext(ctx, w.binaryPath, parts...) //nolint:gosec // test-only: args from Gherkin steps

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
