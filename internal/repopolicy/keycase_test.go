package repopolicy

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// foldedOnPurpose are the production-path files whose Stores ignore the
// tool's key mode by design: gtb's own test helpers.
var foldedOnPurpose = map[string]bool{
	"internal/testutil/config.go": true,
}

// TestEveryStoreTakesTheToolsKeyCase pins spec 0209 D2: a file that builds a
// go/config Store also applies the tool's key mode, so no Store reads a key
// differently from another. It checks that the file mentions WithKeyCase, not
// that each call routes through it; the Store-level tests carry that proof.
func TestEveryStoreTakesTheToolsKeyCase(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	newStore := regexp.MustCompile(`\b(config|cfg)\.NewStore\(`)

	var offenders []string

	for _, f := range listGoFiles(t, root) {
		if strings.HasSuffix(f, "_test.go") || strings.HasPrefix(f, "internal/repopolicy/") || foldedOnPurpose[f] {
			continue
		}

		src, err := os.ReadFile(filepath.Join(root, f))
		require.NoError(t, err)

		if newStore.Match(src) && !strings.Contains(string(src), "WithKeyCase(") {
			offenders = append(offenders, f)
		}
	}

	assert.Empty(t, offenders, "build the Store's options through props.ConfigSpec.WithKeyCase")
}
