package repopolicy

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPackageLevelSentinelsUseNewSentinel enforces the AGENTS.md rule: a
// package-scope error built with errors.New captures its stack at
// runtime.doInit, so package-level sentinels use errors.NewSentinel.
func TestPackageLevelSentinelsUseNewSentinel(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	pkgLevelNew := regexp.MustCompile(`(?m)^(?:var\s+\w+\s*=|\s+\w+\s*=)\s*errors\.New\(`)

	var offenders []string

	for _, f := range listGoFiles(t, root) {
		if strings.HasSuffix(f, "_test.go") || (!strings.HasPrefix(f, "pkg/") && !strings.HasPrefix(f, "cli/pkg/")) {
			continue
		}

		src, err := os.ReadFile(filepath.Join(root, f))
		require.NoError(t, err)

		// The rule is about go/errors, whose New captures a stack. A file on
		// stdlib errors (pkg/logger, by deliberate exception) is not affected.
		if !strings.Contains(string(src), `"gitlab.com/phpboyscout/go/errors"`) {
			continue
		}

		for _, line := range packageLevelErrorsNew(string(src), pkgLevelNew) {
			offenders = append(offenders, f+":"+strconv.Itoa(line))
		}
	}

	require.Empty(t, offenders, "package-level errors.New; use errors.NewSentinel")
}

// packageLevelErrorsNew returns the 1-based lines that declare a top-level
// variable with errors.New: a `var x = errors.New(` at column 0, or an
// indented one inside a top-level `var (` block.
func packageLevelErrorsNew(src string, pkgLevelNew *regexp.Regexp) []int {
	var lines []int

	inVarBlock := false

	for i, line := range strings.Split(src, "\n") {
		switch {
		case strings.HasPrefix(line, "var ("):
			inVarBlock = true
		case inVarBlock && strings.HasPrefix(line, ")"):
			inVarBlock = false
		}

		if (strings.HasPrefix(line, "var ") || inVarBlock) && pkgLevelNew.MatchString(line) {
			lines = append(lines, i+1)
		}
	}

	return lines
}
