package generator

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestDeregisterSubcommand_RootVariadicArg is the regression guard for keryx
// v0.19.0 Bug 1: removing a root-level command stripped the import from
// pkg/cmd/root/cmd.go but left the `foo.NewCmdFoo(p)` argument inside the
// variadic gtbRoot.NewCmdRoot(p, ...) call — so the project no longer compiled
// (`undefined: foo`). De-registration must remove BOTH the import and the call.
func TestDeregisterSubcommand_RootVariadicArg(t *testing.T) {
	fs := afero.NewMemMapFs()
	workDir := "/work"

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "go.mod"), []byte("module github.com/acme/demo\n"), 0644))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/foo"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/root"), 0755))

	rootCode := `package root

import (
	"github.com/acme/demo/pkg/cmd/foo"
	"github.com/acme/demo/pkg/cmd/bar"
	gtbRoot "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func NewCmdRoot(p *props.Props) *setup.Command {
	rootCmd := gtbRoot.NewCmdRoot(p,
		foo.NewCmdFoo(p),
		bar.NewCmdBar(p),
	)
	return rootCmd
}
`
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/root/cmd.go"), []byte(rootCode), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/foo/cmd.go"), []byte("package foo\n"), 0644))

	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	g := New(p, &Config{Path: workDir, Name: "foo"}) // Parent empty => root-level

	require.NoError(t, g.deregisterSubcommand())

	out, err := afero.ReadFile(fs, filepath.Join(workDir, "pkg/cmd/root/cmd.go"))
	require.NoError(t, err)
	src := string(out)

	// Both the import and the registration call for foo must be gone...
	assert.NotContains(t, src, "pkg/cmd/foo", "foo import must be removed")
	assert.NotContains(t, src, "foo.NewCmdFoo", "foo registration arg must be removed from NewCmdRoot")

	// ...while the unrelated sibling 'bar' is untouched.
	assert.Contains(t, src, "bar.NewCmdBar", "sibling registration must be preserved")
	assert.Contains(t, src, "pkg/cmd/bar", "sibling import must be preserved")

	// And the result must still parse as valid Go (no dangling references).
	_, perr := parser.ParseFile(token.NewFileSet(), "cmd.go", src, parser.AllErrors)
	require.NoError(t, perr, "regenerated root must still be valid Go:\n%s", src)
	require.NotContains(t, src, "foo", "no stray foo reference should remain")
}
