package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"debug/buildinfo"
	"io"
	"os"
	"strings"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/internal/binversion"
)

var errNoPlatform = errors.NewSentinel("gtb.releasemanifest.no_platform", "no Go binary with a GOOS and GOARCH in its build info")

// built is what a Go binary records about its own build.
type built struct {
	goos, goarch string
	// version is the release it was built as, or "" when it records none.
	version string
}

// readPlatform is what the tool's binary inside the archive at path records in
// its own build info: the one update installs, or the first Go binary when none
// is named for the tool. The archive's name says nothing: goreleaser titles and
// remaps the platform in the file name, and a hand-rolled publisher may name it
// anything.
func readPlatform(path, tool string) (built, error) {
	var err error

	found := pick{tool: tool}

	switch {
	case strings.HasSuffix(path, ".tar.gz"), strings.HasSuffix(path, ".tgz"):
		err = eachInTarGz(path, found.offer)
	case strings.HasSuffix(path, ".zip"):
		err = eachInZip(path, found.offer)
	default:
		return built{}, errors.Wrapf(errNoPlatform, "%s: not a tar.gz or zip archive", path)
	}

	if err != nil {
		return built{}, errors.Wrapf(err, "%s", path)
	}

	if !found.any {
		return built{}, errors.Wrapf(errNoPlatform, "%s", path)
	}

	return found.built, nil
}

// pick chooses an archive's binary from the entries it is offered.
type pick struct {
	tool  string
	built built
	any   bool
}

// offer reads an entry, and reports whether the choice is settled. An entry
// named for the tool settles it even when it is no Go binary, because update
// installs it whatever it is.
func (p *pick) offer(name string, entry io.Reader) (settled bool) {
	named := name == p.tool || name == p.tool+".exe"
	if p.any && !named {
		return false
	}

	b, ok := platformOf(entry)
	if ok || named {
		p.built, p.any = b, ok
	}

	return named
}

func eachInTarGz(path string, offer func(string, io.Reader) bool) error {
	f, err := os.Open(path)
	if err != nil {
		return errors.WithStack(err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return errors.WithStack(err)
	}

	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return errors.WithStack(err)
		}

		if offer(hdr.Name, tr) {
			return nil
		}
	}
}

func eachInZip(path string, offer func(string, io.Reader) bool) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return errors.WithStack(err)
	}
	defer func() { _ = zr.Close() }()

	for _, entry := range zr.File {
		if entry.FileInfo().IsDir() {
			continue
		}

		rc, err := entry.Open()
		if err != nil {
			return errors.Wrap(err, entry.Name)
		}

		settled := offer(entry.Name, rc)
		_ = rc.Close()

		if settled {
			return nil
		}
	}

	return nil
}

// platformOf reads one archive entry and reports its build platform when the
// entry is a Go binary that records one. Anything else (a licence, a readme,
// a binary of another language) is simply not it.
func platformOf(entry io.Reader) (built, bool) {
	data, err := io.ReadAll(entry)
	if err != nil {
		return built{}, false
	}

	info, err := buildinfo.Read(bytes.NewReader(data))
	if err != nil {
		return built{}, false
	}

	b := built{version: binversion.Recorded(info)}

	for _, s := range info.Settings {
		switch s.Key {
		case "GOOS":
			b.goos = s.Value
		case "GOARCH":
			b.goarch = s.Value
		}
	}

	return b, b.goos != "" && b.goarch != ""
}
