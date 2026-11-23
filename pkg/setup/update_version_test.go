package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/forge"
	forgetest "gitlab.com/phpboyscout/go/forge/test"
	"gitlab.com/phpboyscout/go/signing/verify"

	"gitlab.com/phpboyscout/go-tool-base/internal/binversion/binversiontest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
)

// stampedRelease publishes tag on store, signed, with the binary given.
func stampedRelease(t *testing.T, store *staticStore, tag string, binary []byte) {
	t.Helper()
	mustInitTestSigningKeys(t)

	archive := forgetest.TarGzAsset(e2eToolName, e2eToolName, string(binary))
	sums := forgetest.Manifest(false, archive)
	store.publish(t, tag, archive, sums, forgetest.SignatureAsset(testEd25519.entity, sums, false).Body)
}

func stampedUpdater(t *testing.T, store *staticStore, signed bool) (*SelfUpdater, *logger.CaptureHandler) {
	t.Helper()

	s, _ := signedStaticUpdater(t, store)
	s.requireSignature = signed

	captured := logger.NewCaptureHandler()
	s.logger = slog.New(captured)

	return s, captured
}

// requireInstalled asserts what is at the target, and that no temporary file
// is left beside it.
func requireInstalled(t *testing.T, s *SelfUpdater, want []byte) {
	t.Helper()

	const target = "/usr/local/bin/" + e2eToolName

	got, err := afero.ReadFile(s.Fs, target)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(want, got), "the installed binary is not the one expected")

	left, err := afero.Exists(s.Fs, target+"_")
	require.NoError(t, err)
	assert.False(t, left, "the extracted file is left behind")
}

var oldBinary = []byte("old-binary")

// Spec 0220 D1, D2, D4, D7, D8.
func TestUpdate_ChecksTheBinarysVersion(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	stamped := func(v string) []byte { return binversiontest.Binary(t, v, binversiontest.Tagged) }
	dirty := binversiontest.Binary(t, "v1.1.0", binversiontest.Dirty)
	pseudo := binversiontest.Binary(t, "", binversiontest.Untagged)
	devel := binversiontest.Binary(t, "v1.1.0", binversiontest.NoVCS)

	const (
		unchecked = "version could not be checked"
		installed = "not the version asked for"
	)

	for name, tt := range map[string]struct {
		tag     string
		binary  []byte
		signed  bool
		force   bool
		allow   bool
		refused string // what the hint says about the download
		warns   string
	}{
		"matching":                      {tag: "v1.1.0", binary: stamped("v1.1.0"), signed: true},
		"matching, dirty":               {tag: "v1.1.0", binary: dirty, signed: true},
		"a prerelease":                  {tag: "v1.1.0-rc.1", binary: stamped("v1.1.0-rc.1"), signed: true},
		"older":                         {tag: "v1.1.0", binary: stamped("v0.9.0"), signed: true, refused: "its binary is v0.9.0"},
		"newer":                         {tag: "v1.1.0", binary: stamped("v1.2.0"), signed: true, refused: "its binary is v1.2.0"},
		"the release's prerelease":      {tag: "v1.1.0", binary: stamped("v1.1.0-rc.1"), signed: true, refused: "its binary is v1.1.0-rc.1"},
		"older, unsigned":               {tag: "v1.1.0", binary: stamped("v0.9.0"), refused: "its binary is v0.9.0"},
		"older, forced":                 {tag: "v1.1.0", binary: stamped("v0.9.0"), signed: true, force: true, refused: "its binary is v0.9.0"},
		"older, allowed":                {tag: "v1.1.0", binary: stamped("v0.9.0"), signed: true, allow: true, warns: installed},
		"matching, allowed":             {tag: "v1.1.0", binary: stamped("v1.1.0"), signed: true, allow: true},
		"devel, signed":                 {tag: "v1.1.0", binary: devel, signed: true, refused: "its binary records no version"},
		"pseudo-version, signed":        {tag: "v1.1.0", binary: pseudo, signed: true, refused: "its binary records no version"},
		"not a Go binary, signed":       {tag: "v1.1.0", binary: []byte("text"), signed: true, refused: "its binary records no version"},
		"unversioned, signed, allowed":  {tag: "v1.1.0", binary: devel, signed: true, allow: true, refused: "its binary records no version"},
		"unversioned, signed, forced":   {tag: "v1.1.0", binary: devel, signed: true, force: true, refused: "its binary records no version"},
		"devel, unsigned":               {tag: "v1.1.0", binary: devel, warns: unchecked},
		"pseudo-version, unsigned":      {tag: "v1.1.0", binary: pseudo, warns: unchecked},
		"not a Go binary, unsigned":     {tag: "v1.1.0", binary: []byte("text"), warns: unchecked},
		"unversioned, unsigned, forced": {tag: "v1.1.0", binary: devel, force: true, warns: unchecked},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := newStaticStore(t)
			stampedRelease(t, store, tt.tag, tt.binary)

			s, captured := stampedUpdater(t, store, tt.signed)
			s.force = tt.force

			if tt.allow {
				WithVersionMismatchAllowed()(s)
			}

			_, err := s.Update(t.Context())

			if tt.refused != "" {
				require.ErrorIs(t, err, ErrVersionMismatch)
				hint := hintOf(t, err)
				assert.Contains(t, hint, tt.refused)
				assert.Contains(t, hint, tt.tag)
				assert.Contains(t, hint, "publisher of "+e2eToolName)
				assert.NotContains(t, hint, "--", "the hint never offers a way round")
				requireInstalled(t, s, oldBinary)

				return
			}

			require.NoError(t, err)
			requireInstalled(t, s, tt.binary)

			for _, w := range []string{unchecked, installed} {
				assert.Equal(t, w == tt.warns, warned(captured, w), w)
			}
		})
	}
}

