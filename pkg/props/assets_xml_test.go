package props

import (
	"io"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

// TestAssets_XMLIsAStaticAsset pins that an .xml asset is served verbatim
// like any other static file: encoding/xml cannot unmarshal into or marshal
// from map[string]any, so routing it through the structured merge could
// never succeed.
func TestAssets_XMLIsAStaticAsset(t *testing.T) {
	t.Parallel()

	assets := NewAssets(AssetMap{
		"root": fstest.MapFS{"feed.xml": &fstest.MapFile{Data: []byte("<a><b>1</b></a>")}},
	})

	f, err := assets.Open("feed.xml")
	require.NoError(t, err)

	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, "<a><b>1</b></a>", string(data))
}
