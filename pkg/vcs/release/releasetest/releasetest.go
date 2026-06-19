// Package releasetest provides an in-memory [release.Provider] test double and
// asset builders for exercising the self-update pipeline without network or
// disk access. It serves releases, tar.gz binary assets, checksum manifests
// and detached signatures entirely from memory, including the
// security-relevant corrupt-checksum and bad-signature variants.
//
// The double is public (pkg/) so downstream tools building on GTB can test
// their own self-update wiring hermetically — the same reasoning that keeps
// [release.Provider] public. It is usable both from tests and at runtime (the
// builders take no *testing.T), which lets the e2e binary inject a stub
// release source behind an env gate.
//
// A [Source] always satisfies [release.ChecksumProvider] and
// [release.SignatureProvider], but returns [release.ErrNotSupported] unless
// configured with [WithChecksumManifest] / [WithSignatureManifest]. Per the
// provider contract, the update flow treats [release.ErrNotSupported] exactly
// like "provider does not implement this interface" and falls back to locating
// checksums.txt / checksums.txt.sig by filename in the asset list — so a Source
// exercises both the manifest-provider path and the asset-by-name fallback.
//
// See docs/development/specs/2026-06-19-injectable-release-source.md.
package releasetest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"runtime"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/cockroachdb/errors"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// tarEntryMode is the executable file mode recorded for the single binary
// entry in a generated tar.gz asset.
const tarEntryMode = 0o755

// Asset is a single downloadable release asset served verbatim by a [Source].
type Asset struct {
	Name string
	Body []byte
}

// AssetName returns the OS/arch release-asset name the updater's
// findReleaseAsset expects for toolName on the current platform — e.g.
// "mytool_Linux_x86_64.tar.gz". It mirrors the naming convention in
// pkg/setup so a built asset is the one Update will look for.
func AssetName(toolName string) string {
	targetOS := cases.Title(language.Und).String(runtime.GOOS)

	targetArch := runtime.GOARCH
	if targetArch == "amd64" {
		targetArch = "x86_64"
	}

	return fmt.Sprintf("%s_%s_%s.tar.gz", toolName, targetOS, targetArch)
}

// TarGzAsset builds the platform release-binary asset for toolName: its Name is
// [AssetName](toolName) and its Body is a .tar.gz archive whose single entry is
// binName with binBody as its contents.
func TarGzAsset(toolName, binName, binBody string) Asset {
	return Asset{Name: AssetName(toolName), Body: tarGz(binName, binBody)}
}

// Manifest builds a GoReleaser-style checksums.txt over the given assets
// (one "sha256  name" line each). When corrupt is true the recorded hashes
// intentionally cover a different payload, so verification against the real
// asset bodies fails.
func Manifest(corrupt bool, over ...Asset) []byte {
	var b bytes.Buffer

	for _, a := range over {
		body := a.Body
		if corrupt {
			body = append([]byte("corrupt"), body...)
		}

		sum := sha256.Sum256(body)
		_, _ = fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), a.Name)
	}

	return b.Bytes()
}

// ChecksumsAsset wraps [Manifest] as a "checksums.txt" asset.
func ChecksumsAsset(corrupt bool, over ...Asset) Asset {
	return Asset{Name: "checksums.txt", Body: Manifest(corrupt, over...)}
}

// SignatureAsset builds a "checksums.txt.sig" ASCII-armored detached signature
// over manifest using entity. When bad is true it signs a different payload, so
// the signature does not verify against the served manifest.
func SignatureAsset(entity *openpgp.Entity, manifest []byte, bad bool) Asset {
	signed := manifest
	if bad {
		signed = []byte("a different manifest entirely\n")
	}

	var buf bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&buf, entity, bytes.NewReader(signed), nil); err != nil {
		panic(fmt.Sprintf("releasetest: sign manifest: %v", err)) // in-memory sign cannot fail in practice
	}

	return Asset{Name: "checksums.txt.sig", Body: buf.Bytes()}
}

// tarGz builds a single-entry gzip-compressed tar archive in memory. The writes
// target a bytes.Buffer and cannot fail in practice; a non-nil error indicates
// a programming bug and panics.
func tarGz(name, body string) []byte {
	var buf bytes.Buffer

	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: tarEntryMode, Size: int64(len(body))}); err != nil {
		panic(fmt.Sprintf("releasetest: tar header: %v", err))
	}

	if _, err := tw.Write([]byte(body)); err != nil {
		panic(fmt.Sprintf("releasetest: tar write: %v", err))
	}

	if err := tw.Close(); err != nil {
		panic(fmt.Sprintf("releasetest: tar close: %v", err))
	}

	if err := gw.Close(); err != nil {
		panic(fmt.Sprintf("releasetest: gzip close: %v", err))
	}

	return buf.Bytes()
}

// Source is a fully in-memory [release.Provider] for tests and the e2e stub.
// Construct it with [New] and one or more [Option]s.
type Source struct {
	releases    map[string]release.Release
	order       []string
	latestTag   string
	missing     map[string]struct{}
	bodies      map[string][]byte
	manifest    []byte
	signature   []byte
	downloadErr error
}

// Option configures a [Source].
type Option func(*Source)

// New constructs a Source from the given options.
func New(opts ...Option) *Source {
	s := &Source{
		releases: map[string]release.Release{},
		missing:  map[string]struct{}{},
		bodies:   map[string][]byte{},
	}

	for _, o := range opts {
		o(s)
	}

	return s
}

