package repopolicy

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRootAssetsGenerateDirectivesResolve pins the two go:generate lines that
// produce the gtb binary's embedded docs and changelog. They moved with their
// package into the nested module and kept pre-move relative paths, so three
// releases shipped with an empty embed (#39). Each flag is resolved the way
// the tool resolves it and must land where the embed reads.
func TestRootAssetsGenerateDirectivesResolve(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	pkgDir := filepath.Join(root, "cli", "pkg", "cmd", "root")

	src, err := os.ReadFile(filepath.Join(pkgDir, "generate.go"))
	require.NoError(t, err)

	directives := regexp.MustCompile(`(?m)^//go:generate (.*)$`).FindAllStringSubmatch(string(src), -1)
	require.Len(t, directives, 2)

	var docs, changelog string

	for _, d := range directives {
		switch {
		case strings.Contains(d[1], "go tool docs"):
			docs = d[1]
		case strings.Contains(d[1], "go tool changelog"):
			changelog = d[1]
		}
	}

	require.NotEmpty(t, docs, "docs directive missing")
	require.NotEmpty(t, changelog, "changelog directive missing")

	// go generate runs in the package directory; the docs tool joins
	// --target-dir onto --project-root.
	projectRoot := filepath.Clean(filepath.Join(pkgDir, flagValue(t, docs, "--project-root")))
	require.Equal(t, root, projectRoot, "--project-root must resolve to the repository root (where docs/ lives)")

	target := filepath.Join(projectRoot, flagValue(t, docs, "--target-dir"))
	require.Equal(t, filepath.Join(pkgDir, "assets"), target, "--target-dir must be the directory root.go embeds")

	out := filepath.Join(pkgDir, flagValue(t, changelog, "--output"))
	require.Equal(t, filepath.Join(pkgDir, "assets", "CHANGELOG.md"), out)
}

// TestReleaseBuildGeneratesBothModules pins that the GoReleaser before-hook
// reaches the nested module: `go generate ./...` from the root does not, in
// workspace mode.
func TestReleaseBuildGeneratesBothModules(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".goreleaser.yaml"))
	require.NoError(t, err)

	require.Contains(t, string(raw), "go generate ./... ./cli/...",
		"the release build must generate the CLI's embedded assets too")
}

func flagValue(t *testing.T, directive, flag string) string {
	t.Helper()

	fields := strings.Fields(directive)
	for i, f := range fields {
		if f == flag && i+1 < len(fields) {
			return fields[i+1]
		}
	}

	require.Failf(t, "flag missing", "%s not found in %q", flag, directive)

	return ""
}
