package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
)

const base = "https://pkg.example.internal/acme/fixtool"

// fixturePlatforms is what the build info of each fixture archive's binary
// would say; the golden test injects it so the fixture needs no real Go
// binaries (readPlatform is tested against this test's own executable).
var fixturePlatforms = map[string][2]string{
	"fixtool_Darwin_arm64.tar.gz":   {"darwin", "arm64"},
	"fixtool_Darwin_x86_64.tar.gz":  {"darwin", "amd64"},
	"fixtool_Linux_arm64.tar.gz":    {"linux", "arm64"},
	"fixtool_Linux_x86_64.tar.gz":   {"linux", "amd64"},
	"fixtool_Windows_arm64.tar.gz":  {"windows", "arm64"},
	"fixtool_Windows_x86_64.tar.gz": {"windows", "amd64"},
}

func fixturePlatform(path string) (string, string, error) {
	p, ok := fixturePlatforms[filepath.Base(path)]
	if !ok {
		return "", "", errNoPlatform
	}

	return p[0], p[1], nil
}

// The fixture is a real goreleaser 2.17 metadata.json and checksums.txt,
// recorded from a snapshot of a scaffolded tool (spec 0203 OQ4), with the
// tag set and a signature file added the way the signs pipe writes one.
func fixtureDist(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	for _, name := range []string{"checksums.txt", "metadata.json"} {
		raw, err := os.ReadFile(filepath.Join("testdata", name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), raw, 0o600)) //nolint:gosec // G703: fixture names from a fixed list
	}

	// Sizes come from the archives on disk; the fixture carries stand-ins,
	// plus the sbom goreleaser also lists in checksums.txt and the signature.
	for name := range fixturePlatforms {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("archive:"+name), 0o600))
	}

	require.NoError(t, os.WriteFile(filepath.Join(dir, "fixtool_Linux_x86_64.tar.gz.sbom.json"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "checksums.txt.sig"), []byte("sig"), 0o600))

	return dir
}

func fixtureBuilder() builder {
	return builder{
		platform: fixturePlatform,
		now:      func() time.Time { return time.Date(2026, 9, 19, 12, 20, 0, 0, time.UTC) },
	}
}

func TestBuild_GoldenManifest(t *testing.T) {
	t.Parallel()

	dist := fixtureDist(t)

	m, p, err := fixtureBuilder().build(dist, base, "v1.1.0", "")
	require.NoError(t, err)
	require.NoError(t, m.Validate(base), "what we write is what a reader accepts")
	require.NoError(t, p.Validate(base))

	got, err := json.MarshalIndent(m, "", "  ")
	require.NoError(t, err)

	want, err := os.ReadFile(filepath.Join("testdata", "release.golden.json"))
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got))
}

func TestBuild_Shape(t *testing.T) {
	t.Parallel()

	m, p, err := fixtureBuilder().build(fixtureDist(t), base, "v1.1.0", "")
	require.NoError(t, err)

	assert.Equal(t, static.SchemaVersion, m.Schema)
	assert.Equal(t, "fixtool", m.Tool)
	assert.Equal(t, "v1.2.0", m.Tag)
	assert.Equal(t, "2026-09-19T12:17:26Z", m.ReleasedAt)
	assert.Equal(t, "v1.1.0", m.Previous)
	assert.Equal(t, base+"/v1.2.0/checksums.txt", m.Checksums)
	assert.Equal(t, base+"/v1.2.0/checksums.txt.sig", m.Signature)
	require.Len(t, m.Downloads, 6, "the sbom in checksums.txt is not a download")

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
	assert.Equal(t, "ec90dd6d7f33fcaa8179a6647e0ad6e2fe17e0551b8989760ff2ce33ed907d81", linux.SHA256, "the digest checksums.txt carries")
	assert.Equal(t, int64(len("archive:fixtool_Linux_x86_64.tar.gz")), linux.Size)

	// The pointer the publish step moves last names this release.
	assert.Equal(t, static.Pointer{
		Schema:      static.SchemaVersion,
		Tool:        "fixtool",
		Tag:         "v1.2.0",
		Manifest:    base + "/v1.2.0/release.json",
		PublishedAt: "2026-09-19T12:20:00Z",
	}, p)
}

func TestBuild_Refusals(t *testing.T) {
	t.Parallel()

	t.Run("an archive missing from checksums.txt is refused", func(t *testing.T) {
		t.Parallel()

		dist := fixtureDist(t)
		dropChecksumLine(t, dist, "fixtool_Linux_arm64.tar.gz")

		_, _, err := fixtureBuilder().build(dist, base, "", "")
		require.ErrorIs(t, err, errNoChecksum)
		assert.Contains(t, err.Error(), "fixtool_Linux_arm64.tar.gz")
	})

	t.Run("no signature file means no signature URL", func(t *testing.T) {
		t.Parallel()

		dist := fixtureDist(t)
		require.NoError(t, os.Remove(filepath.Join(dist, "checksums.txt.sig")))

		m, _, err := fixtureBuilder().build(dist, base, "", "")
		require.NoError(t, err)
		assert.Empty(t, m.Signature)
		require.NoError(t, m.Validate(base))
	})

	t.Run("no checksums.txt is refused outright", func(t *testing.T) {
		t.Parallel()

		dist := fixtureDist(t)
		require.NoError(t, os.Remove(filepath.Join(dist, "checksums.txt")))

		_, _, err := fixtureBuilder().build(dist, base, "", "")
		require.ErrorIs(t, err, errNoChecksum)
	})

	t.Run("two archives for one platform are refused", func(t *testing.T) {
		t.Parallel()

		dist := fixtureDist(t)
		b := fixtureBuilder()
		b.platform = func(string) (string, string, error) { return "linux", "amd64", nil }

		_, _, err := b.build(dist, base, "", "")
		require.ErrorIs(t, err, errDuplicatePlatform)
		assert.Contains(t, err.Error(), "linux/amd64")
	})

	t.Run("an archive whose binary names no platform is refused", func(t *testing.T) {
		t.Parallel()

		dist := fixtureDist(t)
		b := fixtureBuilder()
		b.platform = func(path string) (string, string, error) {
			if strings.Contains(path, "Windows_arm64") {
				return "", "", errNoPlatform
			}

			return fixturePlatform(path)
		}

		_, _, err := b.build(dist, base, "", "")
		require.ErrorIs(t, err, errNoPlatform)
		assert.Contains(t, err.Error(), "fixtool_Windows_arm64.tar.gz")
	})

	t.Run("notes come from the file when given", func(t *testing.T) {
		t.Parallel()

		dist := fixtureDist(t)
		notes := filepath.Join(t.TempDir(), "notes.md")
		require.NoError(t, os.WriteFile(notes, []byte("## v1.2.0\n\n- a thing\n"), 0o600))

		m, _, err := fixtureBuilder().build(dist, base, "", notes)
		require.NoError(t, err)
		assert.Equal(t, "## v1.2.0\n\n- a thing\n", m.Notes)
		assert.Empty(t, m.Previous, "no previous given and no pointer fetched: the chain starts here")
	})
}

func dropChecksumLine(t *testing.T, dist, name string) {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(dist, "checksums.txt"))
	require.NoError(t, err)

	var kept []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		if !strings.HasSuffix(line, "  "+name) {
			kept = append(kept, line)
		}
	}

	require.NoError(t, os.WriteFile(filepath.Join(dist, "checksums.txt"), []byte(strings.Join(kept, "\n")), 0o600))
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
