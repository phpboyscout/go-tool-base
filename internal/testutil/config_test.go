package testutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
)

func TestStoreFromYAML_ResolvesValues(t *testing.T) {
	t.Parallel()

	store := StoreFromYAML(t, "a:\n  b: value\n")
	assert.Equal(t, "value", store.View().GetString("a.b"))
}

func TestStoreFromYAML_LayersOptionsAbove(t *testing.T) {
	t.Setenv("TFIX_A_B", "from-env")

	view := ViewFromYAML(t, "a:\n  b: from-file\n", config.WithEnv("TFIX"))
	assert.Equal(t, "from-env", view.GetString("a.b"), "options append above the document layer")
}

// TestFileStoreFromYAML_Provenance pins what the file-backed fixture exists
// for: the document loads as a FILE layer (user-authored provenance), which a
// reader layer deliberately is not, and the layer accepts an Apply.
func TestFileStoreFromYAML_Provenance(t *testing.T) {
	t.Parallel()

	store := FileStoreFromYAML(t, "a:\n  b: value\n")

	origin, ok := store.View().Origin("a.b")
	require.True(t, ok)
	assert.Equal(t, config.SourceFile, origin.Kind)

	_, err := store.Apply(t.Context(), config.Set("a.c", "written"))
	require.NoError(t, err)
	assert.Equal(t, "written", store.View().GetString("a.c"))

	// The reader-backed fixture, by contrast, has no writable layer.
	_, err = StoreFromYAML(t, "a: 1\n").Apply(t.Context(), config.Set("x", "y"))
	require.Error(t, err)
}

func TestFileViewFromYAML(t *testing.T) {
	t.Parallel()

	view := FileViewFromYAML(t, "a: 1\n")
	assert.Equal(t, 1, view.GetInt("a"))
}

func TestMutableStoreFromYAML_ReloadPublishesNewContent(t *testing.T) {
	t.Parallel()

	store, src := MutableStoreFromYAML(t, "a: 1\n")
	assert.Equal(t, 1, store.View().GetInt("a"))

	src.Set("a: 2\n")
	require.NoError(t, store.Reload(t.Context()))
	assert.Equal(t, 2, store.View().GetInt("a"))

	assert.Equal(t, "test.yaml", src.ID())
	assert.Equal(t, config.Capabilities{}, src.Capabilities())
}
