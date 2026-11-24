package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	publishScript = `exec "$SCRIPT" pkgrepo signing-key-v2.asc "$STORE"`
	storeBase     = "/bucket/gtb/"
	dataSuffix    = ".xml.gz"
)

// publishOrder is D5's, with the key (D9) before every index.
var publishOrder = []string{
	"keys/signing-key-v2.asc",
	"rpm/repodata/p1-primary.xml.gz",
	"rpm/repodata/f1-filelists.xml.gz",
	"rpm/repodata/o1-other.xml.gz",
	"rpm/repodata/repomd.xml",
	"rpm/repodata/repomd.xml.asc",
	"apt/Packages",
	"apt/Packages.gz",
	"apt/Release",
	"apt/Release.gpg",
}

// intercept runs ahead of each request, without the store's lock, and answers
// it itself by returning true.
type intercept func(w http.ResponseWriter, r *http.Request, name string) bool

// s3Double answers conditional reads and writes the way the store does.
type s3Double struct {
	mu      sync.Mutex
	objects map[string][]byte
	headers map[string]http.Header
	puts    []string
	faults  []string

	before intercept
}

func etagOf(body []byte) string {
	sum := sha256.Sum256(body)

	return `"` + hex.EncodeToString(sum[:8]) + `"`
}

func (s *s3Double) set(name string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.objects[name] = body
}

func (s *s3Double) get(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return string(s.objects[name])
}

func (s *s3Double) header(name, key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.headers[name].Get(key)
}

func (s *s3Double) remove(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.objects, name)
}

func (s *s3Double) intercept(before intercept) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.before = before
}

// written is every write so far, in order.
func (s *s3Double) written() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string{}, s.puts...)
}

func (s *s3Double) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, storeBase)

	s.mu.Lock()
	before := s.before
	s.mu.Unlock()

	if before != nil && before(w, r, name) {
		return
	}

	body, _ := io.ReadAll(r.Body)

	s.mu.Lock()
	defer s.mu.Unlock()

	sum := sha256.Sum256(body)
	if r.Header.Get("X-Amz-Content-Sha256") != hex.EncodeToString(sum[:]) ||
		!strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=id/") {
		s.faults = append(s.faults, r.Method+" "+name+" is not signed over its payload")
	}

	current, exists := s.objects[name]

	switch {
	case r.Method == http.MethodPut:
		s.put(w, r, name, body)
	case exists:
		w.Header().Set("ETag", etagOf(current))
		_, _ = w.Write(current)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *s3Double) put(w http.ResponseWriter, r *http.Request, name string, body []byte) {
	current, exists := s.objects[name]

	switch {
	case r.Header.Get("If-None-Match") == "*" && !exists:
	case r.Header.Get("If-Match") != "" && exists && r.Header.Get("If-Match") == etagOf(current):
	case r.Header.Get("If-None-Match") == "" && r.Header.Get("If-Match") == "":
		s.faults = append(s.faults, "unconditional write of "+name)
	default:
		w.WriteHeader(http.StatusPreconditionFailed)

		return
	}

	s.objects[name] = body
	s.headers[name] = r.Header.Clone()
	s.puts = append(s.puts, name)
}

type publishFixture struct {
	store *s3Double
	url   string
	dir   string
	key   string
}

// newPublishFixture is a directory as pkgrepo and the signing step leave it,
// whose base is what the store holds.
func newPublishFixture(t *testing.T, published bool) *publishFixture {
	t.Helper()

	for _, tool := range []string{"bash", "curl", "sha256sum", "cmp", "awk"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on PATH", tool)
		}
	}

	store := &s3Double{objects: map[string][]byte{}, headers: map[string]http.Header{}}
	server := httptest.NewServer(store)
	t.Cleanup(server.Close)

	root := t.TempDir()
	f := &publishFixture{
		store: store,
		url:   server.URL + strings.TrimSuffix(storeBase, "/"),
		dir:   filepath.Join(root, "pkgrepo"),
		key:   filepath.Join(root, "signing-key-v2.asc"),
	}

	f.write(t, f.key, "the key")

	for _, name := range publishOrder[1:] {
		f.write(t, filepath.Join(f.dir, name), "new "+name)

		if published && !strings.HasSuffix(name, dataSuffix) {
			f.write(t, filepath.Join(f.dir, "base", name), "old "+name)
			store.set(name, []byte("old "+name))
		}
	}

	return f
}

