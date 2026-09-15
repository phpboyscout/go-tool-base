package generator

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestManifestChat_ExplicitEmptyListSurvivesMarshal pins #45: a manifest can
// say "ai enabled, no providers" and keep saying it. omitempty dropped the
// empty list and then the block, and the next regenerate read the absent
// block as a pre-0194 manifest and wrote all ten providers.
func TestManifestChat_ExplicitEmptyListSurvivesMarshal(t *testing.T) {
	t.Parallel()

	none := ManifestProperties{Name: "tool", Chat: ManifestChat{Providers: []string{}}}
	out, err := yaml.Marshal(none)
	require.NoError(t, err)
	assert.Contains(t, string(out), "providers: []", "an explicit empty list is written out")

	var back ManifestProperties
	require.NoError(t, yaml.Unmarshal(out, &back))
	require.NotNil(t, back.Chat.Providers, "an explicit empty list reads back as empty, not absent")
	assert.Empty(t, back.Chat.Providers)

	unset := ManifestProperties{Name: "tool"}
	out, err = yaml.Marshal(unset)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "chat:", "an absent block stays absent (the pre-0194 shape)")
}

// TestSyncAdapterFiles_HonoursExplicitNone drives the two regenerates that
// used to turn "none" into "everything": the manifest keeps its empty list
// and chat.go links no provider module after both.
func TestSyncAdapterFiles_HonoursExplicitNone(t *testing.T) {
	t.Parallel()

	manifest := "properties:\n  name: mytool\n  features:\n    - name: ai\n      enabled: true\n" +
		"  chat:\n    providers: []\nversion:\n  gtb: v1.0.0\ncommands: []\n"

	g, fs, _ := newPerimeterTestProject(t, manifest)

	for run := 1; run <= 2; run++ {
		m, err := g.loadManifest()
		require.NoError(t, err)
		require.NoError(t, g.syncAdapterFiles(m), "run %d", run)
		// regenerate writes the manifest back after syncing (hashes, sources).
		require.NoError(t, g.marshalManifestFile(ManifestPathFor(g.config.Path), m))

		raw, err := afero.ReadFile(fs, "/work/.gtb/manifest.yaml")
		require.NoError(t, err)
		assert.Containsf(t, string(raw), "providers: []", "run %d: the manifest must keep the explicit empty list", run)
		assert.NotContainsf(t, string(raw), "- claude", "run %d: the default providers must not be written", run)

		chatGo, err := afero.ReadFile(fs, "/work/cmd/mytool/chat.go")
		require.NoError(t, err)
		assert.NotContainsf(t, string(chatGo), "go/chat-", "run %d: chat.go must link no provider module", run)
	}
}

// TestValidateManifest_RefusesUnknownChatProvider pins that regenerate applies
// the same provider validation as generate: a name no module registers is
// refused, not silently dropped from the imports.
func TestValidateManifest_RefusesUnknownChatProvider(t *testing.T) {
	t.Parallel()

	m := &Manifest{Properties: ManifestProperties{
		Name:     "tool",
		Features: []ManifestFeature{{Name: "ai", Enabled: true}},
		Chat:     ManifestChat{Providers: []string{"claude", "not-a-provider"}},
	}}
	err := ValidateManifest(m)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownChatProvider)
}
