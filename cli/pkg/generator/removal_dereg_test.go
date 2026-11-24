package generator

import (
	"context"
	"fmt"
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

	_, err := g.deregisterSubcommand()
	require.NoError(t, err)

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

// Removing a command recorded its parent's edited cmd.go as generated, so the
// last child's removal replaced it (#252).
func TestRemove_LeavesAHandEditedParentAlone(t *testing.T) {
	t.Parallel()

	const (
		source = `package serve

import (
	"github.com/acme/demo/pkg/cmd/serve/health"
	"github.com/acme/demo/pkg/cmd/serve/status"
	"github.com/spf13/cobra"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func NewCmdServe(p *props.Props) *setup.Command {
	cmd := setup.Wrap("serve", &cobra.Command{Use: "serve", RunE: setup.GroupRunE})
	cmd.Register(health.NewCmdHealth(p))
	cmd.Register(status.NewCmdStatus(p))

	return cmd
}
`
		edit   = "\n// mine\n"
		parent = "/work/pkg/cmd/serve/cmd.go"
	)

	type project struct {
		unrecorded, sealed bool
	}

	removeIn := func(t *testing.T, pr project, onDisk string, children []string, force bool) (content, recorded string) {
		t.Helper()

		fs := afero.NewMemMapFs()
		m := Manifest{
			Properties: ManifestProperties{Name: "demo"},
			Commands:   []ManifestCommand{{Name: "serve", Hashes: map[string]string{"cmd.go": calculateHash([]byte(source))}}},
		}

		if pr.unrecorded {
			m.Commands[0].Hashes = nil
		}

		if pr.sealed {
			require.NoError(t, afero.WriteFile(fs, "/work/.gtb/ignore", []byte("pkg/cmd/serve/cmd.go sealed\n"), 0o644))
		}

		for _, child := range children {
			m.Commands[0].Commands = append(m.Commands[0].Commands, ManifestCommand{Name: child})
			require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/serve/"+child+"/cmd.go", []byte("package "+child+"\n"), 0o644))
		}

		data, err := yaml.Marshal(m)
		require.NoError(t, err)
		require.NoError(t, afero.WriteFile(fs, "/work/.gtb/manifest.yaml", data, 0o644))
		require.NoError(t, afero.WriteFile(fs, "/work/go.mod", []byte("module github.com/acme/demo\n"), 0o644))
		require.NoError(t, afero.WriteFile(fs, parent, []byte(onDisk), 0o644))

		p := &props.Props{FS: fs, Logger: logger.NewNoop(), Tool: props.Tool{Name: "demo"}}
		require.NoError(t, New(p, &Config{Path: "/work", Name: "health", Parent: "serve", Force: force}).Remove(context.Background()))

		out, err := afero.ReadFile(fs, parent)
		require.NoError(t, err)

		var after Manifest
		data, err = afero.ReadFile(fs, "/work/.gtb/manifest.yaml")
		require.NoError(t, err)
		require.NoError(t, yaml.Unmarshal(data, &after))

		return string(out), after.Commands[0].Hashes["cmd.go"]
	}

	remove := func(t *testing.T, onDisk string, children []string, force bool) (content, recorded string) {
		t.Helper()

		return removeIn(t, project{}, onDisk, children, force)
	}

	for name, children := range map[string][]string{
		"another child stays": {"health", "status"},
		"the last child":      {"health"},
	} {
		for _, force := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, force %v", name, force), func(t *testing.T) {
				t.Parallel()

				content, recorded := remove(t, source+edit, children, force)
				assert.Contains(t, content, "// mine")
				assert.NotContains(t, content, "NewCmdHealth")
				assert.Equal(t, calculateHash([]byte(source)), recorded, "the edit is not gtb's to record")
			})
		}

		for _, force := range []bool{false, true} {
			// regenerate manifest records no hashes, and regenerate project
			// keeps a file it has no hash for.
			t.Run(fmt.Sprintf("%s, no recorded hash, force %v", name, force), func(t *testing.T) {
				t.Parallel()

				content, recorded := removeIn(t, project{unrecorded: true}, source+edit, children, force)
				assert.Contains(t, content, "// mine")
				assert.NotContains(t, content, "NewCmdHealth")
				assert.Empty(t, recorded, "a file gtb has no hash for stays unrecorded")
			})
		}

		t.Run(name+", sealed", func(t *testing.T) {
			t.Parallel()

			content, recorded := removeIn(t, project{sealed: true}, source+edit, children, true)
			assert.Equal(t, source+edit, content, "a sealed parent is not written")
			assert.Equal(t, calculateHash([]byte(source)), recorded)
		})

		t.Run(name+", unedited", func(t *testing.T) {
			t.Parallel()

			content, recorded := remove(t, source, children, false)
			assert.NotContains(t, content, "NewCmdHealth")
			assert.Equal(t, calculateHash([]byte(content)), recorded)
		})
	}
}
