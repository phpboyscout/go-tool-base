// Command releasemanifest writes the static release channel's per-tag
// manifest (spec 0203 D2, D5) from what goreleaser already knows about a
// release: dist/artifacts.json for the archives and their checksums,
// dist/metadata.json for the tag and date. It runs as a goreleaser after-hook,
// before the blobs pipe uploads dist/, so release.json travels with the
// binaries and inherits the store's immutability.
//
//	go tool releasemanifest --dist dist --base-url https://pkg.example.com/acme/tool [--previous v1.1.0] [--notes NOTES.md]
//
// --previous names the tag the manifest chains to. When omitted the current
// pointer at the base URL is read and its tag used; with no pointer the chain
// starts here. It refuses a dist whose archives carry no checksum: a manifest
// that cannot be verified is not published.
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
	"sort"
	"strings"
	"time"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
)

var errNoChecksum = errors.NewSentinel("gtb.releasemanifest.no_checksum", "an archive carries no checksum; nothing unverifiable is published")

const (
	pointerTimeout  = 15 * time.Second
	maxPointerBytes = 64 << 10
	// manifestMode: a release artefact, readable like the rest of dist.
	manifestMode = 0o644
	exitUsage    = 2
)

func main() {
	var (
		dist     string
		baseURL  string
		previous string
		notes    string
		out      string
	)

	flag.StringVar(&dist, "dist", "dist", "goreleaser's dist directory")
	flag.StringVar(&baseURL, "base-url", "", "the channel's base URL, where the pointer and manifests live (required)")
	flag.StringVar(&previous, "previous", "", "the tag this release follows; read from the current pointer when omitted")
	flag.StringVar(&notes, "notes", "", "a file whose text becomes the manifest's notes")
	flag.StringVar(&out, "out", "", "where to write the manifest (default <dist>/release.json)")
	flag.Parse()

	if baseURL == "" {
		fmt.Fprintln(os.Stderr, "releasemanifest: --base-url is required")
		os.Exit(exitUsage)
	}

	if previous == "" {
		tag, err := currentTag(context.Background(), baseURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "releasemanifest: reading the current pointer: %v\n", err)
			os.Exit(1)
		}

		previous = tag
	}

	m, err := build(dist, baseURL, previous, notes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "releasemanifest: %v\n", err)
		os.Exit(1)
	}

	if out == "" {
		out = filepath.Join(dist, static.ManifestFile)
	}

	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "releasemanifest: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(out, append(raw, '\n'), manifestMode); err != nil {
		fmt.Fprintf(os.Stderr, "releasemanifest: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("releasemanifest: wrote %s (%s, %d downloads, previous %q)\n", out, m.Tag, len(m.Downloads), m.Previous)
}

// artifact is the slice of goreleaser's artifacts.json entry this tool reads.
type artifact struct {
	Name  string         `json:"name"`
	Path  string         `json:"path"`
	Goos  string         `json:"goos"`
	Arch  string         `json:"goarch"`
	Type  string         `json:"type"`
	Extra map[string]any `json:"extra"`
}

type metadata struct {
	ProjectName string `json:"project_name"`
	Tag         string `json:"tag"`
	Date        string `json:"date"`
}

// build composes the manifest for the release in dist. It is pure over the
// directory so a test can pin it with a golden file.
func build(dist, baseURL, previous, notesPath string) (static.Manifest, error) {
	var (
		arts []artifact
		meta metadata
	)

	if err := readJSON(filepath.Join(dist, "artifacts.json"), &arts); err != nil {
		return static.Manifest{}, err
	}

	if err := readJSON(filepath.Join(dist, "metadata.json"), &meta); err != nil {
		return static.Manifest{}, err
	}

	released, err := time.Parse(time.RFC3339Nano, meta.Date)
	if err != nil {
		return static.Manifest{}, errors.Wrapf(err, "metadata.json date %q", meta.Date)
	}

	m := static.Manifest{
		Schema:     static.SchemaVersion,
		Tool:       meta.ProjectName,
		Tag:        meta.Tag,
		ReleasedAt: released.UTC().Format(time.RFC3339),
		Previous:   previous,
	}

	if err := addArtifacts(&m, dist, baseURL, arts); err != nil {
		return static.Manifest{}, err
	}

	if notesPath != "" {
		text, err := os.ReadFile(notesPath)
		if err != nil {
			return static.Manifest{}, errors.Wrap(err, "reading notes")
		}

		m.Notes = string(text)
	}

	return m, nil
}

// addArtifacts fills the manifest's files from artifacts.json: one download
// per archive, the checksums file, the signature when the release has one.
// Downloads are ordered by os then arch so the document is stable whatever
// order goreleaser built in.
func addArtifacts(m *static.Manifest, dist, baseURL string, arts []artifact) error {
	for _, a := range arts {
		switch a.Type {
		case "Archive":
			d, err := download(dist, baseURL, m.Tag, a)
			if err != nil {
				return err
			}

			m.Downloads = append(m.Downloads, d)
		case "Checksum":
			m.Checksums = static.FileURL(baseURL, m.Tag, a.Name)
		case "Signature":
			m.Signature = static.FileURL(baseURL, m.Tag, a.Name)
		}
	}

	if m.Checksums == "" {
		return errors.Wrap(errNoChecksum, "artifacts.json lists no checksums file")
	}

	sort.Slice(m.Downloads, func(i, j int) bool {
		if m.Downloads[i].OS != m.Downloads[j].OS {
			return m.Downloads[i].OS < m.Downloads[j].OS
		}

		return m.Downloads[i].Arch < m.Downloads[j].Arch
	})

	return nil
}

// download is one archive's row. The checksum goreleaser records is
// "sha256:<hex>"; the size is the file's, read from dist.
func download(dist, baseURL, tag string, a artifact) (static.Download, error) {
	sum, _ := a.Extra["Checksum"].(string)
	if sum == "" {
		return static.Download{}, errors.Wrapf(errNoChecksum, "%s", a.Name)
	}

	sum = strings.TrimPrefix(sum, "sha256:")

	info, err := os.Stat(filepath.Join(dist, a.Name))
	if err != nil {
		return static.Download{}, errors.Wrapf(err, "sizing %s", a.Name)
	}

	return static.Download{
		OS:     a.Goos,
		Arch:   a.Arch,
		Name:   a.Name,
		URL:    static.FileURL(baseURL, tag, a.Name),
		Size:   info.Size(),
		SHA256: sum,
	}, nil
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

func readJSON(path string, into any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return errors.Wrapf(err, "reading %s", filepath.Base(path))
	}

	if err := json.Unmarshal(raw, into); err != nil {
		return errors.Wrapf(err, "parsing %s", filepath.Base(path))
	}

	return nil
}
