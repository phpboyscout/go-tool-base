package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
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

	"gitlab.com/phpboyscout/go/signing/minisign"
	"gitlab.com/phpboyscout/go/signing/verify"

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

func fixturePlatform(path, _ string) (built, error) {
	p, ok := fixturePlatforms[filepath.Base(path)]
	if !ok {
		return built{}, errNoPlatform
	}

	return built{goos: p[0], goarch: p[1], version: "v1.2.0"}, nil
}

// fixtureRelease is fixturePlatform with every binary built as tag.
func fixtureRelease(tag string) func(string, string) (built, error) {
	return func(path, tool string) (built, error) {
		b, err := fixturePlatform(path, tool)
		b.version = tag

		return b, err
	}
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

	m, p, err := fixtureBuilder().build(dist, base, "v1.1.0", nil)
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

	m, p, err := fixtureBuilder().build(fixtureDist(t), base, "v1.1.0", nil)
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

		_, _, err := fixtureBuilder().build(dist, base, "", nil)
		require.ErrorIs(t, err, errNoChecksum)
		assert.Contains(t, err.Error(), "fixtool_Linux_arm64.tar.gz")
	})

	t.Run("no signature file means no signature URL", func(t *testing.T) {
		t.Parallel()

		dist := fixtureDist(t)
		require.NoError(t, os.Remove(filepath.Join(dist, "checksums.txt.sig")))

		m, _, err := fixtureBuilder().build(dist, base, "", nil)
		require.NoError(t, err)
		assert.Empty(t, m.Signature)
		require.NoError(t, m.Validate(base))
	})

	t.Run("a rotation manifest and its signature are listed together", func(t *testing.T) {
		t.Parallel()

		dist := fixtureDist(t)
		require.NoError(t, os.WriteFile(filepath.Join(dist, static.RotationFile), []byte("{}"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dist, static.RotationSignatureFile), []byte("sig"), 0o600))

		m, _, err := fixtureBuilder().build(dist, base, "", nil)
		require.NoError(t, err)
		assert.Equal(t, base+"/v1.2.0/"+static.RotationFile, m.Rotation)
		assert.Equal(t, base+"/v1.2.0/"+static.RotationSignatureFile, m.RotationSignature)
		require.NoError(t, m.Validate(base))
	})

	t.Run("a rotation manifest without its signature is refused", func(t *testing.T) {
		t.Parallel()

		dist := fixtureDist(t)
		require.NoError(t, os.WriteFile(filepath.Join(dist, static.RotationFile), []byte("{}"), 0o600))

		_, _, err := fixtureBuilder().build(dist, base, "", nil)
		require.ErrorIs(t, err, errRotationUnsigned)
	})

	t.Run("no checksums.txt is refused outright", func(t *testing.T) {
		t.Parallel()

		dist := fixtureDist(t)
		require.NoError(t, os.Remove(filepath.Join(dist, "checksums.txt")))

		_, _, err := fixtureBuilder().build(dist, base, "", nil)
		require.ErrorIs(t, err, errNoChecksum)
	})

	t.Run("two archives for one platform are refused", func(t *testing.T) {
		t.Parallel()

		dist := fixtureDist(t)
		b := fixtureBuilder()
		b.platform = func(string, string) (built, error) {
			return built{goos: "linux", goarch: "amd64", version: "v1.2.0"}, nil
		}

		_, _, err := b.build(dist, base, "", nil)
		require.ErrorIs(t, err, errDuplicatePlatform)
		assert.Contains(t, err.Error(), "linux/amd64")
	})

	t.Run("an archive whose binary names no platform is refused", func(t *testing.T) {
		t.Parallel()

		dist := fixtureDist(t)
		b := fixtureBuilder()
		b.platform = func(path, tool string) (built, error) {
			if strings.Contains(path, "Windows_arm64") {
				return built{}, errNoPlatform
			}

			return fixturePlatform(path, tool)
		}

		_, _, err := b.build(dist, base, "", nil)
		require.ErrorIs(t, err, errNoPlatform)
		assert.Contains(t, err.Error(), "fixtool_Windows_arm64.tar.gz")
	})

	// Spec 0220 D6.
	for name, tt := range map[string]struct{ recorded, named string }{
		"an archive whose binary is another release is refused": {recorded: "v1.0.0", named: "records v1.0.0"},
		"an archive whose binary records no release is refused": {named: "records no release version"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			b := fixtureBuilder()
			b.platform = func(path, tool string) (built, error) {
				p, err := fixturePlatform(path, tool)
				if strings.Contains(path, "Windows_arm64") {
					p.version = tt.recorded
				}

				return p, err
			}

			_, _, err := b.build(fixtureDist(t), base, "", nil)
			require.ErrorIs(t, err, errOtherVersion)
			assert.Contains(t, err.Error(), "fixtool_Windows_arm64.tar.gz")
			assert.Contains(t, err.Error(), tt.named)
		})
	}

	t.Run("the tool's binary is the one read", func(t *testing.T) {
		t.Parallel()

		b := fixtureBuilder()
		b.platform = func(path, tool string) (built, error) {
			assert.Equal(t, "fixtool", tool)

			return fixturePlatform(path, tool)
		}

		_, _, err := b.build(fixtureDist(t), base, "", nil)
		require.NoError(t, err)
	})

	t.Run("a dirty stamp of the release is published", func(t *testing.T) {
		t.Parallel()

		b := fixtureBuilder()
		b.platform = func(path, tool string) (built, error) {
			p, err := fixturePlatform(path, tool)
			p.version += "+dirty"

			return p, err
		}

		_, _, err := b.build(fixtureDist(t), base, "", nil)
		require.NoError(t, err)
	})

	t.Run("notes come from the file when given", func(t *testing.T) {
		t.Parallel()

		dist := fixtureDist(t)
		notes := filepath.Join(t.TempDir(), "notes.md")
		require.NoError(t, os.WriteFile(notes, []byte("## v1.2.0\n\n- a thing\n"), 0o600))

		m, _, err := fixtureBuilder().build(dist, base, "", notesFromFile(notes))
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

// A prerelease tag publishes its manifest but leaves the pointer, so it is
// never offered as every user's latest (spec 0104 D12).
func TestWriteDocuments_PrereleaseLeavesThePointer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		tag         string
		wantPointer bool
	}{
		{"stable tag moves the pointer", "v1.2.0", true},
		{"rc tag leaves it", "v1.2.0-rc.1", false},
		{"beta tag leaves it", "v1.2.0-beta.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dist := fixtureDist(t)
			setFixtureTag(t, dist, tt.tag)

			b := fixtureBuilder()
			b.platform = fixtureRelease(tt.tag)

			m, p, err := b.build(dist, base, "v1.1.0", nil)
			require.NoError(t, err)

			wrote, err := writeDocuments(dist, m, p)
			require.NoError(t, err)
			assert.Equal(t, tt.wantPointer, wrote)

			assert.FileExists(t, filepath.Join(dist, static.ManifestFile), "every tag publishes its manifest")

			_, statErr := os.Stat(filepath.Join(dist, static.PointerFile))
			if tt.wantPointer {
				require.NoError(t, statErr)
			} else {
				require.ErrorIs(t, statErr, os.ErrNotExist)
			}
		})
	}
}

