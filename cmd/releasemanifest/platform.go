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
)

var errNoPlatform = errors.NewSentinel("gtb.releasemanifest.no_platform", "no Go binary with a GOOS and GOARCH in its build info")

// readPlatform is the GOOS and GOARCH of the Go binary inside the archive at
// path, from the binary's own build info. The archive's name says nothing:
// goreleaser titles and remaps the platform in the file name, and a
// hand-rolled publisher may name it anything.
func readPlatform(path string) (string, string, error) {
	switch {
	case strings.HasSuffix(path, ".tar.gz"), strings.HasSuffix(path, ".tgz"):
		return platformInTarGz(path)
	case strings.HasSuffix(path, ".zip"):
		return platformInZip(path)
	default:
		return "", "", errors.Wrapf(errNoPlatform, "%s: not a tar.gz or zip archive", path)
	}
}

func platformInTarGz(path string) (string, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", errors.WithStack(err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", "", errors.Wrapf(err, "%s", path)
	}

	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return "", "", errors.Wrapf(errNoPlatform, "%s", path)
		}

		if err != nil {
			return "", "", errors.Wrapf(err, "%s", path)
		}

		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		if goos, goarch, ok := platformOf(tr); ok {
			return goos, goarch, nil
		}
	}
}

func platformInZip(path string) (string, string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", "", errors.Wrapf(err, "%s", path)
	}
	defer func() { _ = zr.Close() }()

	for _, entry := range zr.File {
		if entry.FileInfo().IsDir() {
			continue
		}

		rc, err := entry.Open()
		if err != nil {
			return "", "", errors.Wrapf(err, "%s: %s", path, entry.Name)
		}

		goos, goarch, ok := platformOf(rc)
		_ = rc.Close()

		if ok {
			return goos, goarch, nil
		}
	}

	return "", "", errors.Wrapf(errNoPlatform, "%s", path)
}

// platformOf reads one archive entry and reports its build platform when the
// entry is a Go binary that records one. Anything else (a licence, a readme,
// a binary of another language) is simply not it.
func platformOf(entry io.Reader) (string, string, bool) {
	data, err := io.ReadAll(entry)
	if err != nil {
		return "", "", false
	}

	info, err := buildinfo.Read(bytes.NewReader(data))
	if err != nil {
		return "", "", false
	}

	var goos, goarch string

	for _, s := range info.Settings {
		switch s.Key {
		case "GOOS":
			goos = s.Value
		case "GOARCH":
			goarch = s.Value
		}
	}

	return goos, goarch, goos != "" && goarch != ""
}
