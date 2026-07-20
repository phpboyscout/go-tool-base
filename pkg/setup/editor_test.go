package setup

import (
	"testing"
	"testing/fstest"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func editorProps(t *testing.T) *props.Props {
	t.Helper()

	return &props.Props{
		Tool:   props.Tool{Name: "edtool"},
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Assets: props.NewAssets(),
	}
}

// TestStoreEditor_SetAppliesToFile drives the real store-backed editor: a Set
// lands in the target document immediately and the next view resolves it.
func TestStoreEditor_SetAppliesToFile(t *testing.T) {
	t.Parallel()

	p := editorProps(t)

	editor, path, err := OpenConfigEditor(t.Context(), p, "/home/user/.edtool", false)
	require.NoError(t, err)

	require.NoError(t, editor.Set("feature.key", "value"))
	assert.Equal(t, "value", editor.View().GetString("feature.key"))

	data, err := afero.ReadFile(p.FS, path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "key: value")
}

// TestClearCredentialKeysExcept_BlanksOthers covers the editor-backed clear:
// every listed key except the kept one is blanked to "", and the kept key
// survives untouched.
func TestClearCredentialKeysExcept_BlanksOthers(t *testing.T) {
	t.Parallel()

	p := editorProps(t)

	editor, _, err := OpenConfigEditor(t.Context(), p, "/home/user/.edtool", false)
	require.NoError(t, err)

	require.NoError(t, editor.Set("cred.env", "TOKEN"))
	require.NoError(t, editor.Set("cred.value", "literal"))
	require.NoError(t, editor.Set("cred.keychain", "svc/acct"))

	all := []string{"cred.env", "cred.value", "cred.keychain"}
	require.NoError(t, ClearCredentialKeysExcept(editor, all, "cred.env"))

	view := editor.View()
	assert.Equal(t, "TOKEN", view.GetString("cred.env"), "the kept key must survive")
	assert.Empty(t, view.GetString("cred.value"))
	assert.Empty(t, view.GetString("cred.keychain"))
}

// failingEditor errors on every Set, driving the first-error capture in the
// KeyWriter adapter.
type failingEditor struct{ Editor }

func (failingEditor) Set(string, any) error { return errors.New("write refused") }

func TestClearCredentialKeysExcept_SurfacesWriteError(t *testing.T) {
	t.Parallel()

	err := ClearCredentialKeysExcept(failingEditor{}, []string{"a", "b"}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write refused")
}

// TestRegisterAssets_RoundTrip covers the feature-asset registry slot: a
// registered bundle comes back from GetAssets under its feature, and the
// snapshot is a copy of the live map.
func TestRegisterAssets_RoundTrip(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	bundle := fstest.MapFS{"assets/config.yaml": &fstest.MapFile{Data: []byte("x: 1\n")}}
	RegisterAssets("examplefeature", "example", bundle)

	got := GetAssets()
	require.Len(t, got["examplefeature"], 1)
	assert.Equal(t, "example", got["examplefeature"][0].Name)
	assert.Equal(t, bundle, got["examplefeature"][0].Bundle)
}

func TestRegisterAssets_PanicsWhenSealed(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	SealRegistry()

	assert.Panics(t, func() {
		RegisterAssets("late", "late", fstest.MapFS{})
	})
}

// TestMergeExistingOverTemplate covers the re-init merge branches directly.
func TestMergeExistingOverTemplate(t *testing.T) {
	t.Parallel()

	t.Run("no template yields nil", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		out, err := mergeExistingOverTemplate(fs, "config.yaml", nil)
		require.NoError(t, err)
		assert.Nil(t, out)
	})

	t.Run("existing values win, template keys are gained", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "config.yaml", []byte("log:\n  level: debug\n"), 0o600))

		out, err := mergeExistingOverTemplate(fs, "config.yaml",
			[]byte("log:\n  level: info\nupdate:\n  policy: \"\"\n"))
		require.NoError(t, err)
		assert.Contains(t, string(out), "level: debug")
		assert.Contains(t, string(out), "policy:")
	})

	t.Run("unreadable existing file errors", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		_, err := mergeExistingOverTemplate(fs, "missing.yaml", []byte("a: 1\n"))
		require.Error(t, err)
	})

	t.Run("malformed template errors", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "config.yaml", []byte("a: 1\n"), 0o600))

		_, err := mergeExistingOverTemplate(fs, "config.yaml", []byte(":\tnot yaml"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "init template")
	})

	t.Run("malformed existing config errors", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "config.yaml", []byte(":\tnot yaml"), 0o600))

		_, err := mergeExistingOverTemplate(fs, "config.yaml", []byte("a: 1\n"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "existing config")
	})
}

// TestAssetDocument covers the merged-read helper's guards.
func TestAssetDocument(t *testing.T) {
	t.Parallel()

	t.Run("nil assets", func(t *testing.T) {
		t.Parallel()

		assert.Nil(t, assetDocument(&props.Props{}, DefaultsAssetPath))
	})

	t.Run("absent path", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{Assets: props.NewAssets()}
		assert.Nil(t, assetDocument(p, "assets/nonexistent.yaml"))
	})

	t.Run("merged across bundles", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{Assets: props.NewAssets(props.AssetMap{
			"one": fstest.MapFS{"assets/config.yaml": &fstest.MapFile{Data: []byte("a: 1\n")}},
			"two": fstest.MapFS{"assets/config.yaml": &fstest.MapFile{Data: []byte("b: 2\n")}},
		})}

		doc := string(assetDocument(p, DefaultsAssetPath))
		assert.Contains(t, doc, "a:")
		assert.Contains(t, doc, "b:")
	})
}

// TestWriteInitialConfig_CleanOverwrites covers the --clean branch: an
// existing file is replaced by the template rather than merged.
func TestWriteInitialConfig_CleanOverwrites(t *testing.T) {
	t.Parallel()

	p := editorProps(t)
	require.NoError(t, p.FS.MkdirAll("/cfg", 0o755))
	require.NoError(t, afero.WriteFile(p.FS, "/cfg/config.yaml", []byte("custom: value\n"), 0o600))

	require.NoError(t, writeInitialConfig(p, "/cfg/config.yaml", true))

	data, err := afero.ReadFile(p.FS, "/cfg/config.yaml")
	require.NoError(t, err)
	assert.NotContains(t, string(data), "custom", "--clean must replace, not merge")
	assert.Contains(t, string(data), "log:", "the framework template seeds the fresh file")
}
