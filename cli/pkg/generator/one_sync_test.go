package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSyncAdapterFiles_KeychainFollowsTheManifest pins spec 0197 D8: the
// keychain import is a synced adapter file like chat.go, written while the
// feature is enabled and removed when it is not. Deleting it by hand lasts
// until the next sync; `disable keychain` is the durable way.
func TestSyncAdapterFiles_KeychainFollowsTheManifest(t *testing.T) {
	t.Parallel()

	g, fs := newPureGenerator(t, &Config{Path: "/proj"})

	// The manifest records keychain as an explicit entry when selected
	// (it has no catalogue default), so absence means off.
	enabled := &Manifest{Properties: ManifestProperties{Name: "tool",
		Features: []ManifestFeature{{Name: KeychainFeature, Enabled: true}}}}
	require.NoError(t, g.syncAdapterFiles(enabled))

	src, err := afero.ReadFile(fs, "/proj/cmd/tool/keychain.go")
	require.NoError(t, err, "the file is written while the feature is enabled")
	assert.Contains(t, string(src), "pkg/setup/keychain")

	disabled := &Manifest{Properties: ManifestProperties{Name: "tool"}}
	require.NoError(t, g.syncAdapterFiles(disabled))

	exists, _ := afero.Exists(fs, "/proj/cmd/tool/keychain.go")
	assert.False(t, exists, "disabling the feature removes the file")

	require.NoError(t, g.syncAdapterFiles(enabled))
	exists, _ = afero.Exists(fs, "/proj/cmd/tool/keychain.go")
	assert.True(t, exists, "a hand-deleted file comes back while the feature is enabled")
}

// TestApplyFeatures_LeavesTheTreeInLine pins spec 0197 D7: enable ai alone
// records the default providers and writes chat.go with their modules; no
// regenerate is needed afterwards. It used to re-render the root and stop.
func TestApplyFeatures_LeavesTheTreeInLine(t *testing.T) {
	t.Parallel()

	manifest := "properties:\n  name: mytool\n  features: []\nversion:\n  gtb: v1.0.0\ncommands: []\n"
	g, fs, _ := newPerimeterTestProject(t, manifest)

	changed, err := g.ApplyFeatures(context.Background(), map[string]bool{"ai": true})
	require.NoError(t, err)
	assert.Equal(t, []string{"ai"}, changed)

	chatGo, err := afero.ReadFile(fs, "/work/cmd/mytool/chat.go")
	require.NoError(t, err, "the adapter file is written by the same command")
	assert.Contains(t, string(chatGo), "chat-anthropic")

	m, err := g.loadManifest()
	require.NoError(t, err)
	assert.Equal(t, DefaultChatProviders(), m.Properties.Chat.Providers, "the derived provider list is recorded")
	assert.True(t, featureEnabledIn(m.Properties.Features, "ai"))
}