func (f *publishFixture) write(t *testing.T, path, body string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

func (f *publishFixture) command(t *testing.T) *exec.Cmd {
	t.Helper()

	script, err := filepath.Abs("../../scripts/publish-repos.sh")
	require.NoError(t, err)

	cmd := exec.CommandContext(t.Context(), "bash", "-c", publishScript)
	cmd.Dir = filepath.Dir(f.key)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "SCRIPT=" + script, "STORE=" + f.url,
		"AWS_ACCESS_KEY_ID=id", "AWS_SECRET_ACCESS_KEY=secret",
	}

	return cmd
}

func (f *publishFixture) run(t *testing.T) (string, error) {
	t.Helper()

	cmd := f.command(t)
	out, err := cmd.CombinedOutput()

	f.store.mu.Lock()
	defer f.store.mu.Unlock()

	assert.Empty(t, f.store.faults)

	return string(out), err
}

func (f *publishFixture) requirePublished(t *testing.T) {
	t.Helper()

	for _, name := range publishOrder[1:] {
		require.Equal(t, "new "+name, f.store.get(name), name)
	}

	require.Equal(t, "the key", f.store.get(publishOrder[0]))
}

func (f *publishFixture) requireNoIndexWritten(t *testing.T) {
	t.Helper()

	for _, name := range f.store.written() {
		require.Truef(t, strings.HasSuffix(name, dataSuffix) || strings.HasPrefix(name, "keys/"), "%s was written", name)
	}
}

func TestPublishRepos_WritesInOrder(t *testing.T) {
	t.Parallel()

	for name, published := range map[string]bool{"first publish": false, "over an earlier one": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newPublishFixture(t, published)

			out, err := f.run(t)
			require.NoError(t, err, out)

			f.requirePublished(t)
			assert.Equal(t, publishOrder, f.store.written())
		})
	}
}

func TestPublishRepos_MutableObjectsAreNotCached(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t, false)

	out, err := f.run(t)
	require.NoError(t, err, out)

	for _, name := range publishOrder {
		want := "no-cache, max-age=0"
		if strings.HasSuffix(name, dataSuffix) || strings.HasPrefix(name, "keys/") {
			want = "public, max-age=31536000, immutable"
		}

		assert.Equal(t, want, f.store.header(name, "Cache-Control"), name)
	}

	assert.Equal(t, "application/pgp-keys", f.store.header("keys/signing-key-v2.asc", "Content-Type"))
	assert.Equal(t, "application/pgp-signature", f.store.header("apt/Release.gpg", "Content-Type"))
	assert.Equal(t, "application/xml", f.store.header("rpm/repodata/repomd.xml", "Content-Type"))
	assert.Equal(t, "application/gzip", f.store.header("apt/Packages.gz", "Content-Type"))
	assert.Equal(t, "text/plain; charset=utf-8", f.store.header("apt/Release", "Content-Type"))
}

func TestPublishRepos_ASecondRunWritesNothing(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t, true)

	out, err := f.run(t)
	require.NoError(t, err, out)

	first := f.store.written()

	out, err = f.run(t)
	require.NoError(t, err, out)
	assert.Equal(t, first, f.store.written())
	f.requirePublished(t)
}

// A publish that died at any point is finished by running it again (D5).
func TestPublishRepos_FinishesOneThatDied(t *testing.T) {
	t.Parallel()

	for _, published := range []bool{false, true} {
		for written := range publishOrder {
			f := newPublishFixture(t, published)
			f.store.intercept(func(w http.ResponseWriter, r *http.Request, name string) bool {
				if r.Method != http.MethodPut || name != publishOrder[written] {
					return false
				}

				w.WriteHeader(http.StatusForbidden)

				return true
			})

			out, err := f.run(t)
			require.Error(t, err)
			require.Contains(t, out, "PUT "+publishOrder[written]+" returned HTTP 403")
			require.Equal(t, publishOrder[:written], f.store.written())

			f.store.intercept(nil)

			out, err = f.run(t)
			require.NoError(t, err, out)
			require.Equal(t, publishOrder, f.store.written())
			f.requirePublished(t)
		}
	}
}

