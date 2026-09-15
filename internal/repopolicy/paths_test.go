package repopolicy

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "go.work not found above %s", dir)
		dir = parent
	}
}

type coveragePolicy struct {
	NotCounted []string `yaml:"not_counted"`
	Excluded   []struct {
		Pkg string `yaml:"pkg"`
	} `yaml:"excluded"`
}

// TestCoveragePolicyPathsExist fails when .coverage-policy.yaml excludes a
// package or prefix that is no longer in the tree.
func TestCoveragePolicyPathsExist(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	raw, err := os.ReadFile(filepath.Join(root, ".coverage-policy.yaml"))
	require.NoError(t, err)

	var policy coveragePolicy
	require.NoError(t, yaml.Unmarshal(raw, &policy))
	require.NotEmpty(t, policy.Excluded)

	for _, e := range policy.Excluded {
		info, err := os.Stat(filepath.Join(root, e.Pkg))
		require.NoErrorf(t, err, "excluded package %q does not exist", e.Pkg)
		require.Truef(t, info.IsDir(), "excluded package %q is not a directory", e.Pkg)
	}

	for _, prefix := range policy.NotCounted {
		_, err := os.Stat(filepath.Join(root, prefix))
		require.NoErrorf(t, err, "not_counted prefix %q does not exist", prefix)
	}
}

// TestGolangciPathRulesMatchAFile fails when a golangci exclusion `path:`
// pattern matches no Go file in either module, which means the rule
// suppresses nothing and hides that the file it was written for is gone.
func TestGolangciPathRulesMatchAFile(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	raw, err := os.ReadFile(filepath.Join(root, ".golangci.yaml"))
	require.NoError(t, err)

	pathRule := regexp.MustCompile(`(?m)^\s*-\s*path:\s*(\S+)`)
	matches := pathRule.FindAllStringSubmatch(string(raw), -1)
	require.NotEmpty(t, matches)

	goFiles := listGoFiles(t, root)

	for _, m := range matches {
		pattern := m[1]
		re, err := regexp.Compile(pattern)
		require.NoErrorf(t, err, "path rule %q is not a valid regexp", pattern)

		matched := false

		for _, f := range goFiles {
			if re.MatchString(f) {
				matched = true

				break
			}
		}

		require.Truef(t, matched, "golangci path rule %q matches no file in the tree", pattern)
	}
}

func listGoFiles(t *testing.T, root string) []string {
	t.Helper()

	var files []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "bin", "site":
				return filepath.SkipDir
			}

			return nil
		}

		if strings.HasSuffix(d.Name(), ".go") {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}

			files = append(files, filepath.ToSlash(rel))
		}

		return nil
	})
	require.NoError(t, err)

	return files
}
