package props

import (
	"io"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// TestSetLogger_NilReceiverSafe covers the nil-receiver guard.
func TestSetLogger_NilReceiverSafe(t *testing.T) {
	t.Parallel()

	var a *embeddedAssets
	assert.NotPanics(t, func() { a.SetLogger(logger.NewNoop()) })
}

// TestOpenMerged_MalformedBundleWarns proves a malformed bundle is surfaced as a
// WARN (naming it) rather than silently dropped, while a well-formed bundle still
// merges — and that the merged file's Name() is the base name.
func TestOpenMerged_MalformedBundleWarns(t *testing.T) {
	t.Parallel()

	good := fstest.MapFS{"conf/app.yaml": &fstest.MapFile{Data: []byte("log:\n  level: info\n")}}
	broken := fstest.MapFS{"conf/app.yaml": &fstest.MapFile{Data: []byte("log: [unterminated\n")}}

	a := newEmbeddedAssets()
	a.Register("good", good)
	a.Register("broken", broken)

	buf := logger.NewBuffer()
	a.SetLogger(buf)

	f, err := a.Open("conf/app.yaml")
	require.NoError(t, err, "the good bundle still yields merged content")

	data, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Contains(t, string(data), "info")

	// The malformed bundle is named in a WARN, not silently dropped.
	assert.True(t, buf.Contains("malformed embedded asset bundle"),
		"a malformed bundle must be logged at WARN")

	var named bool

	for _, e := range buf.Entries() {
		for _, kv := range e.Keyvals {
			if s, ok := kv.(string); ok && s == "broken" {
				named = true
			}
		}
	}

	assert.True(t, named, "the WARN names the offending bundle in its keyvals")

	// fs.FileInfo.Name() returns the base name, not the full path.
	info, err := f.Stat()
	require.NoError(t, err)
	assert.Equal(t, "app.yaml", info.Name())
}

// TestOpenMerged_MissingFileNotWarned confirms a bundle that simply lacks the
// file is silent (fs.ErrNotExist), not warned.
func TestOpenMerged_MissingFileNotWarned(t *testing.T) {
	t.Parallel()

	present := fstest.MapFS{"conf/app.yaml": &fstest.MapFile{Data: []byte("a: 1\n")}}
	absent := fstest.MapFS{"other.yaml": &fstest.MapFile{Data: []byte("b: 2\n")}}

	a := newEmbeddedAssets()
	a.Register("present", present)
	a.Register("absent", absent)

	buf := logger.NewBuffer()
	a.SetLogger(buf)

	_, err := a.Open("conf/app.yaml")
	require.NoError(t, err)
	assert.False(t, buf.Contains("malformed"), "a bundle missing the file must not warn")

	var _ fs.FS = a
}
