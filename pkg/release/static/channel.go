package static

import (
	"context"
	"io"
	"net/http"
	"net/url"

	"gitlab.com/phpboyscout/go/errors"
)

// Bounds on the channel's documents. The pointer is five fields; a manifest
// grows with its download list and notes. A body past the bound is refused
// rather than read, so a mis-served object cannot be made into a memory bill.
const (
	MaxPointerBytes  = 64 << 10
	MaxManifestBytes = 1 << 20
	// DefaultListLimit is the page List returns for limit <= 0, a match for
	// the forge providers' first page so callers see the same shape.
	DefaultListLimit = 30
)

// Sentinels the reader answers with, namespaced gtb.release.static.
var (
	// ErrInvalidBaseURL: the configured base is not an absolute http(s) URL.
	ErrInvalidBaseURL = errors.NewSentinel("gtb.release.static.invalid_base_url", "release channel base URL must be an absolute http(s) URL")
	// ErrNoReleasesPublished: the pointer does not exist yet.
	ErrNoReleasesPublished = errors.NewSentinel("gtb.release.static.no_releases_published", "no release has been published on this channel yet")
	// ErrReleaseNotFound: no manifest for the tag asked for.
	ErrReleaseNotFound = errors.NewSentinel("gtb.release.static.release_not_found", "no release with that tag on this channel")
	// ErrNotFound: an object a manifest names is not there.
	ErrNotFound = errors.NewSentinel("gtb.release.static.not_found", "the channel has no such object")
	// ErrBrokenChain: a manifest the pointer or a previous link names does not
	// resolve, or the chain loops.
	ErrBrokenChain = errors.NewSentinel("gtb.release.static.broken_chain", "the release chain does not resolve")
	// ErrTooLarge: a document exceeded its bound.
	ErrTooLarge = errors.NewSentinel("gtb.release.static.too_large", "a channel document exceeded its size bound")
	// ErrUnexpectedStatus: a status that is neither success nor 404.
	ErrUnexpectedStatus = errors.NewSentinel("gtb.release.static.unexpected_status", "the channel answered with an unexpected HTTP status")
)

// Channel reads a static release channel (spec 0203 D4): the pointer, the
// manifests it chains to, and the files they name. It knows one base URL and
// trusts nothing that would take it off it, redirects included.
type Channel struct {
	base   string
	client *http.Client
}

// New builds a reader over base. client is copied so its redirect policy can
// be replaced; nil means http.DefaultClient's transport.
func New(base string, client *http.Client) (*Channel, error) {
	b, err := url.Parse(trimBase(base))
	if err != nil || (b.Scheme != "https" && b.Scheme != "http") || b.Host == "" || b.RawQuery != "" || b.Fragment != "" {
		return nil, errors.Wrapf(ErrInvalidBaseURL, "%q", base)
	}

	if client == nil {
		client = http.DefaultClient
	}

	c := &Channel{base: b.String()}

	// A redirect is a URL the document did not name. It is held to the same
	// rule as every URL that was.
	cp := *client
	cp.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		return underBase(c.base, req.URL.String(), "redirect to")
	}
	c.client = &cp

	return c, nil
}

// BaseURL is the base the channel was built over, without a trailing slash.
func (c *Channel) BaseURL() string { return c.base }

// Pointer reads <base>/latest.json. A missing pointer is ErrNoReleasesPublished.
func (c *Channel) Pointer(ctx context.Context) (Pointer, error) {
	raw, err := c.fetch(ctx, PointerURL(c.base), MaxPointerBytes)
	if errors.Is(err, ErrNotFound) {
		return Pointer{}, errors.Wrapf(ErrNoReleasesPublished, "%s", PointerURL(c.base))
	}

	if err != nil {
		return Pointer{}, err
	}

	return DecodePointer(raw, c.base)
}

// Latest is the release the pointer names.
func (c *Channel) Latest(ctx context.Context) (Manifest, error) {
	p, err := c.Pointer(ctx)
	if err != nil {
		return Manifest{}, err
	}

	m, err := c.manifestAt(ctx, p.Manifest)
	if errors.Is(err, ErrNotFound) {
		return Manifest{}, errors.Wrapf(ErrBrokenChain, "pointer %s names manifest %s, which does not resolve", PointerURL(c.base), p.Manifest)
	}

	return m, err
}

