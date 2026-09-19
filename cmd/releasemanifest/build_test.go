package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
)

const base = "https://pkg.example.internal/acme/fixtool"

// The fixture is a real goreleaser 2.17 artifacts.json and metadata.json,
// recorded from a snapshot of a scaffolded tool (spec 0203 OQ4), with the
// tag set and a Signature entry added the way the signs pipe writes one.
func fixtureDist(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	for _, name := range []string{"artifacts.json", "metadata.json"} {
		raw, err := os.ReadFile(filepath.Join("testdata", name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), raw, 0o600)) //nolint:gosec // G703: fixture names from a fixed list
	}

	// Sizes come from the archives on disk; the fixture carries stand-ins.
	for _, name := range []string{
		"fixtool_Darwin_arm64.tar.gz", "fixtool_Darwin_x86_64.tar.gz", "fixtool_Linux_arm64.tar.gz",
		"fixtool_Linux_x86_64.tar.gz", "fixtool_Windows_arm64.tar.gz", "fixtool_Windows_x86_64.tar.gz",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("archive:"+name), 0o600))
	}

	return dir
}

func TestBuild_GoldenManifest(t *testing.T) {
	t.Parallel()

	dist := fixtureDist(t)

	m, err := build(dist, base, "v1.1.0", "")
	require.NoError(t, err)
	require.NoError(t, m.Validate(base), "what we write is what a reader accepts")

	got, err := json.MarshalIndent(m, "", "  ")
	require.NoError(t, err)

	want, err := os.ReadFile(filepath.Join("testdata", "release.golden.json"))
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got))
}

func TestBuild_Shape(t *testing.T) {
	t.Parallel()

	m, err := build(fixtureDist(t), base, "v1.1.0", "")
	require.NoError(t, err)

	assert.Equal(t, static.SchemaVersion, m.Schema)
	assert.Equal(t, "fixtool", m.Tool)
	assert.Equal(t, "v1.2.0", m.Tag)
	assert.Equal(t, "2026-09-19T12:17:26Z", m.ReleasedAt)
	assert.Equal(t, "v1.1.0", m.Previous)
	assert.Equal(t, base+"/v1.2.0/checksums.txt", m.Checksums)
	assert.Equal(t, base+"/v1.2.0/checksums.txt.sig", m.Signature)
	require.Len(t, m.Downloads, 6)

	// Downloads are ordered by os then arch, so the document is stable
	// whatever order goreleaser happened to build in.
	assert.Equal(t, "darwin", m.Downloads[0].OS)
	assert.Equal(t, "amd64", m.Downloads[0].Arch)
	assert.Equal(t, "windows", m.Downloads[5].OS)

	linux := m.Downloads[2]
	assert.Equal(t, "linux", linux.OS)
	assert.Equal(t, "amd64", linux.Arch)
	assert.Equal(t, "fixtool_Linux_x86_64.tar.gz", linux.Name)
	assert.Equal(t, base+"/v1.2.0/fixtool_Linux_x86_64.tar.gz", linux.URL)
	assert.Equal(t, "ec90dd6d7f33fcaa8179a6647e0ad6e2fe17e0551b8989760ff2ce33ed907d81", linux.SHA256, "the sha256: prefix goreleaser writes is stripped")
	assert.Equal(t, int64(len("archive:fixtool_Linux_x86_64.tar.gz")), linux.Size)
}

func TestBuild_Refusals(t *testing.T) {
	t.Parallel()

	t.Run("an archive without a checksum is refused", func(t *testing.T) {
		t.Parallel()

		dist := fixtureDist(t)
		stripChecksum(t, dist, "fixtool_Linux_arm64.tar.gz")

		_, err := build(dist, base, "", "")
		require.ErrorIs(t, err, errNoChecksum)
		assert.Contains(t, err.Error(), "fixtool_Linux_arm64.tar.gz")
	})

	t.Run("no signature entry means no signature URL", func(t *testing.T) {
		t.Parallel()

		dist := fixtureDist(t)
		dropType(t, dist, "Signature")

		m, err := build(dist, base, "", "")
		require.NoError(t, err)
		assert.Empty(t, m.Signature)
		require.NoError(t, m.Validate(base))
	})

	t.Run("no checksums entry is refused outright", func(t *testing.T) {
		t.Parallel()

		dist := fixtureDist(t)
		dropType(t, dist, "Checksum")

		_, err := build(dist, base, "", "")
		require.ErrorIs(t, err, errNoChecksum)
	})

	t.Run("notes come from the file when given", func(t *testing.T) {
		t.Parallel()

		dist := fixtureDist(t)
		notes := filepath.Join(t.TempDir(), "notes.md")
		require.NoError(t, os.WriteFile(notes, []byte("## v1.2.0\n\n- a thing\n"), 0o600))

		m, err := build(dist, base, "", notes)
		require.NoError(t, err)
		assert.Equal(t, "## v1.2.0\n\n- a thing\n", m.Notes)
		assert.Empty(t, m.Previous, "no previous given and no pointer fetched: the chain starts here")
	})
}

func stripChecksum(t *testing.T, dist, name string) {
	t.Helper()

	var entries []map[string]any
	raw, err := os.ReadFile(filepath.Join(dist, "artifacts.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &entries))

	for _, e := range entries {
		if e["name"] == name {
			delete(e["extra"].(map[string]any), "Checksum")
		}
	}

	out, err := json.Marshal(entries)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dist, "artifacts.json"), out, 0o600))
}

func dropType(t *testing.T, dist, typ string) {
	t.Helper()

	var entries []map[string]any
	raw, err := os.ReadFile(filepath.Join(dist, "artifacts.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &entries))

	kept := entries[:0]
	for _, e := range entries {
		if e["type"] != typ {
			kept = append(kept, e)
		}
	}

	out, err := json.Marshal(kept)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dist, "artifacts.json"), out, 0o600))
}

// The chain's previous comes from the pointer at the base when --previous is
// not given: a missing pointer starts the chain, a present one names the tag.
func TestCurrentTag(t *testing.T) {
	t.Parallel()

	var srv *httptest.Server

	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/acme/present/latest.json":
			p := static.Pointer{Schema: 1, Tool: "fixtool", Tag: "v1.1.0", Manifest: srv.URL + "/acme/present/v1.1.0/release.json", PublishedAt: "2026-09-19T12:00:00Z"}
			_ = json.NewEncoder(w).Encode(p)
		case "/acme/escapes/latest.json":
			_, _ = w.Write([]byte(`{"schema":1,"tool":"fixtool","tag":"v1.1.0","manifest":"https://elsewhere.example.com/release.json","published_at":"2026-09-19T12:00:00Z"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	tag, err := currentTag(context.Background(), srv.URL+"/acme/present")
	require.NoError(t, err)
	assert.Equal(t, "v1.1.0", tag)

	tag, err = currentTag(context.Background(), srv.URL+"/acme/absent")
	require.NoError(t, err)
	assert.Empty(t, tag, "no pointer yet: the chain starts here")

	_, err = currentTag(context.Background(), srv.URL+"/acme/escapes")
	require.ErrorIs(t, err, static.ErrEscapesBase)
}
