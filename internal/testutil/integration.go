// Package testutil provides shared test helpers for the go-tool-base module.
package testutil

import (
	"os"
	"strings"
	"testing"
)

// SkipIfNotIntegration skips the current test unless integration tests are
// enabled. Tests are enabled when:
//
//   - INT_TEST is set to any non-empty value (runs all integration tests), OR
//   - INT_TEST_<TAG> is set for any of the provided tags (targeted runs).
//
// Tags are matched case-insensitively against INT_TEST_<TAG> environment
// variables, allowing targeted execution of specific test groups:
//
//	testutil.SkipIfNotIntegration(t, "vcs")    // runs if INT_TEST=1 OR INT_TEST_VCS=1
//	testutil.SkipIfNotIntegration(t, "config") // runs if INT_TEST=1 OR INT_TEST_CONFIG=1
//	testutil.SkipIfNotIntegration(t)           // runs only if INT_TEST=1
func SkipIfNotIntegration(t *testing.T, tags ...string) {
	t.Helper()

	if os.Getenv("INT_TEST") != "" {
		return
	}

	for _, tag := range tags {
		if os.Getenv("INT_TEST_"+strings.ToUpper(tag)) != "" {
			return
		}
	}

	msg := "skipping integration test; set INT_TEST=1 to run all"

	if len(tags) > 0 {
		tagNames := make([]string, len(tags))
		for i, tag := range tags {
			tagNames[i] = "INT_TEST_" + strings.ToUpper(tag)
		}

		msg += " or " + strings.Join(tagNames, "/") + "=1 for this group"
	}

	t.Skip(msg)
}
