package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (s store) options(t *testing.T) options {
	t.Helper()

	keys := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(keys, "key.asc"), s.key.public, 0o600))

	return options{
		source: s.dir, tag: "v1.0.0", date: "2026-01-01T12:00:00Z", keys: keys, origin: "gtb",
		out: filepath.Join(t.TempDir(), "pkgrepo"),
	}
}

func TestTheIndexesAreWritten(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	s.releaseFixtures(t, "1.0.0")
	o := s.options(t)
	require.NoError(t, run(t.Context(), o))

	want, err := s.build(t, "v1.0.0", january)
	require.NoError(t, err)

	for name, data := range want {
		got, err := os.ReadFile(filepath.Join(o.out, filepath.FromSlash(name)))
		require.NoError(t, err)
		assert.Equal(t, string(data), string(got), name)
	}

	err = run(t.Context(), o)
	require.ErrorIs(t, err, errOutput, "an earlier run's files would be published with these")
}

// Any key in the directory will do, so a release before a key rotation and
// one after it are both listed.
func TestEveryReleaseKeyIsTrusted(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	s.releaseFixtures(t, "1.0.0")
	o := s.options(t)
	require.NoError(t, os.WriteFile(filepath.Join(o.keys, "a-newer-key.asc"), newReleaseKey(t).public, 0o600))
	require.NoError(t, run(t.Context(), o))
}

func TestARunThatCannotStartWritesNothing(t *testing.T) {
	t.Parallel()

	for name, change := range map[string]func(t *testing.T, o *options){
		"a date that is not one": func(_ *testing.T, o *options) { o.date = "yesterday" },
		"no keys":                func(t *testing.T, o *options) { o.keys = t.TempDir() },
		"a key that does not read": func(t *testing.T, o *options) {
			require.NoError(t, os.WriteFile(filepath.Join(o.keys, "key.asc"), []byte("x"), 0o600))
		},
		"a key that is a directory": func(t *testing.T, o *options) {
			require.NoError(t, os.Mkdir(filepath.Join(o.keys, "dir.asc"), 0o750))
		},
		"no such directory":  func(t *testing.T, o *options) { o.source = filepath.Join(t.TempDir(), "none") },
		"an unpublished tag": func(_ *testing.T, o *options) { o.tag = "v9.0.0" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := newStore(t)
			s.releaseFixtures(t, "1.0.0")
			o := s.options(t)
			change(t, &o)

			require.Error(t, run(t.Context(), o))
			assert.NoDirExists(t, o.out)
		})
	}
}

func TestASourceIsADirectoryOrHTTPSWithNoLogin(t *testing.T) {
	t.Parallel()

	for _, location := range []string{
		"http://pkg.example.test", "https://a:b@pkg.example.test", "https:///store", "https://[", "s3://bucket",
	} {
		_, err := newSource(location)
		require.ErrorIs(t, err, errSource, location)
		require.NotContains(t, err.Error(), "a:b", "a login is not shown")
	}
}

func TestASourceOverHTTPS(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	s.releaseFixtures(t, "1.0.0")

	files := http.FileServerFS(os.DirFS(s.dir))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/store/moved":
			http.Redirect(w, r, "/store/v1.0.0/checksums.txt", http.StatusFound)
		case "/store/broken":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.StripPrefix("/store", files).ServeHTTP(w, r)
		}
	}))
	t.Cleanup(server.Close)

	src, err := newSource(server.URL + "/store/")
	require.NoError(t, err)

	remote, ok := src.(httpSource)
	require.True(t, ok)

	remote.client.Transport = server.Client().Transport

	got, err := build(t.Context(), remote, s.key.trust(t), release{tag: "v1.0.0", origin: "gtb", date: january})
	require.NoError(t, err)

	want, err := s.build(t, "v1.0.0", january)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	_, err = remote.get(t.Context(), "none")
	require.ErrorIs(t, err, fs.ErrNotExist)

	// Only a missing object is missing: anything else would start the indexes
	// again from nothing.
	for _, path := range []string{"moved", "broken"} {
		_, err = remote.get(t.Context(), path)
		require.ErrorIs(t, err, errSource, path)
		require.NotErrorIs(t, err, fs.ErrNotExist, path)
	}

	server.Close()

	_, err = remote.get(t.Context(), "v1.0.0/checksums.txt")
	require.Error(t, err)
	require.NotErrorIs(t, err, fs.ErrNotExist)
}
