package generator

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Issue #14: a command file gtb rewrites must end the run recorded with the
// hash of the bytes actually on disk. A command's cmd.go is hashed at render
// time, a later pass in the same run rewrote it, and nothing re-recorded it —
// so the manifest carried a hash the file never had, every later run raised a
// conflict on gtb's own output, and the project could not converge.
//
// refreshProjectFileHashes covered only Manifest.Hashes — the project-level
// skeleton map. A command's hash lives in ManifestCommand.Hashes["cmd.go"],
// which nothing re-recorded.
//
// These tests drive the post-processing writer through the runCommand seam
// rather than naming a real one. The pass that rewrote the file on keryx was
// never established and did not reproduce on a fresh scaffold, so pinning the
// test to a guessed mechanism would assert something unverified. What must
// hold either way is the property below: whatever wrote last, the run ends
// with the manifest recording the bytes on disk.

// renderedCmdGo is a stand-in for a generated registration file, in the shape
// the reported divergence took: a statement, then `return nil` with no blank
// line between them.
const renderedCmdGo = `package pin

func NewCmdPin() error {
	props.ErrorHandler.SetUsage(cmd.Usage)
	return nil
}
`

// reformatted is renderedCmdGo after a later pass has rewritten it — a
// whitespace-only edit, matching the shape of every divergence measured on
// keryx.
var reformatted = strings.Replace(
	renderedCmdGo,
	"SetUsage(cmd.Usage)\n\treturn nil",
	"SetUsage(cmd.Usage)\n\n\treturn nil",
	1,
)

// commandHashFixture lays out a project on a real filesystem with one nested
// command whose cmd.go is recorded at its as-rendered hash. It returns the
// generator and the command's directory.
//
// A real OsFs is required: runPostRegenerationProcessing returns early for any
// other filesystem, so a MemMapFs test would pass without exercising the path
// that breaks.
func commandHashFixture(t *testing.T) (*Generator, string) {
	t.Helper()

	root := t.TempDir()
	fs := afero.NewOsFs()

	cmdDir := filepath.Join(root, "pkg", "cmd", "theme", "pin")
	require.NoError(t, fs.MkdirAll(cmdDir, DefaultDirMode))
	require.NoError(t, fs.MkdirAll(filepath.Join(root, ".gtb"), DefaultDirMode))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(cmdDir, "cmd.go"), []byte(renderedCmdGo), DefaultFileMode))

	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	g := New(p, &Config{Path: root})

	m := &Manifest{
		Properties: ManifestProperties{Name: "keryx"},
		Commands: []ManifestCommand{{
			Name: "theme",
			Commands: []ManifestCommand{{
				Name:   "pin",
				Hashes: map[string]string{"cmd.go": calculateHash([]byte(renderedCmdGo))},
			}},
		}},
	}
	require.NoError(t, g.marshalManifestFile(ManifestPathFor(root), m))

	return g, cmdDir
}

// recordedCommandHash reads back the hash the manifest carries for theme/pin.
func recordedCommandHash(t *testing.T, g *Generator) string {
	t.Helper()

	m, err := g.decodeManifestFile(ManifestPathFor(g.config.Path))
	require.NoError(t, err)

	cmd := walkCommandPath(m.Commands, []string{"theme", "pin"})
	require.NotNil(t, cmd)

	return cmd.Hashes["cmd.go"]
}

func TestRunPostRegenerationProcessing_RecordsTheHashOfWhatLandedOnDisk(t *testing.T) {
	t.Parallel()

	g, cmdDir := commandHashFixture(t)
	cmdGo := filepath.Join(cmdDir, "cmd.go")

	// Stand in for whichever post-processing pass rewrites the file after the
	// render-time hash has already been recorded.
	g.runCommand = func(_ context.Context, _, name string, _ ...string) ([]byte, error) {
		if name == "golangci-lint" {
			require.NoError(t, afero.WriteFile(g.props.FS, cmdGo, []byte(reformatted), DefaultFileMode))
		}

		return nil, nil
	}

	g.runPostRegenerationProcessing(context.Background(), map[string]string{})

	onDisk, err := afero.ReadFile(g.props.FS, cmdGo)
	require.NoError(t, err)
	require.Equal(t, reformatted, string(onDisk), "fixture should have applied the post-processing edit")

	assert.Equal(t, calculateHash(onDisk), recordedCommandHash(t, g),
		"the manifest must record the hash of the bytes on disk, or the project can never converge")
}

func TestRunPostRegenerationProcessing_LeavesAKeptCommandFileAlone(t *testing.T) {
	t.Parallel()

	g, cmdDir := commandHashFixture(t)
	cmdGo := filepath.Join(cmdDir, "cmd.go")

	// The developer declined the overwrite, so their edit stands on disk and
	// the stored hash must NOT adopt it — re-hashing here would forget the
	// divergence and overwrite it unprompted on the next run (0187 D3).
	stored := recordedCommandHash(t, g)
	require.NoError(t, afero.WriteFile(g.props.FS, cmdGo, []byte("package pin // hand-edited\n"), DefaultFileMode))
	g.conflicts.recordKeep(g.relProjectPath(cmdGo), keepReasonDeclined)

	g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) { return nil, nil }
	g.runPostRegenerationProcessing(context.Background(), map[string]string{})

	assert.Equal(t, stored, recordedCommandHash(t, g),
		"a kept file keeps its stored hash so the conflict is raised again next run")
}