func setFixtureTag(t *testing.T, dist, tag string) {
	t.Helper()

	path := filepath.Join(dist, "metadata.json")

	var meta map[string]any

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &meta))

	meta["tag"] = tag
	meta["version"] = strings.TrimPrefix(tag, "v")

	raw, err = json.Marshal(meta)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0o600))
}

// Once a manifest is committed, every later release carries it (spec 0101 D12).
func TestStageRotation(t *testing.T) {
	t.Parallel()

	t.Run("both files are carried into dist and listed", func(t *testing.T) {
		t.Parallel()

		repo, dist := t.TempDir(), fixtureDist(t)
		require.NoError(t, os.WriteFile(filepath.Join(repo, static.RotationFile), []byte(`{"epoch":1}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(repo, static.RotationSignatureFile), []byte("sig"), 0o600))

		require.NoError(t, stageRotation(repo, dist))

		got, err := os.ReadFile(filepath.Join(dist, static.RotationFile))
		require.NoError(t, err)
		assert.JSONEq(t, `{"epoch":1}`, string(got))

		m, _, err := fixtureBuilder().build(dist, base, "", nil)
		require.NoError(t, err)
		assert.Equal(t, base+"/v1.2.0/"+static.RotationSignatureFile, m.RotationSignature)
	})

	t.Run("no directory, or an empty one, adds nothing", func(t *testing.T) {
		t.Parallel()

		for _, dir := range []string{"", filepath.Join(t.TempDir(), "absent"), t.TempDir()} {
			dist := fixtureDist(t)
			require.NoError(t, stageRotation(dir, dist))

			m, _, err := fixtureBuilder().build(dist, base, "", nil)
			require.NoError(t, err)
			assert.Empty(t, m.Rotation)
		}
	})

	t.Run("a manifest committed without its signature stops the release", func(t *testing.T) {
		t.Parallel()

		repo, dist := t.TempDir(), fixtureDist(t)
		require.NoError(t, os.WriteFile(filepath.Join(repo, static.RotationFile), []byte("{}"), 0o600))

		require.NoError(t, stageRotation(repo, dist))

		_, _, err := fixtureBuilder().build(dist, base, "", nil)
		require.ErrorIs(t, err, errRotationUnsigned)
	})
}

// A carried manifest is checked before it is published: one that does not
// verify, or will expire soon, would fail every fresh install's update to this
// release and every later one (spec 0101 D5, D12).
func TestCheckRotation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

	authority := rotationAuthority
	staged := func(t *testing.T, signer ed25519.PrivateKey, notAfter time.Time) string {
		t.Helper()

		return signedRotation(t, signer, now, notAfter)
	}

	t.Run("a verified manifest well inside its window passes", func(t *testing.T) {
		t.Parallel()

		signer, keys := authority(t)
		require.NoError(t, checkRotation(staged(t, signer, now.AddDate(1, 0, 0)), keys, now))
	})

	t.Run("nothing staged needs no authority", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, checkRotation(t.TempDir(), "", now))
	})

	t.Run("a manifest no authority key verifies is refused", func(t *testing.T) {
		t.Parallel()

		signer, _ := authority(t)
		_, otherKeys := authority(t)
		require.ErrorIs(t, checkRotation(staged(t, signer, now.AddDate(1, 0, 0)), otherKeys, now), verify.ErrRotationSignatureInvalid)
	})

	t.Run("a manifest with no authority keys to check it against is refused", func(t *testing.T) {
		t.Parallel()

		signer, _ := authority(t)
		require.ErrorIs(t, checkRotation(staged(t, signer, now.AddDate(1, 0, 0)), t.TempDir(), now), errRotationUnverifiable)
	})

	t.Run("a manifest that does not parse is refused", func(t *testing.T) {
		t.Parallel()

		signer, keys := authority(t)
		dist := staged(t, signer, now.AddDate(1, 0, 0))
		require.NoError(t, os.WriteFile(filepath.Join(dist, static.RotationFile), []byte("{not json"), 0o600))
		require.Error(t, checkRotation(dist, keys, now))
	})

	t.Run("a manifest expiring inside 30 days, or expired, is refused", func(t *testing.T) {
		t.Parallel()

		signer, keys := authority(t)
		for _, notAfter := range []time.Time{now.Add(29*24*time.Hour + 12*time.Hour), now.Add(-time.Minute)} {
			require.ErrorIs(t, checkRotation(staged(t, signer, notAfter), keys, now), errRotationExpiring, notAfter.String())
		}
	})

	t.Run("a manifest valid for exactly 30 days, or longer, passes", func(t *testing.T) {
		t.Parallel()

		signer, keys := authority(t)
		for _, notAfter := range []time.Time{now.Add(30 * 24 * time.Hour), now.Add(30*24*time.Hour + time.Hour)} {
			require.NoError(t, checkRotation(staged(t, signer, notAfter), keys, now), notAfter.String())
		}
	})

	t.Run("an authority key below the top of the directory is not read", func(t *testing.T) {
		t.Parallel()

		signer, keys := authority(t)
		nested := filepath.Join(t.TempDir(), "old")
		require.NoError(t, os.Rename(keys, nested))
		require.ErrorIs(t, checkRotation(staged(t, signer, now.AddDate(1, 0, 0)), filepath.Dir(nested), now), errRotationUnverifiable)
	})

	t.Run("a committed manifest is staged and checked", func(t *testing.T) {
		t.Parallel()

		signer, keys := authority(t)
		committed := staged(t, signer, now.AddDate(1, 0, 0))

		require.ErrorIs(t, prepareRotation(committed, "", t.TempDir(), now), errRotationUnverifiable,
			"a release with --rotation and no --rotation-authority stops rather than publishing the manifest unchecked")

		dist := t.TempDir()
		require.NoError(t, prepareRotation(committed, keys, dist, now))
		assert.FileExists(t, filepath.Join(dist, static.RotationFile))
	})

	t.Run("no committed manifest needs no authority", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, prepareRotation(t.TempDir(), "", t.TempDir(), now))
	})
}

// A manifest the channel reader would refuse is not written: update would
// fail on the release it describes (#188). The reader takes exactly
// MaxManifestBytes, so that size is written and one byte more is not.
func TestWriteDocuments_ManifestSizeBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		size    int
		written bool
	}{
		{"at the limit", static.MaxManifestBytes, true},
		{"a byte over", static.MaxManifestBytes + 1, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dist := fixtureDist(t)

			m, p, err := fixtureBuilder().build(dist, base, "v1.1.0", nil)
			require.NoError(t, err)

			m.Notes = "x"
			raw, err := encodeDocument(m)
			require.NoError(t, err)

			m.Notes = strings.Repeat("x", 1+tc.size-len(raw))
			raw, err = encodeDocument(m)
			require.NoError(t, err)
			require.Len(t, raw, tc.size)

			_, err = writeDocuments(dist, m, p)
			if tc.written {
				require.NoError(t, err)
				assert.FileExists(t, filepath.Join(dist, static.ManifestFile))

				return
			}

			require.ErrorIs(t, err, errManifestTooLarge)
			assert.NoFileExists(t, filepath.Join(dist, static.ManifestFile))
			assert.NoFileExists(t, filepath.Join(dist, static.PointerFile))
		})
	}
}

// rotationAuthority writes a fresh rotation authority's public key into a
// directory and returns its private key and that directory.
func rotationAuthority(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	key, err := minisign.NewPublicKey(pub)
	require.NoError(t, err)

	file, err := key.File("rotation authority")
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "authority.pub"), file, 0o600))

	return priv, dir
}

// signedRotation writes a rotation manifest signed by signer, and its
// signature, into a directory and returns it.
func signedRotation(t *testing.T, signer ed25519.PrivateKey, now, notAfter time.Time) string {
	t.Helper()

	manifest, sig, err := verify.SignRotationManifest(signer, verify.RotationManifest{
		Schema:   verify.RotationSchema,
		IssuedAt: now.Add(-time.Hour),
		NotAfter: notAfter,
		Reason:   verify.RotationReasonScheduled,
		Epoch:    1,
	})
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, static.RotationFile), manifest, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, static.RotationSignatureFile), sig, 0o600))

	return dir
}

func TestVerifyCommittedRotation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

	t.Run("a verified manifest passes and is left where it is", func(t *testing.T) {
		t.Parallel()

		signer, keys := rotationAuthority(t)
		dir := signedRotation(t, signer, now, now.AddDate(1, 0, 0))
		require.NoError(t, verifyCommittedRotation(dir, keys, now))
		assert.FileExists(t, filepath.Join(dir, static.RotationFile))
	})

	t.Run("no rotation directory yet passes", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, verifyCommittedRotation(filepath.Join(t.TempDir(), "absent"), "", now))
	})

	t.Run("a manifest without its signature is refused", func(t *testing.T) {
		t.Parallel()

		signer, keys := rotationAuthority(t)
		dir := signedRotation(t, signer, now, now.AddDate(1, 0, 0))
		require.NoError(t, os.Remove(filepath.Join(dir, static.RotationSignatureFile)))
		require.ErrorIs(t, verifyCommittedRotation(dir, keys, now), errRotationUnsigned)
	})

	t.Run("a signature without its manifest is refused", func(t *testing.T) {
		t.Parallel()

		signer, keys := rotationAuthority(t)
		dir := signedRotation(t, signer, now, now.AddDate(1, 0, 0))
		require.NoError(t, os.Remove(filepath.Join(dir, static.RotationFile)))
		require.ErrorIs(t, verifyCommittedRotation(dir, keys, now), errRotationUnsigned)
	})

	t.Run("a manifest another key signed is refused", func(t *testing.T) {
		t.Parallel()

		signer, _ := rotationAuthority(t)
		_, otherKeys := rotationAuthority(t)
		dir := signedRotation(t, signer, now, now.AddDate(1, 0, 0))
		require.ErrorIs(t, verifyCommittedRotation(dir, otherKeys, now), verify.ErrRotationSignatureInvalid)
	})

	t.Run("a manifest about to expire is refused", func(t *testing.T) {
		t.Parallel()

		signer, keys := rotationAuthority(t)
		dir := signedRotation(t, signer, now, now.AddDate(0, 0, 10))
		require.ErrorIs(t, verifyCommittedRotation(dir, keys, now), errRotationExpiring)
	})
}

func TestVerifyCommittedRotation_DanglingLinkIsRefused(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	signer, keys := rotationAuthority(t)
	dir := signedRotation(t, signer, now, now.AddDate(1, 0, 0))

	sig := filepath.Join(dir, static.RotationSignatureFile)
	require.NoError(t, os.Remove(sig))
	require.NoError(t, os.Symlink(filepath.Join(dir, "gone"), sig))

	require.ErrorIs(t, verifyCommittedRotation(dir, keys, now), errRotationUnreadable,
		"goreleaser would fail on it at upload, after the build")
}

func TestRunVerifyOnly(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	assert.Equal(t, exitUsage, runVerifyOnly("", "", &stderr))
	assert.Contains(t, stderr.String(), "--verify-only needs --rotation")

	assert.Zero(t, runVerifyOnly(filepath.Join(t.TempDir(), "absent"), "", &bytes.Buffer{}), "no rotation yet")

	lone := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(lone, static.RotationFile), []byte("{}"), 0o600))
	assert.Equal(t, 1, runVerifyOnly(lone, "", &bytes.Buffer{}))
}

// A committed rotation manifest given to a run is published with the release
// and named in its manifest (go-tool-base#205).
func TestRun_CarriesACommittedRotationManifest(t *testing.T) {
	t.Parallel()

	b := fixtureBuilder()
	signer, keys := rotationAuthority(t)
	committed := signedRotation(t, signer, b.now(), b.now().AddDate(1, 0, 0))
	dist := fixtureDist(t)

	require.NoError(t, run(b, release{dist: dist, baseURL: base, previous: "v1.1.0", rotation: committed, authority: keys}))

	assert.FileExists(t, filepath.Join(dist, static.RotationFile))
	assert.FileExists(t, filepath.Join(dist, static.RotationSignatureFile))

	raw, err := os.ReadFile(filepath.Join(dist, static.ManifestFile))
	require.NoError(t, err)

	var m static.Manifest
	require.NoError(t, json.Unmarshal(raw, &m))
	assert.Equal(t, static.FileURL(base, m.Tag, static.RotationFile), m.Rotation)
	assert.Equal(t, static.FileURL(base, m.Tag, static.RotationSignatureFile), m.RotationSignature)

	require.ErrorIs(t, run(b, release{dist: fixtureDist(t), baseURL: base, previous: "v1.1.0", rotation: committed}), errRotationUnverifiable,
		"a run refuses a committed manifest it has no authority to check")
}

// Equal strings are not enough: a tag that is not a version is never published.
func TestRefuseOtherVersion_ATagThatIsNotAVersion(t *testing.T) {
	t.Parallel()

	for _, recorded := range []string{"", "v1.2.3", "nightly"} {
		require.ErrorIs(t, refuseOtherVersion("fixtool.tar.gz", "nightly", recorded), errOtherVersion, recorded)
	}
}
