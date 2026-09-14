package generator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spf13/afero"
)

// mutateManifest loads the scaffolded project's manifest, applies mut, and
// writes it back through the canonical encoder — the state the CLI attach path
// will produce.
func mutateManifest(t *testing.T, fs afero.Fs, mut func(*Manifest)) {
	t.Helper()

	path := ManifestPathFor("/work")
	m, err := DecodeManifestFile(fs, path)
	require.NoError(t, err)

	mut(m)

	require.NoError(t, EncodeManifestFile(fs, path, m))
}

// TestRegenerate_RendersDeclarativeExternalCommands proves a manifest-declared
// external attachment is rendered into the generated root by the real regenerate
// path, and survives a second regenerate — the core regeneration-safety
// guarantee at the manifest level (the Attach API layer is exercised in Phase 3).
func TestRegenerate_RendersDeclarativeExternalCommands(t *testing.T) {
	t.Parallel()

	g, fs := newFeatureProject(t)

	mutateManifest(t, fs, func(m *Manifest) {
		m.Properties.ExternalCommands = []ManifestExternalCommand{
			{
				Module:  "gitlab.com/phpboyscout/go/signing-cli",
				Version: "v0.1.0",
				Attach: []ManifestExternalAttach{
					{Constructor: "NewCmdSign", Args: []string{"logger"}, Wrap: true},
					{Constructor: "NewCmdKeys", Args: []string{"logger"}, Wrap: true},
				},
			},
		}
	})

	require.NoError(t, g.RegenerateProject(context.Background()))

	out := readRootCmd(t, fs)
	assert.Contains(t, out, `setup.Wrap("", signingcli.NewCmdSign(p.GetLogger()))`)
	assert.Contains(t, out, `setup.Wrap("", signingcli.NewCmdKeys(p.GetLogger()))`)
	assert.Contains(t, out, "gitlab.com/phpboyscout/go/signing-cli")

	// The wiring survives a further regenerate (it is manifest-driven, re-rendered
	// every time — never dropped like a hand-edit would be).
	require.NoError(t, g.RegenerateProject(context.Background()))
	assert.Contains(t, readRootCmd(t, fs), "signingcli.NewCmdSign(p.GetLogger())")
}

// TestRegenerate_RendersAdapterChannel proves the adapter flag renders the
// external.Commands(p) spread (via append, since a spread cannot mix with
// individual NewCmdRoot args).
func TestRegenerate_RendersAdapterChannel(t *testing.T) {
	t.Parallel()

	g, fs := newFeatureProject(t)

	mutateManifest(t, fs, func(m *Manifest) {
		m.Properties.ExternalCommandsAdapter = true
	})

	require.NoError(t, g.RegenerateProject(context.Background()))

	out := readRootCmd(t, fs)
	assert.Contains(t, out, "external.Commands(p)")
	assert.Contains(t, out, "append(")
	assert.Contains(t, out, "github.com/acme/feat-tool/pkg/cmd/external")
}

// TestRegenerate_ExternalProvenanceReconstructs proves the external_commands
// block is recorded in the provenance file during regenerate, so a from-scratch
// manifest reconstruction recovers it.
func TestRegenerate_ExternalProvenanceReconstructs(t *testing.T) {
	t.Parallel()

	g, fs := newFeatureProject(t)

	mutateManifest(t, fs, func(m *Manifest) {
		m.Properties.ExternalCommands = []ManifestExternalCommand{
			{
				Module:  "gitlab.com/phpboyscout/go/signing-cli",
				Version: "v0.1.0",
				Attach:  []ManifestExternalAttach{{Constructor: "NewCmdSign", Args: []string{"logger"}, Wrap: true}},
			},
		}
	})

	require.NoError(t, g.RegenerateProject(context.Background()))

	// The provenance file beside the root records the not-in-source block.
	content, err := afero.ReadFile(fs, provenancePath("/work"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "// gtb:external ")

	// And it recovers into a from-scratch manifest.
	var recovered ManifestProperties
	g.applyProvenanceFile(&recovered)
	require.Len(t, recovered.ExternalCommands, 1)
	assert.Equal(t, "gitlab.com/phpboyscout/go/signing-cli", recovered.ExternalCommands[0].Module)
	require.Len(t, recovered.ExternalCommands[0].Attach, 1)
	assert.Equal(t, "NewCmdSign", recovered.ExternalCommands[0].Attach[0].Constructor)
}
