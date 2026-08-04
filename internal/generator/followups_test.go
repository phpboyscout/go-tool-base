package generator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// TestRemove_RefusesProtectedCommand is the 2.1.1 guard: `gtb remove command`
// must not delete a command marked Protected in the manifest — the operator
// protected it precisely because it carries hand-written logic. --force is the
// only escape hatch.
func TestRemove_RefusesProtectedCommand(t *testing.T) {
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

// TestCleanupDocumentation_DiataxisNestedCommand is the 2.1.2 guard: removing a
// nested command must delete its Diátaxis reference doc (docs/reference/cli/
// <parent>/<leaf>.md), not the hardcoded legacy flat path (which leaves the
// real doc stranded).
func TestCleanupDocumentation_DiataxisNestedCommand(t *testing.T) {
	fs := afero.NewMemMapFs()
	workDir := "/work"

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "go.mod"), []byte("module github.com/acme/demo\n"), 0o644))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/parent/child"), 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/parent/child/cmd.go"), []byte("package child\n"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/parent/child/main.go"), []byte("package main\n"), 0o644))

	parentCode := `package parent
import (
	"github.com/acme/demo/pkg/cmd/parent/child"
	"github.com/spf13/cobra"
)
func NewCmdParent(props *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "parent"}
	cmd.AddCommand(child.NewCmdChild(props))
	return cmd
}`
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/parent/cmd.go"), []byte(parentCode), 0o644))

	// Diátaxis-layout project: nested command doc lives under reference/cli.
	docPath := filepath.Join(workDir, "docs/reference/cli/parent/child.md")
	require.NoError(t, afero.WriteFile(fs, docPath, []byte("# demo parent child\n"), 0o644))

	m := Manifest{
		Properties: ManifestProperties{Name: "demo", DocsLayout: DocsLayoutDiataxis},
		Commands: []ManifestCommand{
			{Name: "parent", Commands: []ManifestCommand{{Name: "child"}}},
		},
	}
	data, _ := yaml.Marshal(m)
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, ".gtb"), 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"), data, 0o644))

	p := &props.Props{FS: fs, Logger: logger.NewNoop(), Tool: props.Tool{Name: "demo"}}
	g := New(p, &Config{Path: workDir, Name: "child", Parent: "parent"})

	require.NoError(t, g.Remove(context.Background()))

	exists, _ := afero.Exists(fs, docPath)
	assert.False(t, exists, "nested command's Diátaxis reference doc must be removed")
}

// TestStagedFS_MoveAndDeleteMaterialise covers the staged-overlay move/delete
// semantics the atomic regenerate (2.2.1) relies on: a base-only file can be
// renamed (the flat->Diátaxis docs migration) and removed (dropping signing.go)
// through the buffer without EPERM, and materialise commits both to base.
func TestStagedFS_MoveAndDeleteMaterialise(t *testing.T) {
	t.Parallel()

	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/p/docs/commands/foo/index.md", []byte("# foo\n"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/p/pkg/cmd/root/signing.go", []byte("package root\n"), 0o644))

	s := newStagedFS(base)

	// Rename a base-only file (CoW would EPERM).
	require.NoError(t, s.Rename("/p/docs/commands/foo/index.md", "/p/docs/reference/cli/foo.md"))
	// Remove a base-only file (CoW would EPERM).
	require.NoError(t, s.Remove("/p/pkg/cmd/root/signing.go"))
	// A fresh write.
	require.NoError(t, afero.WriteFile(s, "/p/new.txt", []byte("new\n"), 0o644))

	// Base is untouched until materialise.
	moved, _ := afero.Exists(base, "/p/docs/reference/cli/foo.md")
	assert.False(t, moved, "base must not change before materialise")

	require.NoError(t, s.materialise())

	moved, _ = afero.Exists(base, "/p/docs/reference/cli/foo.md")
	assert.True(t, moved, "renamed file must land at the destination")
	old, _ := afero.Exists(base, "/p/docs/commands/foo/index.md")
	assert.False(t, old, "renamed file's source must be gone")
	sig, _ := afero.Exists(base, "/p/pkg/cmd/root/signing.go")
	assert.False(t, sig, "removed file must be gone")
	nw, _ := afero.Exists(base, "/p/new.txt")
	assert.True(t, nw, "fresh write must be committed")
}

// TestProvenance_LocalLocationWithSpace is the 2.2.2 guard: a local
// template-source location containing spaces (a legitimate filesystem path)
// must round-trip byte-exact through the provenance encode/decode, not be
// truncated at the first space.
func TestProvenance_LocalLocationWithSpace(t *testing.T) {
	t.Parallel()

	spaced := "/home/me/My Templates/gtb"

	original := ManifestProperties{
		Templates: []TemplateSource{
			{Name: "local", Type: TemplateSourceLocal, Location: spaced, Fingerprint: "abc123"},
		},
	}

	fs := afero.NewMemMapFs()
	g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: "/proj"})

	require.NoError(t, g.writeProvenanceFile(&Manifest{Properties: original}))

	var recovered ManifestProperties
	g.applyProvenanceFile(&recovered)

	require.Len(t, recovered.Templates, 1)
	assert.Equal(t, spaced, recovered.Templates[0].Location, "spaced local location must survive the round-trip")
}

