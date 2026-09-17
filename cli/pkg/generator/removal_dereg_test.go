package generator

import (
	"context"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestDeregisterSubcommand_RootVariadicArg is the regression guard for keryx
// v0.19.0 Bug 1: removing a root-level command stripped the import from
// pkg/cmd/root/cmd.go but left the `foo.NewCmdFoo(p)` argument inside the
// variadic gtbRoot.NewCmdRoot(p, ...) call — so the project no longer compiled
// (`undefined: foo`). De-registration must remove BOTH the import and the call.
func TestDeregisterSubcommand_RootVariadicArg(t *testing.T) {
	t.Parallel()

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

// TestRemove_RefusesProtectedCommand is the 2.1.1 guard: `gtb remove command`
// must not delete a command marked Protected in the manifest — the operator
// protected it precisely because it carries hand-written logic. --force is the
// only escape hatch.
func TestRemove_RefusesProtectedCommand(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	workDir := "/work"

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "go.mod"), []byte("module github.com/acme/demo\n"), 0o644))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/secret"), 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/secret/cmd.go"), []byte("package secret\n"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/secret/main.go"), []byte("package main\n"), 0o644))

	m := Manifest{
		Properties: ManifestProperties{Name: "demo"},
		Commands: []ManifestCommand{
			{Name: "secret", Protected: boolPtr(true)},
		},
	}
	data, _ := yaml.Marshal(m)
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, ".gtb"), 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"), data, 0o644))

	p := &props.Props{FS: fs, Logger: logger.NewNoop(), Tool: props.Tool{Name: "demo"}}

	g := New(p, &Config{Path: workDir, Name: "secret", Parent: "root"})
	err := g.Remove(context.Background())
	require.Error(t, err, "removing a protected command must fail")
	require.ErrorIs(t, err, ErrCommandProtected)

	exists, _ := afero.Exists(fs, filepath.Join(workDir, "pkg/cmd/secret/cmd.go"))
	assert.True(t, exists, "protected command directory must be untouched")

	// --force overrides the protection guard.
	gf := New(p, &Config{Path: workDir, Name: "secret", Parent: "root", Force: true})
	require.NoError(t, gf.Remove(context.Background()))

	exists, _ = afero.Exists(fs, filepath.Join(workDir, "pkg/cmd/secret"))
	assert.False(t, exists, "--force must remove the protected command")
}
