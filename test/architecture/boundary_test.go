// Package architecture holds source-level guard tests that lock in the module
// boundaries the codebase has committed to, so an accidental import cannot
// silently re-couple a package that has been prepared for extraction.
package architecture

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const propsImportPath = `"gitlab.com/phpboyscout/go-tool-base/pkg/props"`

// firstWaveRoots are the package trees decoupled from GTB's composition layer
// for future extraction as standalone modules. Their non-adapter source must
// not import pkg/props — the main-module weight vector that drags in the tool
// runtime, collector, and version machinery. GTB coupling for these packages
// belongs in *_adapter.go files, which are exempt.
//
// pkg/config is intentionally NOT guarded here: it is itself an early
// extraction candidate and a legitimate lightweight dependency, so packages may
// import it directly (see docs/development/specs/2026-07-07-config-section-adapters-for-extraction.md).
//
// pkg/telemetry (root) is intentionally excluded: per the package extraction
// report it still couples to props through product-analytics consent and must
// not be extracted until observability is split from analytics.
var firstWaveRoots = []string{
	"pkg/chat",
	"pkg/tls",
	"pkg/http",
	"pkg/grpc",
	"pkg/gateway",
	"pkg/vcs",
	"pkg/telemetry/otelcore",
}

// TestFirstWaveCoresDoNotImportProps fails if any non-adapter, non-test Go file
// in a first-wave package tree imports pkg/props. This keeps the GTB coupling
// confined to the adapter boundary so each package stays extractable.
func TestFirstWaveCoresDoNotImportProps(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()

	checked := 0

	for _, rel := range firstWaveRoots {
		dir := filepath.Join(root, rel)

		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			if d.IsDir() || !isGuardedGoFile(d.Name()) {
				return nil
			}

			checked++

			file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}

			for _, imp := range file.Imports {
				if imp.Path.Value == propsImportPath {
					relPath, _ := filepath.Rel(root, path)
					t.Errorf(
						"%s imports pkg/props outside an adapter file; move the GTB "+
							"coupling into a *_adapter.go file to keep the package extractable",
						relPath,
					)
				}
			}

			return nil
		})
		require.NoErrorf(t, err, "walking %s", rel)
	}

	// Guard against the test silently passing because it inspected nothing
	// (e.g. a mistaken root path or an over-broad exclusion filter).
	require.Greaterf(t, checked, 20,
		"boundary guard only inspected %d files; expected the first-wave cores to have many more", checked)
}

// isGuardedGoFile reports whether a file is subject to the boundary rule: Go
// source that is neither a test file nor an adapter file. Adapter files (named
// *adapter*.go, e.g. config_adapter.go) are the sanctioned home for GTB
// coupling and are therefore exempt.
func isGuardedGoFile(name string) bool {
	if !strings.HasSuffix(name, ".go") {
		return false
	}

	if strings.HasSuffix(name, "_test.go") {
		return false
	}

	return !strings.Contains(name, "adapter")
}

// repoRoot walks up from this test file until it finds the module's go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "cannot determine caller for repo root discovery")

	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		require.NotEqualf(t, parent, dir, "reached filesystem root without finding go.mod from %s", thisFile)
		dir = parent
	}
}
