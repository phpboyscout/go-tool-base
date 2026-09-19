package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/forge"
	forgetest "gitlab.com/phpboyscout/go/forge/test"
	"gitlab.com/phpboyscout/go/signing/verify"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
)

// staticStore is a static release channel on an httptest.Server, published
// the way the layout reference says: archives, checksums (and signature),
// then the manifest, then the pointer. Tests build releases from the same
// forgetest assets the forge-branch tests use.
type staticStore struct {
	*httptest.Server
	objects map[string][]byte
	latest  string
}

func newStaticStore(t *testing.T) *staticStore {
	t.Helper()

	s := &staticStore{objects: map[string][]byte{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := s.objects[r.URL.Path]
		if !ok {
			http.NotFound(w, r)

			return
		}

		_, _ = w.Write(body)
	}))
	t.Cleanup(s.Close)

	return s
}

func (s *staticStore) base() string { return s.URL + "/acme/" + e2eToolName }

// publish puts one release on the store: the archive for this test's own
// platform, the checksums file, an optional signature, its manifest chained
// to the previous tag, and moves the pointer.
func (s *staticStore) publish(t *testing.T, tag string, archive forgetest.Asset, checksums []byte, signature []byte) {
	t.Helper()

	prefix := "/acme/" + e2eToolName + "/" + tag + "/"
	s.objects[prefix+archive.Name] = archive.Body
	s.objects[prefix+static.ChecksumsFile] = checksums

	sum := sha256.Sum256(archive.Body)
	m := static.Manifest{
		Schema:     static.SchemaVersion,
		Tool:       e2eToolName,
		Tag:        tag,
		ReleasedAt: "2026-09-19T12:00:00Z",
		Previous:   s.latest,
		Checksums:  static.FileURL(s.base(), tag, static.ChecksumsFile),
		Notes:      "notes for " + tag,
		Downloads: []static.Download{{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Name: archive.Name,
			URL: static.FileURL(s.base(), tag, archive.Name), Size: int64(len(archive.Body)), SHA256: hex.EncodeToString(sum[:]),
		}},
	}

	if signature != nil {
		s.objects[prefix+static.SignatureFile] = signature
		m.Signature = static.FileURL(s.base(), tag, static.SignatureFile)
	}

	raw, err := json.Marshal(m)
	require.NoError(t, err)
	s.objects[prefix+static.ManifestFile] = raw

	raw, err = json.Marshal(static.Pointer{
		Schema: static.SchemaVersion, Tool: e2eToolName, Tag: tag,
		Manifest: static.ManifestURL(s.base(), tag), PublishedAt: "2026-09-19T12:20:00Z",
	})
	require.NoError(t, err)
	s.objects["/acme/"+e2eToolName+"/"+static.PointerFile] = raw
	s.latest = tag
}

func TestStaticChannel_PresentsManifestsAsReleases(t *testing.T) {
	t.Parallel()

	store := newStaticStore(t)
	old := forgetest.TarGzAsset(e2eToolName, e2eToolName, "old-binary")
	store.publish(t, "v1.0.0", old, forgetest.Manifest(false, old), nil)
	archive := forgetest.TarGzAsset(e2eToolName, e2eToolName, "new-binary")
	store.publish(t, "v1.1.0", archive, forgetest.Manifest(false, archive), []byte("sig"))

	ch, err := newStaticChannel(store.base(), store.Client())
	require.NoError(t, err)

	ctx := context.Background()

	latest, err := ch.Latest(ctx)
	require.NoError(t, err)
	assert.Equal(t, "v1.1.0", latest.GetTagName())
	assert.Equal(t, "v1.1.0", latest.GetName())
	assert.Equal(t, "notes for v1.1.0", latest.GetBody())
	assert.False(t, latest.GetDraft())
	assert.Equal(t, "2026-09-19T12:00:00Z", latest.GetReleasedAt().UTC().Format("2006-01-02T15:04:05Z"))

	names := make([]string, 0, len(latest.GetAssets()))
	for _, a := range latest.GetAssets() {
		names = append(names, a.GetName())
	}

	assert.Equal(t, []string{archive.Name, "checksums.txt", "checksums.txt.sig"}, names, "downloads, then checksums, then the signature")

	byTag, err := ch.ByTag(ctx, "v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", byTag.GetTagName())
	assert.Len(t, byTag.GetAssets(), 2, "an unsigned release lists no signature asset")

	list, err := ch.List(ctx, 0)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "v1.1.0", list[0].GetTagName(), "newest first, as the forge providers list")

	sums, err := ch.Checksums(ctx, latest, forge.DefaultMaxChecksumsSize)
	require.NoError(t, err)
	assert.Equal(t, forgetest.Manifest(false, archive), sums)

	sig, err := ch.Signature(ctx, latest, forge.DefaultMaxSignatureSize)
	require.NoError(t, err)
	assert.Equal(t, "sig", string(sig))

	_, err = ch.Signature(ctx, byTag, forge.DefaultMaxSignatureSize)
	require.True(t, errors.Is(err, forge.ErrNotSupported), "no signature is not-supported, the answer a forge without one gives: %v", err)

	// A release or asset from another channel is refused, not guessed at.
	foreign := forgetest.New(forgetest.WithRelease("v1.1.0", archive))
	foreignRel, err := foreign.GetLatestRelease(ctx, "acme", e2eToolName)
	require.NoError(t, err)

	_, err = ch.Checksums(ctx, foreignRel, forge.DefaultMaxChecksumsSize)
	require.True(t, errors.Is(err, forge.ErrNotSupported))

	_, _, err = ch.Download(ctx, foreignRel.GetAssets()[0])
	require.Error(t, err)
}