// WithRelease registers a release at tag exposing the given assets. The first
// registered release becomes the "latest" unless [WithLatestTag] overrides it.
func WithRelease(tag string, assets ...Asset) Option {
	return func(s *Source) {
		ra := make([]release.ReleaseAsset, 0, len(assets))

		for _, a := range assets {
			s.bodies[a.Name] = a.Body
			ra = append(ra, &asset{name: a.Name})
		}

		s.releases[tag] = &rel{tag: tag, assets: ra}
		s.order = append(s.order, tag)

		if s.latestTag == "" {
			s.latestTag = tag
		}
	}
}

// WithLatestTag sets which registered tag [Source.GetLatestRelease] returns.
func WithLatestTag(tag string) Option { return func(s *Source) { s.latestTag = tag } }

// WithMissingTag makes [Source.GetReleaseByTag](tag) return a not-found error
// wrapping [release.ErrReleaseNotFound].
func WithMissingTag(tag string) Option {
	return func(s *Source) { s.missing[tag] = struct{}{} }
}

// WithChecksumManifest puts the Source in checksum manifest-provider mode: it
// implements [release.ChecksumProvider] by returning these bytes instead of
// the default asset-by-name fallback.
func WithChecksumManifest(manifest []byte) Option {
	return func(s *Source) { s.manifest = manifest }
}

// WithSignatureManifest puts the Source in signature manifest-provider mode:
// it implements [release.SignatureProvider] by returning these bytes instead
// of the default asset-by-name fallback.
func WithSignatureManifest(signature []byte) Option {
	return func(s *Source) { s.signature = signature }
}

// WithDownloadError makes [Source.DownloadReleaseAsset] fail with err.
func WithDownloadError(err error) Option {
	return func(s *Source) { s.downloadErr = err }
}

// GetLatestRelease returns the release at the configured latest tag.
func (s *Source) GetLatestRelease(_ context.Context, _, _ string) (release.Release, error) {
	if s.latestTag == "" {
		return nil, errors.WithStack(release.ErrReleaseNotFound)
	}

	r, ok := s.releases[s.latestTag]
	if !ok {
		return nil, errors.Wrapf(release.ErrReleaseNotFound, "latest tag %q", s.latestTag)
	}

	return r, nil
}

// GetReleaseByTag returns the release registered at tag, or a not-found error
// when the tag is unknown or was marked missing.
func (s *Source) GetReleaseByTag(_ context.Context, _, _, tag string) (release.Release, error) {
	if _, gone := s.missing[tag]; gone {
		return nil, errors.Wrapf(release.ErrReleaseNotFound, "tag %q", tag)
	}

	r, ok := s.releases[tag]
	if !ok {
		return nil, errors.Wrapf(release.ErrReleaseNotFound, "tag %q", tag)
	}

	return r, nil
}

// ListReleases returns the registered releases in registration order, capped at
// limit when limit > 0.
func (s *Source) ListReleases(_ context.Context, _, _ string, limit int) ([]release.Release, error) {
	out := make([]release.Release, 0, len(s.order))

	for _, tag := range s.order {
		out = append(out, s.releases[tag])
		if limit > 0 && len(out) >= limit {
			break
		}
	}

	return out, nil
}

// DownloadReleaseAsset returns the in-memory body registered for the asset.
func (s *Source) DownloadReleaseAsset(_ context.Context, _, _ string, a release.ReleaseAsset) (io.ReadCloser, string, error) {
	if s.downloadErr != nil {
		return nil, "", s.downloadErr
	}

	body, ok := s.bodies[a.GetName()]
	if !ok {
		return nil, "", errors.Newf("releasetest: no body for asset %q", a.GetName())
	}

	return io.NopCloser(bytes.NewReader(body)), "", nil
}

// DownloadChecksumManifest implements [release.ChecksumProvider]. It returns
// [release.ErrNotSupported] unless the Source was built with
// [WithChecksumManifest], so the default Source uses the asset-by-name
// fallback.
func (s *Source) DownloadChecksumManifest(_ context.Context, _ release.Release, _ int64) ([]byte, error) {
	if s.manifest == nil {
		return nil, release.ErrNotSupported
	}

	return s.manifest, nil
}

// DownloadSignature implements [release.SignatureProvider]. It returns
// [release.ErrNotSupported] unless the Source was built with
// [WithSignatureManifest], so the default Source uses the asset-by-name
// fallback.
func (s *Source) DownloadSignature(_ context.Context, _ release.Release, _ int64) ([]byte, error) {
	if s.signature == nil {
		return nil, release.ErrNotSupported
	}

	return s.signature, nil
}

// asset implements [release.ReleaseAsset].
type asset struct {
	id   int64
	name string
	url  string
}

func (a *asset) GetID() int64                  { return a.id }
func (a *asset) GetName() string               { return a.name }
func (a *asset) GetBrowserDownloadURL() string { return a.url }

// rel implements [release.Release].
type rel struct {
	tag    string
	assets []release.ReleaseAsset
}

func (r *rel) GetName() string                   { return r.tag }
func (r *rel) GetTagName() string                { return r.tag }
func (r *rel) GetBody() string                   { return "" }
func (r *rel) GetDraft() bool                    { return false }
func (r *rel) GetAssets() []release.ReleaseAsset { return r.assets }

var (
	_ release.Provider          = (*Source)(nil)
	_ release.ChecksumProvider  = (*Source)(nil)
	_ release.SignatureProvider = (*Source)(nil)
)
