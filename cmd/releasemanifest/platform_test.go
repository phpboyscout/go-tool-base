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
)

// An archive's platform is what the Go binary inside it was built for, read
// from its build info, whatever the archive is called. The test's own
// executable is the binary, so no toolchain runs here.
func TestReadPlatform(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	require.NoError(t, err)

	binary, err := os.ReadFile(self)
	require.NoError(t, err)

	entries := map[string][]byte{"LICENSE": []byte("MIT"), "README.md": []byte("# tool"), "tool": binary}

	t.Run("tar.gz", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "tool_Somewhere_x86_64.tar.gz")
		writeTarGz(t, path, entries)

		goos, goarch, err := readPlatform(path)
		require.NoError(t, err)
		assert.Equal(t, runtime.GOOS, goos)
		assert.Equal(t, runtime.GOARCH, goarch)
	})

	t.Run("zip", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "tool_Somewhere_x86_64.zip")
		writeZip(t, path, entries)

		goos, goarch, err := readPlatform(path)
		require.NoError(t, err)
		assert.Equal(t, runtime.GOOS, goos)
		assert.Equal(t, runtime.GOARCH, goarch)
	})

	t.Run("no Go binary inside", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "docs.tar.gz")
		writeTarGz(t, path, map[string][]byte{"LICENSE": []byte("MIT")})

		_, _, err := readPlatform(path)
		require.ErrorIs(t, err, errNoPlatform)
	})

	t.Run("an unknown archive format", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "tool.rar")
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))

		_, _, err := readPlatform(path)
		require.ErrorIs(t, err, errNoPlatform)
	})
}

func writeTarGz(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()

	f, err := os.Create(path)
	require.NoError(t, err)

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	for name, body := range entries {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}))
		_, err = tw.Write(body)
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	require.NoError(t, f.Close())
}

func writeZip(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()

	f, err := os.Create(path)
	require.NoError(t, err)

	zw := zip.NewWriter(f)

	for name, body := range entries {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write(body)
		require.NoError(t, err)
	}

	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
}
