package steps_test

import (
	"os"
	"testing"

	"github.com/cucumber/godog"

	"github.com/phpboyscout/go-tool-base/internal/testutil"
	"github.com/phpboyscout/go-tool-base/test/e2e/support"
)

func TestFeatures(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "e2e")
	t.Cleanup(support.CleanupBinary)

	opts := &godog.Options{
		Format:   "pretty",
		Paths:    []string{"../../../features"},
		TestingT: t,
	}

	opts.Tags = buildTagExpression()

	suite := godog.TestSuite{
		ScenarioInitializer: initializeScenario,
		Options:             opts,
	}

	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}

func initializeScenario(ctx *godog.ScenarioContext) {
	initControlsSteps(ctx)
	initCLISteps(ctx)
}

func buildTagExpression() string {
	// If a specific subsystem is requested, filter to that
	if os.Getenv("INT_TEST_E2E_SMOKE") != "" {
		return "@smoke"
	}

	if os.Getenv("INT_TEST_E2E_CONTROLS") != "" {
		return "@controls"
	}

	if os.Getenv("INT_TEST_E2E_CLI") != "" {
		return "@cli"
	}

	// Default: run everything
	return ""
}
