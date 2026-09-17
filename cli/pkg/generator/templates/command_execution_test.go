package templates

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// importsOf parses src and returns its import paths.
func importsOf(t *testing.T, src string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "main.go", src, 0)
	require.NoError(t, err, src)

	var paths []string
	for _, imp := range file.Imports {
		paths = append(paths, imp.Path.Value)
	}

	return paths
}

// usesIdent reports whether any selector in src has the given package name.
func usesIdent(src string, pkg string) bool {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", src, 0)
	if err != nil {
		return false
	}

	found := false

	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == pkg {
				found = true
			}
		}

		return !found
	})

	return found
}

// TestCommandExecution_ImportsOnlyWhatItUses (#80): a pure group emits no
// function, so it must import nothing; every other shape imports only what
// the functions it emits reference. The generator's lint fix pass used to
// hide the unused imports, and the compile gate saw the raw output.
func TestCommandExecution_ImportsOnlyWhatItUses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		data CommandData
	}{
		{"pure group", CommandData{Name: "deploy", Package: "deploy", PascalName: "Deploy", PureGroup: true}},
		{"pure group with pre run", CommandData{Name: "deploy", Package: "deploy", PascalName: "Deploy", PureGroup: true, PreRun: true}},
		{"pure group with initializer", CommandData{Name: "deploy", Package: "deploy", PascalName: "Deploy", PureGroup: true, WithInitializer: true}},
		{"leaf", CommandData{Name: "canary", Package: "canary", PascalName: "Canary"}},
		{"leaf with hooks and initializer", CommandData{Name: "canary", Package: "canary", PascalName: "Canary", PersistentPreRun: true, WithInitializer: true}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := CommandExecution(tc.data)
			imports := importsOf(t, src)

			for _, imp := range imports {
				switch imp {
				case `"context"`:
					assert.True(t, usesIdent(src, "context"), "context imported but unused:\n%s", src)
				case `"gitlab.com/phpboyscout/go-tool-base/pkg/props"`:
					assert.True(t, usesIdent(src, "props"), "props imported but unused:\n%s", src)
				case `"gitlab.com/phpboyscout/go/errorhandling"`:
					assert.True(t, usesIdent(src, "errorhandling"), "errorhandling imported but unused:\n%s", src)
				case `"gitlab.com/phpboyscout/go-tool-base/pkg/setup"`:
					assert.True(t, usesIdent(src, "setup"), "setup imported but unused:\n%s", src)
				}
			}
		})
	}
}

// TestCommandExecution_ImportsAreGroupedForGoimports (#30): stdlib, third
// party and the project's own packages in separate sorted groups, which is
// what the scaffold's goimports local-prefixes setting checks.
func TestCommandExecution_ImportsAreGroupedForGoimports(t *testing.T) {
	t.Parallel()

	src := CommandExecution(CommandData{
		Name: "canary", Package: "canary", PascalName: "Canary", WithInitializer: true,
		ModulePath: "github.com/acme/mytool",
		Imports:    []string{"github.com/acme/mytool/internal/shared", "strings"},
	})

	assert.Contains(t, src, "import (\n\t\"context\"\n\t\"strings\"\n\n\t\"gitlab.com/phpboyscout/go-tool-base/pkg/props\"\n\t\"gitlab.com/phpboyscout/go-tool-base/pkg/setup\"\n\t\"gitlab.com/phpboyscout/go/errorhandling\"\n\n\t\"github.com/acme/mytool/internal/shared\"\n)\n", src)
}
