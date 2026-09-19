package static_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
)

const base = "https://pkg.example.internal/acme/fixtool"

func manifest() static.Manifest {
	return static.Manifest{
		Schema:     static.SchemaVersion,
		Tool:       "fixtool",
		Tag:        "v1.2.0",
		ReleasedAt: "2026-09-19T12:17:26Z",
		Previous:   "v1.1.0",
		Checksums:  base + "/v1.2.0/checksums.txt",
		Signature:  base + "/v1.2.0/checksums.txt.sig",
		Downloads: []static.Download{
			{OS: "linux", Arch: "amd64", Name: "fixtool_Linux_x86_64.tar.gz", URL: base + "/v1.2.0/fixtool_Linux_x86_64.tar.gz", Size: 10, SHA256: "ec90dd6d7f33fcaa8179a6647e0ad6e2fe17e0551b8989760ff2ce33ed907d81"},
		},
	}
}

// The wire form is the contract with any publisher that is not our tool
// (spec 0203 D8): field names are fixed, the signature is absent when there
// is none, and notes are optional.
func TestManifest_WireForm(t *testing.T) {
	t.Parallel()

	m := manifest()
	m.Signature = ""

	raw, err := json.Marshal(m)
	require.NoError(t, err)

	var wire map[string]any
	require.NoError(t, json.Unmarshal(raw, &wire))

	for _, key := range []string{"schema", "tool", "tag", "released_at", "previous", "checksums", "downloads"} {
		assert.Contains(t, wire, key)
	}

	assert.NotContains(t, wire, "signature", "an unsigned release carries no signature key")
	assert.NotContains(t, wire, "notes", "notes are optional and omitted when empty")
	assert.InDelta(t, float64(1), wire["schema"], 0)

	dl := wire["downloads"].([]any)[0].(map[string]any)
	for _, key := range []string{"os", "arch", "name", "url", "size", "sha256"} {
		assert.Contains(t, dl, key)
	}
}

func TestManifest_Validate(t *testing.T) {
	t.Parallel()

	t.Run("a well-formed manifest under its base passes", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, manifest().Validate(base))
	})

	t.Run("an unknown schema is refused with the value seen", func(t *testing.T) {
		t.Parallel()

		m := manifest()
		m.Schema = 7
		err := m.Validate(base)
		require.ErrorIs(t, err, static.ErrUnsupportedSchema)
		assert.Contains(t, err.Error(), "7")
	})

	t.Run("a URL outside the base is refused before anything is fetched", func(t *testing.T) {
		t.Parallel()

		m := manifest()
		m.Downloads[0].URL = "https://evil.example.com/fixtool.tar.gz"
		require.ErrorIs(t, m.Validate(base), static.ErrEscapesBase)

		m = manifest()
		m.Checksums = "http://pkg.example.internal/acme/fixtool/v1.2.0/checksums.txt"
		require.ErrorIs(t, m.Validate(base), static.ErrEscapesBase, "a scheme downgrade is an escape too")
	})

	t.Run("a previous that is not a tag is refused", func(t *testing.T) {
		t.Parallel()

		m := manifest()
		m.Previous = "https://elsewhere.example.com/v1.1.0/release.json"
		require.ErrorIs(t, m.Validate(base), static.ErrInvalidManifest)
	})

	t.Run("a download needs every field, and a well-formed sha256", func(t *testing.T) {
		t.Parallel()

		m := manifest()
		m.Downloads[0].SHA256 = "not-hex"
		require.ErrorIs(t, m.Validate(base), static.ErrInvalidManifest)

		m = manifest()
		m.Downloads[0].Arch = ""
		require.ErrorIs(t, m.Validate(base), static.ErrInvalidManifest)

		m = manifest()
		m.Downloads = nil
		require.ErrorIs(t, m.Validate(base), static.ErrInvalidManifest, "a release with nothing to download is not a release")
	})

	t.Run("checksums are required, the signature is not", func(t *testing.T) {
		t.Parallel()

		m := manifest()
		m.Signature = ""
		require.NoError(t, m.Validate(base))

		m.Checksums = ""
		require.ErrorIs(t, m.Validate(base), static.ErrInvalidManifest)
	})
}

func TestPointer_Validate(t *testing.T) {
	t.Parallel()

	p := static.Pointer{Schema: static.SchemaVersion, Tool: "fixtool", Tag: "v1.2.0", Manifest: base + "/v1.2.0/release.json", PublishedAt: "2026-09-19T12:20:00Z"}
	require.NoError(t, p.Validate(base))

	bad := p
	bad.Manifest = "https://elsewhere.example.com/v1.2.0/release.json"
	require.ErrorIs(t, bad.Validate(base), static.ErrEscapesBase)

	bad = p
	bad.Schema = 2
	require.ErrorIs(t, bad.Validate(base), static.ErrUnsupportedSchema)

	bad = p
	bad.Tag = ""
	require.ErrorIs(t, bad.Validate(base), static.ErrInvalidManifest)
}

// The paths are the layout the reference page states: one pointer under the
// base, one manifest per tag beside its files.
func TestLayoutPaths(t *testing.T) {
	t.Parallel()

	assert.Equal(t, base+"/latest.json", static.PointerURL(base))
	assert.Equal(t, base+"/v1.2.0/release.json", static.ManifestURL(base, "v1.2.0"))
	assert.Equal(t, base+"/v1.2.0/fixtool_Linux_x86_64.tar.gz", static.FileURL(base, "v1.2.0", "fixtool_Linux_x86_64.tar.gz"))
	assert.Equal(t, base+"/latest.json", static.PointerURL(base+"/"), "a trailing slash on the base is tolerated")
}

// Decode is the one way in for bytes off the wire: it parses, validates,
// and names the document in its error.
func TestDecode(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(manifest())
	require.NoError(t, err)

	m, err := static.DecodeManifest(raw, base)
	require.NoError(t, err)
	assert.Equal(t, "v1.2.0", m.Tag)

	_, err = static.DecodeManifest([]byte(`{"schema": 1, "tag": `), base)
	require.ErrorIs(t, err, static.ErrInvalidManifest)
	assert.Equal(t, "gtb.release.static.invalid_manifest", errors.KindOf(err))

	_, err = static.DecodePointer([]byte(`{"schema": 9}`), base)
	require.ErrorIs(t, err, static.ErrUnsupportedSchema)
}

// The remaining refusals: a manifest that is not JSON at the pointer, a
// signature that escapes, a tool-less manifest, and unparsable URLs on either
// side of the base check.
func TestValidate_RemainingRefusals(t *testing.T) {
	t.Parallel()

	m := manifest()
	m.Signature = "https://elsewhere.example.com/checksums.txt.sig"
	require.ErrorIs(t, m.Validate(base), static.ErrEscapesBase)

	m = manifest()
	m.Tool = ""
	require.ErrorIs(t, m.Validate(base), static.ErrInvalidManifest)

	m = manifest()
	m.Downloads[0].URL = "://not a url"
	require.ErrorIs(t, m.Validate(base), static.ErrInvalidManifest, "an unparsable URL is malformed, not merely elsewhere")

	require.ErrorIs(t, manifest().Validate("://bad base"), static.ErrInvalidManifest)

	_, err := static.DecodePointer([]byte(`{"schema": 1, "tag": `), base)
	require.ErrorIs(t, err, static.ErrInvalidManifest)

	_, err = static.DecodeManifest([]byte(`{"schema": 3}`), base)
	require.ErrorIs(t, err, static.ErrUnsupportedSchema)
}
