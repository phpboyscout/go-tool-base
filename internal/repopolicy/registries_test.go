package repopolicy

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoSealsAndNoRegistryResets pins spec 0199 D1 and D5: a reader takes a
// snapshot of the feature registry instead of sealing it, and a test that
// needs its own features builds its own Registry instead of resetting the
// process's. Comments are not sites.
func TestNoSealsAndNoRegistryResets(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	banned := regexp.MustCompile(`\b(ResetRegistryForTesting|SealRegistry|SealFeatures)\b`)
	// A test declaring or contributing on the default registry would leak
	// into every other test's enumeration; RegisterFeature at init is the
	// production path and stays out of test files.
	defaultWrite := regexp.MustCompile(`features\.Default\(\)\.(Declare|Contribute)\(|\bRegisterFeature\(`)
	comment := regexp.MustCompile(`^\s*//`)

	var offenders []string

	for _, f := range listGoFiles(t, root) {
		if strings.HasPrefix(f, "internal/repopolicy/") {
			continue
		}

		src, err := os.ReadFile(filepath.Join(root, f))
		require.NoError(t, err)

		isTest := strings.HasSuffix(f, "_test.go")

		for i, line := range strings.Split(string(src), "\n") {
			if comment.MatchString(line) {
				continue
			}

			if banned.MatchString(line) || (isTest && defaultWrite.MatchString(line)) {
				offenders = append(offenders, f+":"+strconv.Itoa(i+1))
			}
		}
	}

	assert.Empty(t, offenders, "snapshot the registry instead of sealing it; a test builds its own features.NewRegistry()")
}
