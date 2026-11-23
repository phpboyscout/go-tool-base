package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/binversion/binversiontest"
)

// An archive's platform is what the Go binary inside it was built for, read
// from its build info, whatever the archive is called.
func TestReadPlatform(t *testing.T) {
	t.Parallel()

	binary := binversiontest.Binary(t, "v1.2.3", binversiontest.Tagged)
	entries := []entry{{name: "LICENSE", body: []byte("MIT")}, {name: "README.md", body: []byte("# tool")}, {name: "tool", body: binary}}

	t.Run("tar.gz", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "tool_Somewhere_x86_64.tar.gz")
		writeTarGz(t, path, entries...)

		got, err := readPlatform(path, "tool")
		require.NoError(t, err)
		assert.Equal(t, built{goos: runtime.GOOS, goarch: runtime.GOARCH, version: "v1.2.3"}, got)
	})

	t.Run("zip", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "tool_Somewhere_x86_64.zip")
		writeZip(t, path, entries...)

		got, err := readPlatform(path, "tool")
		require.NoError(t, err)
		assert.Equal(t, built{goos: runtime.GOOS, goarch: runtime.GOARCH, version: "v1.2.3"}, got)
	})

	t.Run("a binary that records no release", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "tool.tar.gz")
		writeTarGz(t, path, entry{name: "tool", body: binversiontest.Binary(t, "v1.2.3", binversiontest.NoVCS)})

		got, err := readPlatform(path, "tool")
		require.NoError(t, err)
		assert.Equal(t, built{goos: runtime.GOOS, goarch: runtime.GOARCH}, got)
	})

	// update installs the binary named for the tool, so that is the one checked.
	other := binversiontest.Binary(t, "v0.9.0", binversiontest.Tagged)

	for name, tt := range map[string]struct {
		entries []entry
		want    string
		none    bool
	}{
		"a helper before the tool":          {entries: []entry{{name: "helper", body: binary}, {name: "tool", body: other}}, want: "v0.9.0"},
		"a helper after the tool":           {entries: []entry{{name: "tool", body: other}, {name: "helper", body: binary}}, want: "v0.9.0"},
		"a helper that records no release":  {entries: []entry{{name: "helper", body: binversiontest.Binary(t, "v1.2.3", binversiontest.NoVCS)}, {name: "tool", body: binary}}, want: "v1.2.3"},
		"a Windows tool":                    {entries: []entry{{name: "helper", body: binary}, {name: "tool.exe", body: other}}, want: "v0.9.0"},
		"one whose name begins as the tool": {entries: []entry{{name: "toolbox", body: binary}, {name: "tool", body: other}}, want: "v0.9.0"},
		"a tool in a directory is not it":   {entries: []entry{{name: "helper", body: binary}, {name: "bin/tool", body: other}}, want: "v1.2.3"},
		"none named for the tool":           {entries: []entry{{name: "one", body: binary}, {name: "two", body: other}}, want: "v1.2.3"},
		"the first of two with the name":    {entries: []entry{{name: "tool", body: other}, {name: "tool", body: binary}}, want: "v0.9.0"},
		"the first of them no Go binary":    {entries: []entry{{name: "tool", body: []byte("#!/bin/sh\n")}, {name: "tool", body: binary}}, none: true},
		"a tool that is not a Go binary":    {entries: []entry{{name: "helper", body: binary}, {name: "tool", body: []byte("#!/bin/sh\n")}}, none: true},
	} {
		for ext, write := range map[string]func(*testing.T, string, ...entry){".tar.gz": writeTarGz, ".zip": writeZip} {
			t.Run(name+ext, func(t *testing.T) {
				t.Parallel()

				path := filepath.Join(t.TempDir(), "tool"+ext)
				write(t, path, tt.entries...)

				got, err := readPlatform(path, "tool")
				if tt.none {
					require.ErrorIs(t, err, errNoPlatform)

					return
				}

				require.NoError(t, err)
				assert.Equal(t, tt.want, got.version)
			})
		}
	}

	// update takes the entry by its name alone, so a link is what it would install.
	t.Run("a link named for the tool", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "tool.tar.gz")
		writeTarGz(t, path, entry{name: "helper", body: binary}, entry{name: "tool", link: "helper"})

		_, err := readPlatform(path, "tool")
		require.ErrorIs(t, err, errNoPlatform)
	})

	t.Run("no Go binary inside", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "docs.tar.gz")
		writeTarGz(t, path, entry{name: "LICENSE", body: []byte("MIT")})

		_, err := readPlatform(path, "tool")
		require.ErrorIs(t, err, errNoPlatform)
	})

	t.Run("an unknown archive format", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "tool.rar")
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))

		_, err := readPlatform(path, "tool")
		require.ErrorIs(t, err, errNoPlatform)
	})
}

// entry is one file of an archive, in the order given.
type entry struct {
	name string
	body []byte
	link string
}

func writeTarGz(t *testing.T, path string, entries ...entry) {
	t.Helper()

	f, err := os.Create(path)
	require.NoError(t, err)

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o755, Size: int64(len(e.body))}
		if e.link != "" {
			hdr.Typeflag, hdr.Linkname = tar.TypeSymlink, e.link
		}

		require.NoError(t, tw.WriteHeader(hdr))
		_, err = tw.Write(e.body)
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	require.NoError(t, f.Close())
}

func writeZip(t *testing.T, path string, entries ...entry) {
	t.Helper()

	f, err := os.Create(path)
	require.NoError(t, err)

	zw := zip.NewWriter(f)

	for _, e := range entries {
		w, err := zw.Create(e.name)
		require.NoError(t, err)
		_, err = w.Write(e.body)
		require.NoError(t, err)
	}

	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
}
