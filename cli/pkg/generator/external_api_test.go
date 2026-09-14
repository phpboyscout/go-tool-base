package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sigillumSpec() ExternalCommandSpec {
	return ExternalCommandSpec{
		Module:  "gitlab.com/phpboyscout/go/signing-cli",
		Version: "v0.1.0",
		Attach: []ManifestExternalAttach{
			{Constructor: "NewCmdSign", Args: []string{"logger"}, Wrap: true},
			{Constructor: "NewCmdKeys", Args: []string{"logger"}, Wrap: true},
		},
	}
}

func readWorkManifest(t *testing.T, fs afero.Fs) *Manifest {
	t.Helper()

	m, err := DecodeManifestFile(fs, ManifestPathFor("/work"))
	require.NoError(t, err)

	return m
}

func TestAttachExternalCommand(t *testing.T) {
	t.Parallel()

	g, fs := newFeatureProject(t)

	require.NoError(t, g.AttachExternalCommand(context.Background(), sigillumSpec()))

	m := readWorkManifest(t, fs)
	require.Len(t, m.Properties.ExternalCommands, 1)
	assert.Equal(t, "gitlab.com/phpboyscout/go/signing-cli", m.Properties.ExternalCommands[0].Module)

	out := readRootCmd(t, fs)
	assert.Contains(t, out, `setup.Wrap("", signingcli.NewCmdSign(p.GetLogger()))`)
	assert.Contains(t, out, `setup.Wrap("", signingcli.NewCmdKeys(p.GetLogger()))`)
}

// TestAttachExternalCommand_AppendsToSameModule proves a second attach for the
// same module (same version) appends another constructor rather than erroring —
// the CLI attaches one constructor per invocation.
func TestAttachExternalCommand_AppendsToSameModule(t *testing.T) {
	t.Parallel()

	g, fs := newFeatureProject(t)

	one := ExternalCommandSpec{
		Module:  "gitlab.com/phpboyscout/go/signing-cli",
		Version: "v0.1.0",
		Attach:  []ManifestExternalAttach{{Constructor: "NewCmdSign", Args: []string{"logger"}, Wrap: true}},
	}
	two := ExternalCommandSpec{
		Module:  "gitlab.com/phpboyscout/go/signing-cli",
		Version: "v0.1.0",
		Attach:  []ManifestExternalAttach{{Constructor: "NewCmdKeys", Args: []string{"logger"}, Wrap: true}},
	}

	require.NoError(t, g.AttachExternalCommand(context.Background(), one))
	require.NoError(t, g.AttachExternalCommand(context.Background(), two))

	m := readWorkManifest(t, fs)
	require.Len(t, m.Properties.ExternalCommands, 1)
	require.Len(t, m.Properties.ExternalCommands[0].Attach, 2)

	out := readRootCmd(t, fs)
	assert.Contains(t, out, "signingcli.NewCmdSign(p.GetLogger())")
	assert.Contains(t, out, "signingcli.NewCmdKeys(p.GetLogger())")
}

// TestAttachExternalCommand_DuplicateConstructorRejected proves re-attaching the
// same (module, constructor) is rejected by the cross-entry validation.
func TestAttachExternalCommand_DuplicateConstructorRejected(t *testing.T) {
	t.Parallel()

	g, _ := newFeatureProject(t)

	require.NoError(t, g.AttachExternalCommand(context.Background(), sigillumSpec()))

	err := g.AttachExternalCommand(context.Background(), sigillumSpec())
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidInput)
}

// TestAttachExternalCommand_VersionMismatchRejected proves attaching the same
// module at a different version is rejected (detach first to re-pin).
func TestAttachExternalCommand_VersionMismatchRejected(t *testing.T) {
	t.Parallel()

	g, _ := newFeatureProject(t)

	require.NoError(t, g.AttachExternalCommand(context.Background(), sigillumSpec()))

	other := sigillumSpec()
	other.Version = "v0.2.0"
	other.Attach = []ManifestExternalAttach{{Constructor: "NewCmdOther", Args: []string{"logger"}, Wrap: true}}

	err := g.AttachExternalCommand(context.Background(), other)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidInput)
}