func warned(h *logger.CaptureHandler, text string) bool {
	for _, r := range h.Records() {
		if r.Level == slog.LevelWarn && strings.Contains(r.Message, text) {
			return true
		}
	}

	return false
}

// The warning names both versions, before the binary is replaced.
func TestUpdate_AllowedMismatchNamesBothVersions(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	store := newStaticStore(t)
	stampedRelease(t, store, "v1.1.0", binversiontest.Binary(t, "v0.9.0", binversiontest.Tagged))

	s, captured := stampedUpdater(t, store, true)
	WithVersionMismatchAllowed()(s)

	_, err := s.Update(t.Context())
	require.NoError(t, err)

	for _, r := range captured.Records() {
		if !strings.Contains(r.Message, "not the version asked for") {
			continue
		}

		attrs := map[string]string{}
		r.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value.String()

			return true
		})

		assert.Equal(t, map[string]string{"expected": "v1.1.0", "recorded": "v0.9.0"}, attrs)

		return
	}

	t.Fatal("no warning")
}

// Allowing a mismatch leaves every other check standing (spec 0220 D8).
func TestUpdate_AllowedMismatchMeetsTheOtherChecks(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	t.Run("the downgrade guard", func(t *testing.T) {
		t.Parallel()

		store := newStaticStore(t)
		stampedRelease(t, store, "v0.9.0", binversiontest.Binary(t, "v0.9.0", binversiontest.Tagged))

		s, _ := stampedUpdater(t, store, true)
		WithVersionMismatchAllowed()(s)

		_, err := s.Update(t.Context())
		require.Error(t, err)
		requireInstalled(t, s, oldBinary)
	})

	t.Run("the checksum", func(t *testing.T) {
		t.Parallel()

		archive := forgetest.TarGzAsset(e2eToolName, e2eToolName, string(binversiontest.Binary(t, "v0.9.0", binversiontest.Tagged)))
		s, _ := newE2EUpdater(t, forgetest.New(forgetest.WithRelease("v1.1.0", archive)))
		s.requireChecksum = true
		WithVersionMismatchAllowed()(s)

		_, err := s.Update(t.Context())
		require.ErrorContains(t, err, "no checksums manifest found")
		requireInstalled(t, s, oldBinary)
	})

	t.Run("the signature", func(t *testing.T) {
		t.Parallel()

		archive := forgetest.TarGzAsset(e2eToolName, e2eToolName, string(binversiontest.Binary(t, "v0.9.0", binversiontest.Tagged)))
		sums := forgetest.Manifest(false, archive)
		store := newStaticStore(t)
		store.publish(t, "v1.1.0", archive, sums, forgetest.SignatureAsset(testEd25519.entity, sums, true).Body)

		s, _ := stampedUpdater(t, store, true)
		WithVersionMismatchAllowed()(s)

		_, err := s.Update(t.Context())
		require.ErrorIs(t, err, verify.ErrSignatureInvalid)
		requireInstalled(t, s, oldBinary)
	})
}

