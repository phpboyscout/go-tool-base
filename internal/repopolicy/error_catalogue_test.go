package repopolicy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const errorCatalogue = "docs/explanation/components/errors.md"

var (
	sentinelName     = regexp.MustCompile(`^Err[A-Z]`)
	catalogueHeading = regexp.MustCompile("^#{2,} .*`(pkg/[^`]+)`")
	catalogueRow     = regexp.MustCompile("^\\| `(Err[A-Z][A-Za-z0-9]*)` \\|")
)

// TestErrorCatalogueMatchesSentinels holds the catalogue to every exported
// Err variable in pkg/, however it is declared (#152, #220). A row counts only
// under its own package's heading, since two packages can share a name.
func TestErrorCatalogueMatchesSentinels(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	declared := declaredSentinels(t, root)
	require.NotEmpty(t, declared, "no sentinels found in pkg/: the scan itself is broken")

	src, err := os.ReadFile(filepath.Join(root, errorCatalogue))
	require.NoError(t, err)

	documented := catalogueRows(string(src))

	var missing, stale []string

	for s := range declared {
		if !documented[s] {
			missing = append(missing, s)
		}
	}

	for s := range documented {
		if !declared[s] {
			stale = append(stale, s)
		}
	}

	slices.Sort(missing)
	slices.Sort(stale)

	require.Empty(t, missing, "sentinels with no row under their package's heading in %s", errorCatalogue)
	require.Empty(t, stale, "rows in %s for sentinels their package does not declare", errorCatalogue)
}

// declaredSentinels returns "<package dir> <name>" for each exported package-level Err variable.
func declaredSentinels(t *testing.T, root string) map[string]bool {
	t.Helper()

	found := map[string]bool{}
	fset := token.NewFileSet()

	for _, f := range listGoFiles(t, root) {
		if !strings.HasPrefix(f, "pkg/") || strings.HasSuffix(f, "_test.go") || strings.Contains(f, "/testdata/") {
			continue
		}

		file, err := parser.ParseFile(fset, filepath.Join(root, f), nil, parser.SkipObjectResolution)
		require.NoError(t, err)

		for _, name := range packageVars(file) {
			if name.IsExported() && sentinelName.MatchString(name.Name) {
				found[path.Dir(f)+" "+name.Name] = true
			}
		}
	}

	return found
}

func packageVars(file *ast.File) []*ast.Ident {
	var names []*ast.Ident

	for _, decl := range file.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.VAR {
			for _, spec := range gen.Specs {
				names = append(names, spec.(*ast.ValueSpec).Names...)
			}
		}
	}

	return names
}

// catalogueRows returns "<package dir> <name>" for each row whose first cell
// names a sentinel, under the nearest heading that names a pkg/ path. Rows in
// a fenced block or an HTML comment are not rendered, so they do not count.
// Headings naming an extracted module hold that module's sentinels, which
// this test does not check.
func catalogueRows(src string) map[string]bool {
	rows := map[string]bool{}
	pkg := ""
	fenced, commented := false, false

	for line := range strings.SplitSeq(src, "\n") {
		switch {
		case strings.HasPrefix(line, "```"):
			fenced = !fenced

			continue
		case commented || strings.HasPrefix(line, "<!--"):
			commented = !strings.Contains(line, "-->")

			continue
		case fenced:
			continue
		}

		if strings.HasPrefix(line, "##") {
			pkg = ""
			if m := catalogueHeading.FindStringSubmatch(line); m != nil {
				pkg = m[1]
			}

			continue
		}

		if m := catalogueRow.FindStringSubmatch(line); m != nil && pkg != "" {
			rows[pkg+" "+m[1]] = true
		}
	}

	return rows
}
