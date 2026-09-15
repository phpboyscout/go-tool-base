package root

import (
	"io/fs"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
)

// TestEmbeddedAssetsArePresent runs after `go generate ./cli/...` (the CI job
// gtb-embedded-assets sets INT_TEST_ASSETS=1 once it has) and fails when the
// gtb binary would ship without its documentation or changelog, which three
// releases did (#39). The e2e binary embeds its own fixture and cannot see
// this; the gitignored assets are absent on a plain checkout, so it is gated.
func TestEmbeddedAssetsArePresent(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "assets")

	paths := []string{"assets/CHANGELOG.md", "assets/docs/index.md"}

	// The static site is built only where the docs tool finds a builder; the
	// raw docs and the changelog are produced regardless.
	if _, err := exec.LookPath("zensical"); err == nil {
		paths = append(paths, "assets/site/index.html")
	}

	for _, path := range paths {
		info, err := fs.Stat(assets, path)
		require.NoErrorf(t, err, "%s is not embedded; did go generate run in cli/?", path)
		require.Positivef(t, info.Size(), "%s is embedded but empty", path)
	}
}
