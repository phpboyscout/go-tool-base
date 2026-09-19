// Command releasemanifest writes the static release channel's two documents
// (spec 0203 D2, D3, D5) from what goreleaser has produced by the time its
// before_publish hooks run: dist/metadata.json for the tag and date,
// dist/checksums.txt for the archives and their digests, and the archives
// themselves for their size and, from the binary's build info, their
// platform. It writes dist/release.json, which the blobs pipe uploads with
// the binaries, and dist/latest.json, which the publish step moves last.
//
//	go tool releasemanifest --dist dist --base-url https://pkg.example.com/acme/tool [--previous v1.1.0] [--notes NOTES.md]
//
// --previous names the tag the manifest chains to. When omitted the current
// pointer at the base URL is read and its tag used; with no pointer the chain
// starts here. It refuses a dist whose archives are not all in checksums.txt:
// a manifest that cannot be verified is not published.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
)

const (
	pointerTimeout  = 15 * time.Second
	maxPointerBytes = 64 << 10
	// documentMode: release artefacts, readable like the rest of dist.
	documentMode = 0o644
	exitUsage    = 2
)

func main() {
	var (
		dist     string
		baseURL  string
		previous string
		notes    string
	)

	flag.StringVar(&dist, "dist", "dist", "goreleaser's dist directory")
	flag.StringVar(&baseURL, "base-url", "", "the channel's base URL, where the pointer and manifests live (required)")
	flag.StringVar(&previous, "previous", "", "the tag this release follows; read from the current pointer when omitted")
	flag.StringVar(&notes, "notes", "", "a file whose text becomes the manifest's notes")
	flag.Parse()

	if baseURL == "" {
		fmt.Fprintln(os.Stderr, "releasemanifest: --base-url is required")
		os.Exit(exitUsage)
	}

	if err := run(dist, baseURL, previous, notes); err != nil {
		fmt.Fprintf(os.Stderr, "releasemanifest: %v\n", err)
		os.Exit(1)
	}
}

func run(dist, baseURL, previous, notes string) error {
	if previous == "" {
		tag, err := currentTag(context.Background(), baseURL)
		if err != nil {
			return errors.Wrap(err, "reading the current pointer")
		}

		previous = tag
	}

	m, p, err := builder{platform: readPlatform, now: time.Now}.build(dist, baseURL, previous, notes)
	if err != nil {
		return err
	}

	if err := writeDocument(filepath.Join(dist, static.ManifestFile), m); err != nil {
		return err
	}

	if err := writeDocument(filepath.Join(dist, static.PointerFile), p); err != nil {
		return err
	}

	fmt.Printf("releasemanifest: wrote %s and %s (%s, %d downloads, previous %q)\n",
		static.ManifestFile, static.PointerFile, m.Tag, len(m.Downloads), m.Previous)

	return nil
}

func writeDocument(path string, doc any) error {
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return errors.WithStack(err)
	}

	return errors.Wrapf(os.WriteFile(path, append(raw, '\n'), documentMode), "writing %s", filepath.Base(path))
}

// currentTag reads the pointer at baseURL and returns its tag, or "" when
// there is no pointer yet (the chain starts with this release).
func currentTag(ctx context.Context, baseURL string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pointerTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, static.PointerURL(baseURL), nil)
	if err != nil {
		return "", errors.WithStack(err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.WithStack(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}

	if resp.StatusCode != http.StatusOK {
		return "", errors.Newf("pointer %s: HTTP %d", static.PointerURL(baseURL), resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxPointerBytes))
	if err != nil {
		return "", errors.WithStack(err)
	}

	p, err := static.DecodePointer(raw, baseURL)
	if err != nil {
		return "", err
	}

	return p.Tag, nil
}