// TestProvenance_DecodeLegacyUnencoded is the backward-compat guard for 2.2.2:
// provenance files written before values were percent-encoded carried token-like
// values verbatim, and must still decode unchanged.
func TestProvenance_DecodeLegacyUnencoded(t *testing.T) {
	t.Parallel()

	got := decodeKV("enabled=true external_key_email=sec@acme.example key_id=arn:aws:kms:eu-west-2:1:key/abc")
	assert.Equal(t, "true", got["enabled"])
	assert.Equal(t, "sec@acme.example", got["external_key_email"])
	assert.Equal(t, "arn:aws:kms:eu-west-2:1:key/abc", got["key_id"])
}

// TestValidateHost_PortBounds is the 2.4.4 guard: a numeric-but-out-of-range
// port must be rejected, not merely digit-checked.
func TestValidateHost_PortBounds(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateHost("example.com:8080"))
	require.NoError(t, ValidateHost("example.com:65535"))
	require.NoError(t, ValidateHost("example.com:1"))

	for _, bad := range []string{"example.com:99999", "example.com:0", "example.com:65536"} {
		err := ValidateHost(bad)
		require.Error(t, err, bad)
		require.ErrorIs(t, err, ErrInvalidInput, bad)
		assert.Contains(t, errors.FlattenHints(err), "1-65535", bad)
	}
}

// TestCheckManifestVersion_DevManifestMessage is the 2.4.4 guard: a `gtb: dev`
// manifest is refused with a message naming the dev-build condition, not the
// misleading "your gtb is older than the manifest" wording.
func TestCheckManifestVersion_DevManifestMessage(t *testing.T) {
	t.Parallel()

	p := &props.Props{Logger: logger.NewNoop(), Version: version.NewInfo("v1.2.3", "", "")}
	g := New(p, &Config{Path: "/proj"})

	err := g.checkManifestVersion(&Manifest{Version: ManifestVersion{GoToolBase: "dev"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "development build")
	assert.NotContains(t, err.Error(), "lower than")
}

// TestVerifyProject_CorruptManifestFails is the 2.2.3 guard: a present but
// undecodable manifest must fail verification with the decode error rather than
// silently passing.
func TestVerifyProject_CorruptManifestFails(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/proj/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/proj/.gtb/manifest.yaml", []byte("\tthis: : is not [valid yaml"), 0o644))

	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	g := New(p, &Config{Path: "/proj"})

	err := g.verifyProject()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal manifest")
}

// TestRegenerateProject_AtomicOnMidRunFailure is the 2.2.1 guard: a regenerate
// that aborts mid-run (here: an uncacheable git template source with no clone
// available) must leave the tree untouched, so already-written command files
// are not left with hashes the manifest never recorded (misclassified as
// user-modified on the next run).
func TestRegenerateProject_AtomicOnMidRunFailure(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop(), Version: version.NewInfo("v1.0.0", "", "")}

	workDir := "."
	require.NoError(t, afero.WriteFile(fs, "go.mod", []byte("module github.com/test/project\n\ngo 1.22\n"), 0o644))

	oldFoo := []byte("package foo\n// OLD HAND-WRITTEN CONTENT\n")
	require.NoError(t, fs.MkdirAll("pkg/cmd/foo", 0o755))
	require.NoError(t, afero.WriteFile(fs, "pkg/cmd/foo/cmd.go", oldFoo, 0o644))

	oldRoot := []byte("package root\n// OLD ROOT\n")
	require.NoError(t, fs.MkdirAll("pkg/cmd/root", 0o755))
	require.NoError(t, afero.WriteFile(fs, "pkg/cmd/root/cmd.go", oldRoot, 0o644))

	m := Manifest{
		Properties: ManifestProperties{
			Name: "test-project",
			// A git source with a pin but no warm cache and no clone func makes
			// resolveAllSources (in the skeleton step, after the command files
			// are written) fail deterministically.
			Templates: []TemplateSource{
				{Name: "corp", Type: TemplateSourceGit, Location: "acme/tpl", Ref: "v1", Resolved: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"},
			},
		},
		Version:  ManifestVersion{GoToolBase: "v1.0.0"},
		Commands: []ManifestCommand{{Name: "foo"}},
	}
	data, _ := yaml.Marshal(m)
	require.NoError(t, fs.MkdirAll(".gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, ".gtb/manifest.yaml", data, 0o644))

	g := New(p, &Config{Path: workDir, Name: "test-project"})

	err := g.RegenerateProject(context.Background())
	require.Error(t, err, "regeneration must fail on the uncacheable template source")

	gotFoo, _ := afero.ReadFile(fs, "pkg/cmd/foo/cmd.go")
	assert.Equal(t, string(oldFoo), string(gotFoo), "aborted regenerate must not mutate the command file")

	gotRoot, _ := afero.ReadFile(fs, "pkg/cmd/root/cmd.go")
	assert.Equal(t, string(oldRoot), string(gotRoot), "aborted regenerate must not mutate the root command file")
}