func TestStaticChannel_ListReturnsWhatItReadBeforeABreak(t *testing.T) {
	t.Parallel()

	store := newStaticStore(t)
	old := forgetest.TarGzAsset(e2eToolName, e2eToolName, "old-binary")
	store.publish(t, "v1.0.0", old, forgetest.Manifest(false, old), nil)
	archive := forgetest.TarGzAsset(e2eToolName, e2eToolName, "new-binary")
	store.publish(t, "v1.1.0", archive, forgetest.Manifest(false, archive), nil)
	delete(store.objects, "/acme/"+e2eToolName+"/v1.0.0/"+static.ManifestFile)

	ch, err := newStaticChannel(store.base(), store.Client())
	require.NoError(t, err)

	list, err := ch.List(context.Background(), 0)
	require.NoError(t, err, "the releases that resolved are real; the break is the store's problem, logged by the reader's caller")
	require.Len(t, list, 1)
	assert.Equal(t, "v1.1.0", list[0].GetTagName())

	store = newStaticStore(t)

	ch, err = newStaticChannel(store.base(), store.Client())
	require.NoError(t, err)

	_, err = ch.List(context.Background(), 0)
	require.ErrorIs(t, err, static.ErrNoReleasesPublished)
}

// The whole update runs on the static channel: the pointer names the
// release, the manifest picks the platform, the checksums verify the archive,
// and the binary is replaced. No forge provider exists in this test.
func TestUpdate_OnTheStaticChannel(t *testing.T) {
	t.Parallel()

	store := newStaticStore(t)
	archive := forgetest.TarGzAsset(e2eToolName, e2eToolName, "new-binary")
	store.publish(t, "v1.1.0", archive, forgetest.Manifest(false, archive), nil)

	ch, err := newStaticChannel(store.base(), store.Client())
	require.NoError(t, err)

	s, currentBin := newE2EUpdater(t, nil)
	withReleaseChannel(ch)(s)
	s.requireChecksum = true

	path, err := s.Update(context.Background())
	require.NoError(t, err)
	assert.Equal(t, currentBin, path)

	got, err := afero.ReadFile(s.Fs, currentBin)
	require.NoError(t, err)
	assert.Equal(t, "new-binary", string(got))
}

func TestUpdate_OnTheStaticChannel_VerifiesTheSignature(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	archive := forgetest.TarGzAsset(e2eToolName, e2eToolName, "new-binary")
	manifest := forgetest.Manifest(false, archive)

	t.Run("a good signature installs", func(t *testing.T) {
		t.Parallel()

		store := newStaticStore(t)
		store.publish(t, "v1.1.0", archive, manifest, forgetest.SignatureAsset(testEd25519.entity, manifest, false).Body)

		s, currentBin := signedStaticUpdater(t, store)

		_, err := s.Update(context.Background())
		require.NoError(t, err)

		got, err := afero.ReadFile(s.Fs, currentBin)
		require.NoError(t, err)
		assert.Equal(t, "new-binary", string(got))
	})

	t.Run("a bad signature leaves the binary alone", func(t *testing.T) {
		t.Parallel()

		store := newStaticStore(t)
		store.publish(t, "v1.1.0", archive, manifest, forgetest.SignatureAsset(testEd25519.entity, manifest, true).Body)

		s, currentBin := signedStaticUpdater(t, store)

		_, err := s.Update(context.Background())
		require.Error(t, err)

		got, err := afero.ReadFile(s.Fs, currentBin)
		require.NoError(t, err)
		assert.Equal(t, "old-binary", string(got))
	})

	t.Run("a required signature the manifest does not name is refused", func(t *testing.T) {
		t.Parallel()

		store := newStaticStore(t)
		store.publish(t, "v1.1.0", archive, manifest, nil)

		s, currentBin := signedStaticUpdater(t, store)

		_, err := s.Update(context.Background())
		require.Error(t, err)

		got, err := afero.ReadFile(s.Fs, currentBin)
		require.NoError(t, err)
		assert.Equal(t, "old-binary", string(got))
	})
}

