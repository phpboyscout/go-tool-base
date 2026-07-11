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

// guardedImport is a GTB package that first-wave cores must not import outside
// their adapter files, together with the reason the boundary exists. The reason
// is surfaced verbatim in the failure message so a contributor who trips the
// guard learns what to do instead of merely that they broke a rule.
type guardedImport struct {
	// pkg is the module-relative package path, e.g. "pkg/props".
	pkg string
	// remedy tells the contributor where the coupling belongs instead.
	remedy string
}

// guardedImports are the GTB composition-layer packages barred from first-wave
// package cores.
//
// pkg/config is intentionally NOT guarded: it is itself an early extraction
// candidate and a legitimate lightweight dependency, so packages may import it
// directly (see docs/development/specs/2026-07-07-config-section-adapters-for-extraction.md).
var guardedImports = []guardedImport{
	{
		pkg: "pkg/props",
		remedy: "move the GTB coupling into a *_adapter.go file, or accept a narrow " +
			"typed settings struct the adapter populates",
	},
	{
		pkg: "pkg/logger",
		remedy: "accept a *slog.Logger instead, and convert at the adapter boundary " +
			"with logger.ToSlog (see docs/development/specs/2026-07-07-slog-first-extraction-seams.md)",
	},
}

// firstWaveRoots are the package trees decoupled from GTB's composition layer
// for future extraction as standalone modules. Their non-adapter source must
// import neither pkg/props — the main-module weight vector that drags in the
// tool runtime, collector, and version machinery — nor pkg/logger, GTB's
// logging facade, which the slog-first work replaced with *slog.Logger at every
// package seam. GTB coupling for these packages belongs in *_adapter.go files,
// which are exempt.
//
// pkg/telemetry (root) is included: the shared telemetry value types moved to
// the pkg/telemetrytypes leaf (see
// docs/development/specs/2026-07-10-telemetry-props-decoupling.md), so the
// collector no longer imports pkg/props from its core. Its *_adapter.go files
// still read config through props and remain exempt. Note the tree is not yet
// fully extractable — the product-analytics / observability split is a separate
// effort — but the props *type* coupling this guard protects is now gone.
var firstWaveRoots = []string{
	"pkg/chat",
	"pkg/tls",
	"pkg/http",
	"pkg/grpc",
	"pkg/gateway",
	"pkg/vcs",
	"pkg/telemetry",
}

// TestFirstWaveCoresDoNotImportGTBComposition fails if any non-adapter,
// non-test Go file in a first-wave package tree imports a guarded GTB package.
// This keeps the GTB coupling confined to the adapter boundary so each package
// stays extractable.
func TestFirstWaveCoresDoNotImportGTBComposition(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	files := guardedFiles(t, root)

	// Guard against the test silently passing because it inspected nothing
	// (e.g. a mistaken root path or an over-broad exclusion filter).
	require.Greaterf(t, len(files), 20,
		"boundary guard only inspected %d files; expected the first-wave cores to have many more", len(files))

	for _, guarded := range guardedImports {
		t.Run(guarded.pkg, func(t *testing.T) {
			t.Parallel()

			want := `"gitlab.com/phpboyscout/go-tool-base/` + guarded.pkg + `"`

			for _, file := range files {
				for _, imp := range file.imports {
					if imp == want {
						t.Errorf("%s imports %s outside an adapter file; %s",
							file.path, guarded.pkg, guarded.remedy)
					}
				}
			}
		})
	}
}

// guardedFile is a single parsed source file subject to the boundary rule.
type guardedFile struct {
	// path is relative to the repo root, so failures are clickable.
	path string
	// imports are the raw quoted import paths, as they appear in source.
	imports []string
}

// guardedFiles parses every guarded file across the first-wave roots once, so
// each guarded import is checked against the same scan rather than re-walking.
func guardedFiles(t *testing.T, root string) []guardedFile {
	t.Helper()

	fset := token.NewFileSet()

	var files []guardedFile

	for _, rel := range firstWaveRoots {
		err := filepath.WalkDir(filepath.Join(root, rel), func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			if d.IsDir() || !isGuardedGoFile(d.Name()) {
				return nil
			}

			parsed, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}

			imports := make([]string, 0, len(parsed.Imports))
			for _, imp := range parsed.Imports {
				imports = append(imports, imp.Path.Value)
			}

			relPath, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}

			files = append(files, guardedFile{path: relPath, imports: imports})

			return nil
		})
		require.NoErrorf(t, err, "walking %s", rel)
	}

	return files
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
