// Package static is the static release channel's shared shape (spec 0203): the
// two documents a release publishes to a plain HTTPS location, one pointer to
// the current release and one immutable manifest per tag, and the layout that
// places them. The writer (cmd/releasemanifest) and the reader (the updater's
// static branch) share these types so they cannot drift; the schema version is
// the contract with any publisher that is not our tool.
package static

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"gitlab.com/phpboyscout/go/errors"
)

// SchemaVersion is the document schema this package writes and the highest it
// reads. A reader refuses a value it does not know rather than guessing.
const SchemaVersion = 1

// The fixed file names: PointerFile under the base URL, the rest under a
// tag's directory. ChecksumsFile and SignatureFile are goreleaser's defaults
// and the layout reference's names; the manifest carries their URLs, so a
// reader never assumes them.
const (
	PointerFile   = "latest.json"
	ManifestFile  = "release.json"
	ChecksumsFile = "checksums.txt"
	SignatureFile = "checksums.txt.sig"
)

// Sentinels, namespaced gtb.release.static.
var (
	// ErrUnsupportedSchema is a document whose schema this reader does not know.
	ErrUnsupportedSchema = errors.NewSentinel("gtb.release.static.unsupported_schema", "unsupported release manifest schema")
	// ErrEscapesBase is a URL in a document that is not under the channel's
	// base URL: a manifest cannot send the tool to a host the author did not name.
	ErrEscapesBase = errors.NewSentinel("gtb.release.static.escapes_base", "release manifest names a URL outside the channel's base")
	// ErrInvalidManifest is a document that parses but is not well formed.
	ErrInvalidManifest = errors.NewSentinel("gtb.release.static.invalid_manifest", "release manifest is not well formed")
)

// Pointer is <base>/latest.json: the one object on the channel that changes
// after it is written. It names the current release and where its manifest is.
type Pointer struct {
	Schema      int    `json:"schema"`
	Tool        string `json:"tool"`
	Tag         string `json:"tag"`
	Manifest    string `json:"manifest"`
	PublishedAt string `json:"published_at"`
}

// Manifest is <base>/<tag>/release.json: everything a tool needs to retrieve
// and verify one release, plus the previous tag so the manifests form a chain
// a reader can walk without a listing.
type Manifest struct {
	Schema     int        `json:"schema"`
	Tool       string     `json:"tool"`
	Tag        string     `json:"tag"`
	ReleasedAt string     `json:"released_at"`
	Previous   string     `json:"previous"`
	Checksums  string     `json:"checksums"`
	Signature  string     `json:"signature,omitempty"`
	Notes      string     `json:"notes,omitempty"`
	Downloads  []Download `json:"downloads"`
}

// Download is one archive for one platform. OS and Arch are Go's GOOS and
// GOARCH; SHA256 is the hex digest checksums.txt also carries.
type Download struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// PointerURL is where the pointer lives under base.
func PointerURL(base string) string { return trimBase(base) + "/" + PointerFile }

// ManifestURL is where tag's manifest lives under base.
func ManifestURL(base, tag string) string { return trimBase(base) + "/" + tag + "/" + ManifestFile }

// FileURL is where one of tag's files lives under base.
func FileURL(base, tag, name string) string { return trimBase(base) + "/" + tag + "/" + name }

func trimBase(base string) string { return strings.TrimRight(base, "/") }

var (
	tagPattern    = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Validate reports whether the pointer is well formed and stays under base.
func (p Pointer) Validate(base string) error {
	if p.Schema != SchemaVersion {
		return errors.Wrapf(ErrUnsupportedSchema, "pointer schema %d, this reader knows %d", p.Schema, SchemaVersion)
	}

	if p.Tool == "" || !tagPattern.MatchString(p.Tag) || p.Manifest == "" {
		return errors.Wrap(ErrInvalidManifest, "pointer needs tool, a semver tag and a manifest URL")
	}

	return underBase(base, p.Manifest, "pointer manifest")
}

// Validate reports whether the manifest is well formed and every URL in it
// stays under base. It is the check a reader runs before any fetch.
func (m Manifest) Validate(base string) error {
	if m.Schema != SchemaVersion {
		return errors.Wrapf(ErrUnsupportedSchema, "manifest schema %d, this reader knows %d", m.Schema, SchemaVersion)
	}

	if err := m.validateHeader(); err != nil {
		return err
	}

	if err := underBase(base, m.Checksums, "checksums"); err != nil {
		return err
	}

	if m.Signature != "" {
		if err := underBase(base, m.Signature, "signature"); err != nil {
			return err
		}
	}

	if len(m.Downloads) == 0 {
		return errors.Wrap(ErrInvalidManifest, "manifest lists no downloads")
	}

	for _, d := range m.Downloads {
		if err := d.validate(base); err != nil {
			return err
		}
	}

	return nil
}

// validateHeader checks the fields that need no base URL.
func (m Manifest) validateHeader() error {
	if m.Tool == "" || !tagPattern.MatchString(m.Tag) {
		return errors.Wrap(ErrInvalidManifest, "manifest needs tool and a semver tag")
	}

	if m.Previous != "" && !tagPattern.MatchString(m.Previous) {
		return errors.Wrapf(ErrInvalidManifest, "previous %q is not a tag", m.Previous)
	}

	if m.Checksums == "" {
		return errors.Wrap(ErrInvalidManifest, "manifest names no checksums")
	}

	return nil
}

func (d Download) validate(base string) error {
	if d.OS == "" || d.Arch == "" || d.Name == "" || d.URL == "" || d.Size <= 0 {
		return errors.Wrapf(ErrInvalidManifest, "download %q needs os, arch, name, url and a positive size", d.Name)
	}

	if !sha256Pattern.MatchString(d.SHA256) {
		return errors.Wrapf(ErrInvalidManifest, "download %q: sha256 is not 64 hex characters", d.Name)
	}

	return underBase(base, d.URL, "download "+d.Name)
}

// underBase requires raw to share base's scheme and host and to sit under its
// path. Scheme is compared too, so an https base cannot be walked down to http.
func underBase(base, raw, what string) error {
	b, err := url.Parse(trimBase(base))
	if err != nil {
		return errors.Wrapf(ErrInvalidManifest, "base URL %q: %v", base, err)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return errors.Wrapf(ErrInvalidManifest, "%s URL %q: %v", what, raw, err)
	}

	if u.Scheme != b.Scheme || u.Host != b.Host || !strings.HasPrefix(u.Path, b.Path+"/") {
		return errors.Wrapf(ErrEscapesBase, "%s %q is not under %q", what, raw, base)
	}

	return nil
}

// DecodeManifest parses and validates a manifest fetched from under base.
func DecodeManifest(raw []byte, base string) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, errors.Wrapf(ErrInvalidManifest, "parsing release manifest: %v", err)
	}

	if err := m.Validate(base); err != nil {
		return Manifest{}, err
	}

	return m, nil
}

// DecodePointer parses and validates a pointer fetched from under base.
func DecodePointer(raw []byte, base string) (Pointer, error) {
	var p Pointer
	if err := json.Unmarshal(raw, &p); err != nil {
		return Pointer{}, errors.Wrapf(ErrInvalidManifest, "parsing release pointer: %v", err)
	}

	if err := p.Validate(base); err != nil {
		return Pointer{}, err
	}

	return p, nil
}
