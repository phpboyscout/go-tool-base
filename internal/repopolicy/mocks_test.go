package repopolicy

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEveryGeneratedMockIsUsed fails when a file under mocks/ defines a mock
// nothing outside mocks/ constructs. A generated mock never names the
// interface it mocks, so an unused one compiles forever, and .mockery.yml's
// `all: true` used to regenerate mocks for interfaces that had already left
// the tree.
func TestEveryGeneratedMockIsUsed(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	var mockFiles []string

	err := filepath.WalkDir(filepath.Join(root, "mocks"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && strings.HasSuffix(d.Name(), ".go") {
			mockFiles = append(mockFiles, path)
		}

		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, mockFiles)

	tests := testSources(t, root)

	for _, f := range mockFiles {
		name := strings.TrimSuffix(filepath.Base(f), ".go")
		ctor := regexp.MustCompile(`\bNewMock` + regexp.QuoteMeta(name) + `\(`)

		used := false

		for _, src := range tests {
			if ctor.Match(src) {
				used = true

				break
			}
		}

		rel, _ := filepath.Rel(root, f)
		require.Truef(t, used, "mock %s is constructed by no test outside mocks/", rel)
	}
}

func testSources(t *testing.T, root string) [][]byte {
	t.Helper()

	var sources [][]byte

	for _, f := range listGoFiles(t, root) {
		if !strings.HasSuffix(f, "_test.go") || strings.HasPrefix(f, "mocks/") {
			continue
		}

		src, err := os.ReadFile(filepath.Join(root, f))
		require.NoError(t, err)

		sources = append(sources, src)
	}

	return sources
}
