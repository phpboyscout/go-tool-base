package generator

import (
	"context"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// The framework files carry no hash and are rewritten whatever --overwrite
// says, so a deny run replaced an edited root with nothing on the console
// (#259). A run that wrote what was already there stays quiet.
func TestRegenerateProject_SaysWhichFrameworkFilesItRewrote(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, fs, _ := newIssue13Project(t, "deny", "")
	require.NoError(t, g.RegenerateProject(context.Background()))

	buf := regenerateAgain(t, g)
	assert.False(t, buf.ContainsLevel(logger.WarnLevel, "rewrote a framework file"),
		"a run that changed nothing reports nothing, got: %v", buf.Messages())

	root, err := afero.ReadFile(fs, "/work/pkg/cmd/root/cmd.go")
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/root/cmd.go", append(root, "// hand edit\n"...), 0o644))

	buf = regenerateAgain(t, g)
	assert.True(t, buf.ContainsLevel(logger.WarnLevel, "rewrote a framework file"), "got: %v", buf.Messages())
	assert.True(t, buf.ContainsLevel(logger.InfoLevel, "1 framework file rewritten"), "got: %v", buf.Messages())
}

// enable signing rewrites the root to wire the Signing field. That is the
// command doing its job, not an edit lost, and following the note's advice to
// ignore the root would leave the tool unable to verify a signature.
func TestEnableSigning_DoesNotReportTheRootRewrite(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, fs, _ := newIssue13Project(t, "deny", "")

	m, err := g.loadManifest()
	require.NoError(t, err)

	m.Properties.ModulePath = "test-mod"
	require.NoError(t, g.writeManifest(m))

	require.NoError(t, g.RegenerateProject(context.Background()))

	before, err := afero.ReadFile(fs, "/work/pkg/cmd/root/cmd.go")
	require.NoError(t, err)

	buf := logger.NewBuffer()
	g.props.Logger = buf
	g.conflicts.reset()

	require.NoError(t, g.EnableSigning(context.Background(), ManifestSigning{}))
	assert.False(t, buf.ContainsLevel(logger.WarnLevel, "rewrote a framework file"), "got: %v", buf.Messages())

	after, err := afero.ReadFile(fs, "/work/pkg/cmd/root/cmd.go")
	require.NoError(t, err)
	assert.NotEqual(t, string(before), string(after), "the root gained its signing wiring")
}

// A framework file the run cannot read is not one it can compare, so it fails
// rather than writing over content it never saw.
func TestRegenerateProject_FailsOnAFrameworkFileItCannotRead(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, fs, _ := newIssue13Project(t, "deny", "")
	require.NoError(t, g.RegenerateProject(context.Background()))

	g.props.FS = unreadableFile{Fs: fs, path: "/work/internal/version/version.go"}
	g.conflicts.reset()

	err := g.RegenerateProject(context.Background())
	require.ErrorContains(t, err, "failed to read internal/version/version.go")
}

// unreadableFile fails every open of one path for reading, and leaves writes
// alone, so a test can tell a read check from the create that follows it.
type unreadableFile struct {
	afero.Fs
	path string
}

func (u unreadableFile) Open(name string) (afero.File, error) {
	if name == u.path {
		return nil, os.ErrPermission
	}

	return u.Fs.Open(name)
}
