package props

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/features"
)

// TestDeclareLinksOn declares one link-kind feature per name under the
// prefix, on whatever it is enabled: a link's presence in the binary is its
// enablement (#81), so it defaults on and carries no generated-code constant.
func TestDeclareLinksOn(t *testing.T) {
	t.Parallel()

	r := features.NewRegistry()
	require.NoError(t, DeclareLinksOn(r, ChatLinkPrefix, "claude", "codex-local"))
	require.NoError(t, DeclareLinksOn(r, ForgeLinkPrefix, "github"))

	set, err := features.Resolve(r.Snapshot(), nil)
	require.NoError(t, err)

	assert.True(t, set.Enabled("chat-claude"))
	assert.True(t, set.Enabled("chat-codex-local"))
	assert.True(t, set.Enabled("forge-github"))
	assert.False(t, set.Enabled("chat-openai"))

	d, ok := r.Snapshot().Lookup("chat-claude")
	require.True(t, ok)
	assert.Equal(t, KindLink, d.FeatureKind())
	assert.True(t, d.DefaultOn())

	assert.Equal(t, []string{"claude", "codex-local"}, LinkedNames(set, ChatLinkPrefix))
	assert.Equal(t, []string{"github"}, LinkedNames(set, ForgeLinkPrefix))
	assert.Empty(t, LinkedNames(set, "other-"))

	require.ErrorIs(t, DeclareLinksOn(r, ChatLinkPrefix, "claude"), ErrDuplicateFeature)
}

// TestValidateDescriptor_LinkMayDefaultOnWithoutAConstant: the plugin rule
// (a blank import changes what is available, never what is on) does not apply
// to a link, whose import is precisely what turns it on; and a link the
// generator wrote has no constant to name.
func TestValidateDescriptor_LinkMayDefaultOnWithoutAConstant(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateDescriptor(FeatureDescriptor{ID: "chat-claude", Kind: KindLink, Default: true}, nil))
	require.ErrorIs(t, validateDescriptor(FeatureDescriptor{ID: "x", Kind: KindForge, Default: true, ConstName: "X", ConstPackage: "p"}, nil), ErrPluginDefaultOn)
	require.ErrorIs(t, validateDescriptor(FeatureDescriptor{ID: "x", Kind: KindForge}, nil), ErrInvalidDescriptor, "a non-link still needs its constant")
}
