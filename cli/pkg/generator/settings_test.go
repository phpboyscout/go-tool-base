package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/errors"
)

const settingsManifest = "properties:\n  name: mytool\n  features:\n    - name: ai\n      enabled: true\n" +
	"  chat:\n    providers: [claude, openai]\n    default:\n      provider: claude\n" +
	"release_source:\n  type: github\n  backend: github\n  host: github.com\n  owner: org\n  repo: mytool\n" +
	"version:\n  gtb: v1.0.0\n  go: \"1.26\"\ncommands: []\n"

// TestSetSetting pins spec 0197 D6: one setter over the table's manifest
// column, validated like the flags, syncing the derived files, and refusing
// what the table does not name.
func TestSetSetting(t *testing.T) {
	t.Parallel()

	t.Run("a scalar under properties, with or without the prefix", func(t *testing.T) {
		t.Parallel()

		g, _, _ := newPerimeterTestProject(t, settingsManifest)
		require.NoError(t, g.SetSetting(context.Background(), "chat.default.provider", []string{"openai"}))

		m, err := g.loadManifest()
		require.NoError(t, err)
		assert.Equal(t, "openai", m.Properties.Chat.Default.Provider)

		got, err := g.GetSetting("properties.chat.default.provider")
		require.NoError(t, err)
		assert.Equal(t, []string{"openai"}, got)
	})

	t.Run("the sync follows the write", func(t *testing.T) {
		t.Parallel()

		g, fs, _ := newPerimeterTestProject(t, settingsManifest)
		require.NoError(t, g.SetSetting(context.Background(), "chat.default.model", []string{"gpt-x"}))

		bundle, err := afero.ReadFile(fs, "/work/cmd/mytool/chat/assets/config.yaml")
		require.NoError(t, err, "the chat defaults bundle is re-rendered by the same command")
		assert.Contains(t, string(bundle), "gpt-x")
	})

	t.Run("a list and a bool", func(t *testing.T) {
		t.Parallel()

		g, _, _ := newPerimeterTestProject(t, settingsManifest)
		require.NoError(t, g.SetSetting(context.Background(), "bootstrap.skip_config_check", []string{"version", "doctor"}))
		require.NoError(t, g.SetSetting(context.Background(), "bootstrap.auto_initialise", []string{"true"}))
		require.NoError(t, g.SetSetting(context.Background(), "version.go", []string{"1.27"}))

		m, err := g.loadManifest()
		require.NoError(t, err)
		assert.Equal(t, []string{"version", "doctor"}, m.Properties.Bootstrap.SkipConfigCheck)
		assert.True(t, m.Properties.Bootstrap.AutoInitialise)
		assert.Equal(t, "1.27", m.Version.Go)

		err = g.SetSetting(context.Background(), "bootstrap.auto_initialise", []string{"maybe"})
		require.Error(t, err, "a bool takes true or false")
	})

	t.Run("validated like the flags, and nothing written on refusal", func(t *testing.T) {
		t.Parallel()

		g, _, _ := newPerimeterTestProject(t, settingsManifest)
		err := g.SetSetting(context.Background(), "chat.default.provider", []string{"gemini"})
		require.ErrorIs(t, err, ErrChatDefaultNotLinked)

		m, err := g.loadManifest()
		require.NoError(t, err)
		assert.Equal(t, "claude", m.Properties.Chat.Default.Provider, "the manifest is untouched")
	})

	t.Run("unknown and read-only paths are refused with the list", func(t *testing.T) {
		t.Parallel()

		g, _, _ := newPerimeterTestProject(t, settingsManifest)

		err := g.SetSetting(context.Background(), "bogus.path", []string{"x"})
		require.ErrorIs(t, err, ErrUnknownSetting)
		assert.Contains(t, errors.FlattenHints(err), "chat.default.provider")

		err = g.SetSetting(context.Background(), "hashes", []string{"x"})
		require.ErrorIs(t, err, ErrSettingReadOnly)

		err = g.SetSetting(context.Background(), "properties.features", []string{"ai"})
		require.ErrorIs(t, err, ErrSettingReadOnly, "features have enable and disable")
		assert.Contains(t, errors.FlattenHints(err), "enable")
	})

	t.Run("unset zeroes the field", func(t *testing.T) {
		t.Parallel()

		g, _, _ := newPerimeterTestProject(t, settingsManifest)
		require.NoError(t, g.SetSetting(context.Background(), "chat.default.model", []string{"gpt-x"}))
		require.NoError(t, g.UnsetSetting(context.Background(), "chat.default.model"))

		got, err := g.GetSetting("chat.default.model")
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("the repository is one value, owner and repo", func(t *testing.T) {
		t.Parallel()

		g, _, _ := newPerimeterTestProject(t, settingsManifest)
		require.NoError(t, g.SetSetting(context.Background(), "release_source.repo", []string{"neworg/newname"}))

		m, err := g.loadManifest()
		require.NoError(t, err)
		assert.Equal(t, "neworg", m.ReleaseSource.Owner)
		assert.Equal(t, "newname", m.ReleaseSource.Repo)
	})
}

// TestAuthorSettingPaths_MatchTheManifestSchema: every settable path in the
// table resolves to a real field through the manifest's yaml tags, so the
// table cannot name a path the setter cannot reach.
func TestAuthorSettingPaths_MatchTheManifestSchema(t *testing.T) {
	t.Parallel()

	var m Manifest

	for _, s := range AuthorSettings() {
		if s.Kind != KindSetting || settingReadOnlyReason(s.Manifest) != "" {
			continue
		}

		_, err := manifestLeaf(&m, s.Manifest)
		require.NoErrorf(t, err, "%s: path %q does not resolve", s.Field, s.Manifest)
	}
}