// A version asked for is what the binary must be (spec 0220 D3).
func TestUpdate_ToAVersion(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	old := binversiontest.Binary(t, "v0.9.0", binversiontest.Tagged)
	newer := binversiontest.Binary(t, "v1.1.0", binversiontest.Tagged)

	newStore := func(t *testing.T) *staticStore {
		t.Helper()

		store := newStaticStore(t)
		stampedRelease(t, store, "v0.9.0", old)
		stampedRelease(t, store, "v1.1.0", newer)

		return store
	}

	t.Run("an older one installs", func(t *testing.T) {
		t.Parallel()

		s, _ := stampedUpdater(t, newStore(t), true)
		s.version = "v0.9.0"

		_, err := s.Update(t.Context())
		require.NoError(t, err)
		requireInstalled(t, s, old)
	})

	t.Run("a self-consistent other release is refused", func(t *testing.T) {
		t.Parallel()

		for _, allow := range []bool{false, true} {
			store := newStore(t)
			prefix := "/acme/" + e2eToolName + "/"
			store.objects[prefix+"v1.5.0/"+static.ManifestFile] = store.objects[prefix+"v0.9.0/"+static.ManifestFile]

			s, _ := stampedUpdater(t, store, true)
			s.version = "v1.5.0"
			s.allowVersionMismatch = allow

			_, err := s.Update(t.Context())
			require.ErrorIs(t, err, ErrVersionMismatch)

			hint := hintOf(t, err)
			assert.Contains(t, hint, "v1.5.0")
			assert.Contains(t, hint, "v0.9.0")
			requireInstalled(t, s, oldBinary)
		}
	})

	t.Run("another release's archive under its tag is refused", func(t *testing.T) {
		t.Parallel()

		store := newStore(t)
		prefix := "/acme/" + e2eToolName + "/"

		var m static.Manifest
		require.NoError(t, json.Unmarshal(store.objects[prefix+"v0.9.0/"+static.ManifestFile], &m))
		m.Tag = "v1.5.0"

		raw, err := json.Marshal(m)
		require.NoError(t, err)
		store.objects[prefix+"v1.5.0/"+static.ManifestFile] = raw

		s, _ := stampedUpdater(t, store, true)
		s.version = "v1.5.0"

		_, err = s.Update(t.Context())
		require.ErrorIs(t, err, ErrVersionMismatch)
		requireInstalled(t, s, oldBinary)
	})
}

// The forge channel meets the same two checks.
func TestUpdate_OnAForge_ChecksTheVersion(t *testing.T) {
	t.Parallel()

	old := binversiontest.Binary(t, "v0.9.0", binversiontest.Tagged)
	asset := forgetest.TarGzAsset(e2eToolName, e2eToolName, string(old))

	t.Run("the binary's", func(t *testing.T) {
		t.Parallel()

		s, _ := newE2EUpdater(t, forgetest.New(forgetest.WithRelease("v1.1.0", asset, forgetest.ChecksumsAsset(false, asset))))

		_, err := s.Update(t.Context())
		require.ErrorIs(t, err, ErrVersionMismatch)
		requireInstalled(t, s, oldBinary)
	})

	t.Run("the release's", func(t *testing.T) {
		t.Parallel()

		for _, set := range []func(*SelfUpdater){
			func(*SelfUpdater) {},
			func(s *SelfUpdater) { s.force = true },
			WithVersionMismatchAllowed(),
		} {
			provider := forgetest.New(forgetest.WithRelease("v0.9.0", asset, forgetest.ChecksumsAsset(false, asset)))
			s, _ := newE2EUpdater(t, otherRelease{provider})
			s.version = "v1.1.0"
			set(s)

			_, err := s.Update(t.Context())
			require.ErrorIs(t, err, ErrVersionMismatch)
			assert.Contains(t, hintOf(t, err), "the release is tagged v0.9.0")
			assert.Nil(t, s.NextRelease, "a refused release is not kept")
			requireInstalled(t, s, oldBinary)
		}
	})

	t.Run("a provider that answers with nothing", func(t *testing.T) {
		t.Parallel()

		for _, set := range []struct{ force, allow bool }{{}, {force: true}, {allow: true}, {force: true, allow: true}} {
			provider := forgetest.New(forgetest.WithRelease("v0.9.0", asset, forgetest.ChecksumsAsset(false, asset)))
			s, _ := newE2EUpdater(t, noRelease{provider})
			s.version, s.force, s.allowVersionMismatch = "v1.1.0", set.force, set.allow

			_, err := s.Update(t.Context())
			require.ErrorIs(t, err, forge.ErrNotFound)
			require.ErrorContains(t, err, "v1.1.0")
			assert.Contains(t, hintOf(t, err), "was not found")
			requireInstalled(t, s, oldBinary)
		}
	})
}