func signedStaticUpdater(t *testing.T, store *staticStore) (*SelfUpdater, string) {
	t.Helper()

	ch, err := newStaticChannel(store.base(), store.Client())
	require.NoError(t, err)

	s, currentBin := newE2EUpdater(t, nil)
	withReleaseChannel(ch)(s)
	s.requireChecksum = true
	s.requireSignature = true
	s.embeddedKeys = [][]byte{testEd25519.armoredPub}
	s.keySource = verify.DefaultKeySource
	require.NoError(t, s.buildDefaultKeyResolver())

	return s, currentBin
}

// A release that does not ship for the running platform is a clear answer,
// found from the manifest's os and arch rather than from an archive name.
func TestFindReleaseAsset_ByPlatform(t *testing.T) {
	t.Parallel()

	m := static.Manifest{Tag: "v1.1.0", Downloads: []static.Download{
		{OS: "plan9", Arch: "mips", Name: "oddly_named.tgz", URL: "https://pkg.example.internal/t/v1.1.0/oddly_named.tgz"},
		{OS: runtime.GOOS, Arch: runtime.GOARCH, Name: "also_odd.tgz", URL: "https://pkg.example.internal/t/v1.1.0/also_odd.tgz"},
	}}

	s := &SelfUpdater{}

	asset, err := s.findReleaseAsset(newStaticRelease(m))
	require.NoError(t, err)
	assert.Equal(t, "also_odd.tgz", asset.GetName())

	m.Downloads = m.Downloads[:1]

	_, err = s.findReleaseAsset(newStaticRelease(m))
	require.Error(t, err)
	assert.Contains(t, err.Error(), runtime.GOOS+"/"+runtime.GOARCH)
}

// NewUpdater builds the static branch from the tool's release source alone:
// no forge factory is looked up, no credential gate runs (Private is
// meaningless on a public location), and no config key is read for it.
func TestNewUpdater_StaticReleaseSource(t *testing.T) {
	t.Parallel()

	store := newStaticStore(t)
	archive := forgetest.TarGzAsset(e2eToolName, e2eToolName, "new-binary")
	store.publish(t, "v1.1.0", archive, forgetest.Manifest(false, archive), nil)

	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: testutil.StoreFromYAML(t, "vcs:\n  provider: bogus-provider\n"),
		Tool: props.Tool{
			Name:          e2eToolName,
			ReleaseSource: props.ReleaseSource{Type: props.ReleaseSourceStatic, BaseURL: store.base(), Private: true},
		},
	}

	u, err := NewUpdater(context.Background(), p, "", false)
	require.NoError(t, err, "a bogus vcs.provider and Private are not consulted on the static channel")

	_, isStatic := u.channel.(*staticChannel)
	assert.True(t, isStatic, "the channel is the static branch, not a forge over an injected provider")
	assert.Nil(t, u.releaseClient, "no forge provider was built")

	latest, err := u.GetLatestVersionString(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "v1.1.0", latest)

	// The static type with no base URL cannot be a channel, and says so.
	p.Tool.ReleaseSource.BaseURL = ""

	_, err = NewUpdater(context.Background(), p, "", false)
	require.ErrorIs(t, err, static.ErrInvalidBaseURL)
	assert.Contains(t, hintOf(t, err), "base_url")

	// An injected provider still wins, so a test double runs even on a
	// tool whose source is static.
	p.Tool.ReleaseSource.BaseURL = store.base()
	p.Tool.ReleaseProvider = forgetest.New(forgetest.WithRelease("v9.0.0", archive))

	u, err = NewUpdater(context.Background(), p, "", false)
	require.NoError(t, err)

	latest, err = u.GetLatestVersionString(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "v9.0.0", latest)
}

// The static channel's refusals reach the user with the URL to check, and
// stay apart from the forge outcomes they resemble.
func TestExplainRefusal_StaticChannel(t *testing.T) {
	t.Parallel()

	s := &SelfUpdater{Tool: props.Tool{
		Name:          "mytool",
		ReleaseSource: props.ReleaseSource{Type: props.ReleaseSourceStatic, BaseURL: "https://pkg.example.internal/acme/mytool"},
	}}

	err := s.explainRefusal(context.Background(), errors.Wrap(static.ErrNoReleasesPublished, "x"))
	hint := hintOf(t, err)
	assert.Contains(t, hint, "https://pkg.example.internal/acme/mytool")
	assert.Contains(t, hint, "not a configuration problem")

	err = s.explainRefusal(context.Background(), errors.Wrap(static.ErrReleaseNotFound, "x"))
	assert.Contains(t, hintOf(t, err), "https://pkg.example.internal/acme/mytool/latest.json")

	for _, sentinel := range []error{static.ErrBrokenChain, static.ErrEscapesBase, static.ErrUnsupportedSchema} {
		err = s.explainRefusal(context.Background(), errors.Wrap(sentinel, "x"))
		assert.Contains(t, hintOf(t, err), "publisher", sentinel.Error())
	}
}