// ByTag is one release's manifest, fetched directly. It never reads the
// pointer, so a pinned version costs one request.
func (c *Channel) ByTag(ctx context.Context, tag string) (Manifest, error) {
	if !tagPattern.MatchString(tag) {
		return Manifest{}, errors.Wrapf(ErrInvalidManifest, "%q is not a release tag", tag)
	}

	m, err := c.manifestAt(ctx, ManifestURL(c.base, tag))
	if errors.Is(err, ErrNotFound) {
		return Manifest{}, errors.Wrapf(ErrReleaseNotFound, "%s", ManifestURL(c.base, tag))
	}

	return m, err
}

// List walks the chain from the latest release, newest first, up to limit
// manifests (DefaultListLimit for limit <= 0). A link that does not resolve
// or a chain that loops ends the walk with ErrBrokenChain and whatever was
// read before it.
func (c *Channel) List(ctx context.Context, limit int) ([]Manifest, error) {
	if limit <= 0 {
		limit = DefaultListLimit
	}

	latest, err := c.Latest(ctx)
	if err != nil {
		return nil, err
	}

	out := []Manifest{latest}
	seen := map[string]bool{latest.Tag: true}

	for len(out) < limit && out[len(out)-1].Previous != "" {
		previous := out[len(out)-1].Previous
		if seen[previous] {
			return out, errors.Wrapf(ErrBrokenChain, "the chain loops: %s names %s, already seen", out[len(out)-1].Tag, previous)
		}

		m, err := c.manifestAt(ctx, ManifestURL(c.base, previous))
		if errors.Is(err, ErrNotFound) {
			return out, errors.Wrapf(ErrBrokenChain, "%s names previous %s, whose manifest does not resolve", out[len(out)-1].Tag, previous)
		}

		if err != nil {
			return out, err
		}

		seen[previous] = true

		out = append(out, m)
	}

	return out, nil
}

// Fetch reads one object a manifest names, bounded by maxBytes. The URL
// must be under the base, as every URL in a valid manifest is.
func (c *Channel) Fetch(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	if err := underBase(c.base, rawURL, "object"); err != nil {
		return nil, err
	}

	return c.fetch(ctx, rawURL, maxBytes)
}

// Open streams one object a manifest names; the caller bounds and closes
// it. The URL must be under the base.
func (c *Channel) Open(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	if err := underBase(c.base, rawURL, "object"); err != nil {
		return nil, err
	}

	resp, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

func (c *Channel) manifestAt(ctx context.Context, rawURL string) (Manifest, error) {
	raw, err := c.fetch(ctx, rawURL, MaxManifestBytes)
	if err != nil {
		return Manifest{}, err
	}

	return DecodeManifest(raw, c.base)
}

// fetch reads a whole object of at most maxBytes; one byte more is ErrTooLarge.
func (c *Channel) fetch(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	resp, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, errors.Wrapf(err, "reading %s", rawURL)
	}

	if int64(len(raw)) > maxBytes {
		return nil, errors.Wrapf(ErrTooLarge, "%s exceeds %d bytes", rawURL, maxBytes)
	}

	return raw, nil
}

// get is one GET with the channel's status contract: 200 is the body, 404 is
// ErrNotFound, anything else is ErrUnexpectedStatus; a refused redirect
// surfaces as the ErrEscapesBase the redirect policy returned.
func (c *Channel) get(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "%s", rawURL)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		var escaped *url.Error
		if errors.As(err, &escaped) && errors.Is(escaped.Err, ErrEscapesBase) {
			return nil, escaped.Err
		}

		return nil, errors.Wrapf(err, "fetching %s", rawURL)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return resp, nil
	case http.StatusNotFound:
		_ = resp.Body.Close()

		return nil, errors.Wrapf(ErrNotFound, "%s", rawURL)
	default:
		_ = resp.Body.Close()

		return nil, errors.Wrapf(ErrUnexpectedStatus, "%s: HTTP %d", rawURL, resp.StatusCode)
	}
}