func TestAttachExternalCommand_InvalidRejectedBeforeWrite(t *testing.T) {
	t.Parallel()

	g, fs := newFeatureProject(t)

	bad := sigillumSpec()
	bad.Version = "" // required pin missing

	require.Error(t, g.AttachExternalCommand(context.Background(), bad))
	// Nothing was written.
	assert.Empty(t, readWorkManifest(t, fs).Properties.ExternalCommands)
	assert.NotContains(t, readRootCmd(t, fs), "signingcli")
}

func TestDetachExternalCommand(t *testing.T) {
	t.Parallel()

	g, fs := newFeatureProject(t)

	require.NoError(t, g.AttachExternalCommand(context.Background(), sigillumSpec()))
	require.Contains(t, readRootCmd(t, fs), "signingcli.NewCmdSign")

	require.NoError(t, g.DetachExternalCommand(context.Background(), "gitlab.com/phpboyscout/go/signing-cli"))

	assert.Empty(t, readWorkManifest(t, fs).Properties.ExternalCommands)
	out := readRootCmd(t, fs)
	assert.NotContains(t, out, "signingcli")
	assert.NotContains(t, out, "setup.Wrap")
}

func TestDetachExternalCommand_UnknownModule(t *testing.T) {
	t.Parallel()

	g, _ := newFeatureProject(t)

	err := g.DetachExternalCommand(context.Background(), "example.com/not-attached")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidInput)
}

func TestAttachExternalAdapter(t *testing.T) {
	t.Parallel()

	g, fs := newFeatureProject(t)

	require.NoError(t, g.AttachExternalAdapter(context.Background()))

	// Seed file scaffolded, manifest flag set, root spreads external.Commands(p).
	adapterPath := "/work/pkg/cmd/external/attach.go"
	exists, _ := afero.Exists(fs, adapterPath)
	require.True(t, exists)
	assert.True(t, readWorkManifest(t, fs).Properties.ExternalCommandsAdapter)

	out := readRootCmd(t, fs)
	assert.Contains(t, out, "external.Commands(p)")
	assert.Contains(t, out, "append(")

	// Re-attaching preserves an author-edited adapter (preserve-if-exists).
	custom := []byte("package external\n\n// edited by hand\n")
	require.NoError(t, afero.WriteFile(fs, adapterPath, custom, 0o644))
	require.NoError(t, g.AttachExternalAdapter(context.Background()))

	after, err := afero.ReadFile(fs, adapterPath)
	require.NoError(t, err)
	assert.Equal(t, custom, after, "an existing adapter must never be overwritten")
}

// TestAttachExternalCommand_SurvivesEnableSigning is the headline regeneration-
// safety guarantee: a declarative attachment survives `enable signing`
// re-rendering the root. This is exactly the clobber that broke sigillum's
// hand-edited main.go (issue #4 class) and motivated the whole feature.
func TestAttachExternalCommand_SurvivesEnableSigning(t *testing.T) {
	t.Parallel()

	g, fs := newFeatureProject(t)

	require.NoError(t, g.AttachExternalCommand(context.Background(), sigillumSpec()))
	require.Contains(t, readRootCmd(t, fs), "signingcli.NewCmdSign(p.GetLogger())")

	// enable signing re-renders the root (adds the Signing: field) — the exact
	// operation that used to drop a hand-wired external command.
	require.NoError(t, g.EnableSigning(context.Background(), ManifestSigning{
		ExternalKeyEmail: "release@example.test",
		KeySource:        "both",
	}))

	out := readRootCmd(t, fs)
	assert.Contains(t, out, "signingcli.NewCmdSign(p.GetLogger())",
		"enable signing must NOT drop the external attachment")
	assert.Contains(t, out, "signingcli.NewCmdKeys(p.GetLogger())")
	assert.Contains(t, out, "Signing:", "enable signing still adds the Signing field")
}

// TestAttachExternalCommand_SurvivesRegenerate proves the attachment survives a
// full RegenerateProject.
func TestAttachExternalCommand_SurvivesRegenerate(t *testing.T) {
	t.Parallel()

	g, fs := newFeatureProject(t)

	require.NoError(t, g.AttachExternalCommand(context.Background(), sigillumSpec()))
	require.NoError(t, g.RegenerateProject(context.Background()))

	assert.Contains(t, readRootCmd(t, fs), "signingcli.NewCmdSign(p.GetLogger())")
}