// A tag that is not a version, such as Bitbucket's upload time for a download
// with none in its name, is checked no more than a binary that records none (spec 0220 D7).
func TestUpdate_ATagThatIsNotAVersion(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	const tag = "2026-11-24T06:27:32Z"

	binary := binversiontest.Binary(t, "v1.1.0", binversiontest.Tagged)
	archive := forgetest.TarGzAsset(e2eToolName, e2eToolName, string(binary))
	sums := forgetest.Manifest(false, archive)

	for name, tt := range map[string]struct {
		asked         string
		signed, allow bool
		refused       bool
	}{
		"latest, unsigned":        {},
		"latest, signed":          {signed: true, refused: true},
		"latest, signed, allowed": {signed: true, allow: true, refused: true},
		"asked for, unsigned":     {asked: tag},
		"asked for, signed":       {asked: tag, signed: true, refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s, _ := newE2EUpdater(t, forgetest.New(forgetest.WithRelease(tag, archive,
				forgetest.Asset{Name: "checksums.txt", Body: sums},
				forgetest.SignatureAsset(testEd25519.entity, sums, false))))
			s.version, s.force, s.allowVersionMismatch = tt.asked, true, tt.allow
			s.requireSignature = tt.signed
			s.embeddedKeys = [][]byte{testEd25519.armoredPub}
			s.keySource = verify.DefaultKeySource
			require.NoError(t, s.buildDefaultKeyResolver())

			captured := logger.NewCaptureHandler()
			s.logger = slog.New(captured)

			_, err := s.Update(t.Context())
			if tt.refused {
				require.ErrorIs(t, err, ErrVersionMismatch)
				assert.Contains(t, hintOf(t, err), "the update was for "+tag+", and that tag is not a version")
				requireInstalled(t, s, oldBinary)

				return
			}

			require.NoError(t, err)
			assert.True(t, warned(captured, "that tag is not a version"))
			requireInstalled(t, s, binary)
		})
	}
}

// noRelease finds no release by tag, and says so with no error.
type noRelease struct{ forge.Provider }

func (noRelease) GetReleaseByTag(context.Context, string, string, string) (forge.Release, error) {
	return nil, nil
}

// otherRelease answers a request for any tag with the latest release.
type otherRelease struct{ forge.Provider }

func (p otherRelease) GetReleaseByTag(ctx context.Context, owner, repo, _ string) (forge.Release, error) {
	return p.GetLatestRelease(ctx, owner, repo)
}

// A tag that is not a version still names its own release, and v is optional.
func TestRefuseOtherRelease(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		asked, tagged string
		refused       bool
	}{
		"the tag asked for":             {asked: "v1.1.0", tagged: "v1.1.0"},
		"one of them without the v":     {asked: "v1.1.0", tagged: "1.1.0"},
		"a tag that is not a version":   {asked: "nightly", tagged: "nightly"},
		"another":                       {asked: "v1.1.0", tagged: "v0.9.0", refused: true},
		"another that is not a version": {asked: "nightly", tagged: "weekly", refused: true},
		"its prerelease":                {asked: "v1.1.0", tagged: "v1.1.0-rc.1", refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rel, err := forgetest.New(forgetest.WithRelease(tt.tagged)).GetReleaseByTag(t.Context(), "", "", tt.tagged)
			require.NoError(t, err)

			s := &SelfUpdater{version: tt.asked}

			err = s.refuseOtherRelease(rel)
			if tt.refused {
				require.ErrorIs(t, err, ErrVersionMismatch)

				return
			}

			require.NoError(t, err)
		})
	}
}

// What was asked for is compared, not what the release says it is (spec 0220 D3).
func TestWantedVersion(t *testing.T) {
	t.Parallel()

	rel, err := forgetest.New(forgetest.WithRelease("v1.1.0")).GetReleaseByTag(t.Context(), "", "", "v1.1.0")
	require.NoError(t, err)

	assert.Equal(t, "v0.9.0", (&SelfUpdater{version: "v0.9.0"}).wantedVersion(rel))
	assert.Equal(t, "v1.1.0", (&SelfUpdater{}).wantedVersion(rel))
}

// A local archive has no tag to compare with, so any version installs.
func TestUpdateFromFile_IsNotVersionChecked(t *testing.T) {
	t.Parallel()

	const toolName = "test-tool"

	fs := afero.NewMemMapFs()
	updater := setupOfflineUpdater(t, fs, toolName)
	updater.CurrentVersion = "v1.1.0"
	binary := binversiontest.Binary(t, "v0.9.0", binversiontest.Tagged)

	require.NoError(t, afero.WriteFile(fs, "/tmp/release.tar.gz", createTarGz(t, toolName, string(binary)), 0o644))

	target, err := updater.UpdateFromFile(t.Context(), "/tmp/release.tar.gz")
	require.NoError(t, err)

	got, err := afero.ReadFile(fs, target)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(binary, got))
}