// Another tag published since the indexes were built, whichever object shows it.
func TestPublishRepos_AnotherPublishStopsItBeforeAnyWrite(t *testing.T) {
	t.Parallel()

	for _, name := range publishOrder[4:] {
		for _, published := range []bool{false, true} {
			f := newPublishFixture(t, published)
			f.store.set(name, []byte("another's"))

			out, err := f.run(t)
			require.Error(t, err)
			require.Contains(t, out, name+" is another publish's")
			require.Empty(t, f.store.written())
		}
	}
}

func TestPublishRepos_AVanishedIndexStopsIt(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t, true)
	f.store.remove("apt/Release")

	out, err := f.run(t)
	require.Error(t, err)
	assert.Contains(t, out, "apt/Release was published when the indexes were built, and is gone")
	assert.Empty(t, f.store.written())
}

// The object changes between its read and its write.
func TestPublishRepos_ALostRaceStopsIt(t *testing.T) {
	t.Parallel()

	for _, published := range []bool{false, true} {
		f := newPublishFixture(t, published)
		f.store.intercept(func(_ http.ResponseWriter, r *http.Request, name string) bool {
			if r.Method == http.MethodPut && name == "rpm/repodata/repomd.xml" {
				f.store.set(name, []byte("another's"))
			}

			return false
		})

		out, err := f.run(t)
		require.Error(t, err)
		require.Contains(t, out, "rpm/repodata/repomd.xml changed under this publish")
		f.requireNoIndexWritten(t)
	}
}

// curl retries a write whose answer was lost, and the retry meets a 412.
func TestPublishRepos_AWriteWhoseAnswerWasLostPasses(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t, true)

	f.store.intercept(func(_ http.ResponseWriter, r *http.Request, name string) bool {
		if r.Method == http.MethodPut && name == "apt/Release" {
			f.store.set(name, []byte("new "+name))
		}

		return false
	})

	out, err := f.run(t)
	require.NoError(t, err, out)
	assert.Contains(t, out, "apt/Release is already there")
	f.requirePublished(t)
}

func TestPublishRepos_AnImmutableObjectIsNeverReplaced(t *testing.T) {
	t.Parallel()

	for _, name := range publishOrder[:4] {
		f := newPublishFixture(t, true)
		f.store.set(name, []byte("other bytes"))

		out, err := f.run(t)
		require.Error(t, err)
		require.Contains(t, out, name+" is already published with other bytes")
		require.Equal(t, "other bytes", f.store.get(name))
		f.requireNoIndexWritten(t)
	}
}

func TestPublishRepos_AnAnswerItCannotReadStopsIt(t *testing.T) {
	t.Parallel()

	for status, want := range map[int]string{
		http.StatusForbidden: "GET apt/Packages returned HTTP 403",
		http.StatusFound:     "GET apt/Packages returned HTTP 302",
		http.StatusOK:        "apt/Packages was read without an ETag",
	} {
		f := newPublishFixture(t, true)
		f.store.set("elsewhere", []byte("old apt/Packages"))

		f.store.intercept(func(w http.ResponseWriter, r *http.Request, name string) bool {
			if r.Method != http.MethodGet || name != "apt/Packages" {
				return false
			}

			w.Header().Set("Location", storeBase+"elsewhere")
			w.WriteHeader(status)

			return true
		})

		out, err := f.run(t)
		require.Error(t, err)
		require.Contains(t, out, want)
		require.Empty(t, f.store.written())
	}
}

