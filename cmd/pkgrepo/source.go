package main

import (
	"context"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"gitlab.com/phpboyscout/go/errors"
)

const (
	// maxObjectBytes bounds one object. The packages are the largest, and
	// gtb's are some tens of megabytes.
	maxObjectBytes = 512 << 20
	fetchTimeout   = 5 * time.Minute
)

// source reads the store the tag is already published on. An object that is
// not there is fs.ErrNotExist.
type source interface {
	get(ctx context.Context, path string) ([]byte, error)
}

func newSource(location string) (source, error) {
	u, err := url.Parse(location)
	if err != nil {
		return nil, errors.Wrap(errSource, "not a URL")
	}

	switch u.Scheme {
	case "":
		return dirSource(location), nil
	case "https":
		if u.Host == "" || u.User != nil {
			return nil, errors.Wrapf(errSource, "%s", u.Redacted())
		}

		return newHTTPSource(strings.TrimRight(location, "/")), nil
	default:
		return nil, errors.Wrapf(errSource, "%q", location)
	}
}

type dirSource string

func (d dirSource) get(_ context.Context, path string) ([]byte, error) {
	root, err := os.OpenRoot(string(d))
	if err != nil {
		return nil, errors.Wrapf(err, "opening %s", string(d))
	}
	defer func() { _ = root.Close() }()

	f, err := root.Open(path)
	if err != nil {
		return nil, errors.Wrapf(err, "reading %s", path)
	}
	defer func() { _ = f.Close() }()

	return readBounded(f, path)
}

type httpSource struct {
	base   string
	client *http.Client
}

func newHTTPSource(base string) httpSource {
	return httpSource{base: base, client: &http.Client{
		Timeout: fetchTimeout,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return errors.Wrapf(errSource, "redirected to %s", req.URL)
		},
	}}
}

func (s httpSource) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/"+path, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "reading %s", path)
	}

	// The indexes are mutable, so a cached one would drop a release.
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "reading %s", path)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		return readBounded(resp.Body, path)
	case http.StatusNotFound:
		return nil, errors.Wrapf(fs.ErrNotExist, "reading %s", path)
	default:
		return nil, errors.Wrapf(errSource, "reading %s: HTTP %d", path, resp.StatusCode)
	}
}

func readBounded(r io.Reader, name string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxObjectBytes+1))
	if err != nil {
		return nil, errors.Wrapf(err, "reading %s", name)
	}

	if len(data) > maxObjectBytes {
		return nil, errors.Wrapf(errTooLarge, "%s", name)
	}

	return data, nil
}