// Any answer but 200 to a write stops the rest, or an outage part-way would
// end green with the indexes half written.
func TestPublishRepos_AWriteThatIsNotAcceptedStopsIt(t *testing.T) {
	t.Parallel()

	for _, status := range []int{
		http.StatusNoContent, http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect,
		http.StatusNotFound, http.StatusInternalServerError, http.StatusNotImplemented, http.StatusServiceUnavailable,
	} {
		for at, name := range map[int]string{0: publishOrder[0], 1: publishOrder[1], 5: publishOrder[5], 9: publishOrder[9]} {
			t.Run(fmt.Sprintf("%d to %s", status, filepath.Base(name)), func(t *testing.T) {
				t.Parallel()

				f := newPublishFixture(t, true)
				f.store.intercept(func(w http.ResponseWriter, r *http.Request, asked string) bool {
					if r.Method != http.MethodPut || asked != name {
						return false
					}

					// curl waits this long between its retries.
					w.Header().Set("Retry-After", "1")
					w.Header().Set("Location", storeBase+"elsewhere")
					w.WriteHeader(status)

					return true
				})

				out, err := f.run(t)
				require.Error(t, err)
				require.Contains(t, out, fmt.Sprintf("PUT %s returned HTTP %d", name, status))
				require.Equal(t, publishOrder[:at], f.store.written())
			})
		}
	}
}

// A read cut short has a 200, and may hold exactly the bytes the indexes were
// built from or the ones about to be written.
func TestPublishRepos_AReadCutShortStopsIt(t *testing.T) {
	t.Parallel()

	for _, seen := range []string{"old apt/Packages", "new apt/Packages"} {
		f := newPublishFixture(t, true)
		f.store.set("apt/Packages", []byte(seen+" and another's"))
		f.store.intercept(cutShort("apt/Packages", seen))

		out, err := f.run(t)
		require.Error(t, err)
		require.Contains(t, out, "GET apt/Packages did not finish")
		require.Empty(t, f.store.written())
		require.Equal(t, seen+" and another's", f.store.get("apt/Packages"))
	}
}

// The read that follows a 412 is held to the same.
func TestPublishRepos_AConflictsReadCutShortStopsIt(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t, true)
	name := publishOrder[1]
	f.store.set(name, []byte("new "+name+" and more"))
	f.store.intercept(cutShort(name, "new "+name))

	out, err := f.run(t)
	require.Error(t, err)
	require.Contains(t, out, "GET "+name+" did not finish")
	require.Equal(t, publishOrder[:1], f.store.written())
}

func TestPublishRepos_AWriteWithNoAnswerStopsIt(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t, true)
	f.store.intercept(func(_ http.ResponseWriter, r *http.Request, name string) bool {
		if r.Method == http.MethodPut && name == "apt/Packages" {
			panic(http.ErrAbortHandler)
		}

		return false
	})

	out, err := f.run(t)
	require.Error(t, err)
	require.Contains(t, out, "PUT apt/Packages did not finish")
	require.Equal(t, publishOrder[:6], f.store.written())
}

// cutShort answers a read of name with body and a length it does not reach.
func cutShort(name, body string) intercept {
	return func(w http.ResponseWriter, r *http.Request, asked string) bool {
		if r.Method != http.MethodGet || asked != name {
			return false
		}

		w.Header().Set("ETag", etagOf([]byte(body)))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)+1))
		_, _ = w.Write([]byte(body))

		return true
	}
}

func TestPublishRepos_AnIncompleteDirectoryPublishesNothing(t *testing.T) {
	t.Parallel()

	for _, name := range publishOrder {
		f := newPublishFixture(t, true)

		path := filepath.Join(f.dir, name)
		if strings.HasPrefix(name, "keys/") {
			path = f.key
		}

		for _, spoil := range []func() error{
			func() error { return os.Truncate(path, 0) },
			func() error { return os.Remove(path) },
		} {
			require.NoError(t, spoil())

			out, err := f.run(t)
			require.Error(t, err, name)
			require.Contains(t, out, "nothing is published")
			require.Empty(t, f.store.written())
		}
	}

	f := newPublishFixture(t, true)
	f.write(t, filepath.Join(f.dir, "rpm/repodata/p2-primary.xml.gz"), "a second")

	out, err := f.run(t)
	require.Error(t, err)
	assert.Contains(t, out, "does not hold one primary.xml.gz")
}

func TestPublishRepos_NeedsTheStoresCredentials(t *testing.T) {
	t.Parallel()

	for _, missing := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"} {
		f := newPublishFixture(t, true)

		cmd := f.command(t)
		cmd.Env = append(cmd.Env, missing+"=")

		out, err := cmd.CombinedOutput()
		require.Error(t, err)
		require.True(t, bytes.Contains(out, []byte(missing+" is not set")), string(out))
		require.Empty(t, f.store.written())
	}
}
