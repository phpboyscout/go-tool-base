package static_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
)

// channelServer is a static channel on an httptest.Server: a pointer and a
// chain of manifests under /acme/tool, plus whatever extra objects a test
// adds. Every response is a plain file, as a store would serve it.
type channelServer struct {
	*httptest.Server
	objects  map[string][]byte
	requests atomic.Int32
}

func newChannelServer(t *testing.T) *channelServer {
	t.Helper()

	s := &channelServer{objects: map[string][]byte{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)

		body, ok := s.objects[r.URL.Path]
		if !ok {
			http.NotFound(w, r)

			return
		}

		_, _ = w.Write(body)
	}))
	t.Cleanup(s.Close)

	return s
}

func (s *channelServer) base() string { return s.URL + "/acme/tool" }

func (s *channelServer) put(path string, doc any) {
	raw, err := json.Marshal(doc)
	if err != nil {
		panic(err)
	}

	s.objects[path] = raw
}

// chain publishes v1.0.0 → v1.1.0 → v1.2.0 with the pointer on v1.2.0.
func (s *channelServer) chain() {
	base := s.base()
	tags := []string{"v1.0.0", "v1.1.0", "v1.2.0"}

	previous := ""

	for _, tag := range tags {
		s.put("/acme/tool/"+tag+"/release.json", manifestFor(base, tag, previous))
		s.objects["/acme/tool/"+tag+"/checksums.txt"] = []byte("ec90dd6d7f33fcaa8179a6647e0ad6e2fe17e0551b8989760ff2ce33ed907d81  tool_Linux_x86_64.tar.gz\n")
		s.objects["/acme/tool/"+tag+"/checksums.txt.sig"] = []byte("-----BEGIN PGP SIGNATURE-----\n" + tag + "\n")
		s.objects["/acme/tool/"+tag+"/tool_Linux_x86_64.tar.gz"] = []byte("archive " + tag)
		previous = tag
	}

	s.put("/acme/tool/latest.json", static.Pointer{
		Schema: static.SchemaVersion, Tool: "tool", Tag: "v1.2.0",
		Manifest: base + "/v1.2.0/release.json", PublishedAt: "2026-09-19T12:20:00Z",
	})
}

func manifestFor(base, tag, previous string) static.Manifest {
	return static.Manifest{
		Schema:     static.SchemaVersion,
		Tool:       "tool",
		Tag:        tag,
		ReleasedAt: "2026-09-19T12:00:00Z",
		Previous:   previous,
		Checksums:  static.FileURL(base, tag, "checksums.txt"),
		Signature:  static.FileURL(base, tag, "checksums.txt.sig"),
		Notes:      "notes for " + tag,
		Downloads: []static.Download{{
			OS: "linux", Arch: "amd64", Name: "tool_Linux_x86_64.tar.gz",
			URL:  static.FileURL(base, tag, "tool_Linux_x86_64.tar.gz"),
			Size: 13, SHA256: "ec90dd6d7f33fcaa8179a6647e0ad6e2fe17e0551b8989760ff2ce33ed907d81",
		}},
	}
}

func TestNew_RefusesABaseThatIsNotAnAbsoluteHTTPURL(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "pkg.example.com/acme", "ftp://pkg.example.com/acme", "https://", "https://pkg.example.com/acme?x=1"} {
		_, err := static.New(raw, nil)
		require.ErrorIs(t, err, static.ErrInvalidBaseURL, raw)
	}

	ch, err := static.New("https://pkg.example.com/acme/tool/", nil)
	require.NoError(t, err)
	assert.Equal(t, "https://pkg.example.com/acme/tool", ch.BaseURL(), "a trailing slash is normalised away")
}

func TestChannel_LatestFollowsThePointer(t *testing.T) {
	t.Parallel()

	srv := newChannelServer(t)
	srv.chain()

	ch, err := static.New(srv.base(), srv.Client())
	require.NoError(t, err)

	latest, err := ch.Latest(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "v1.2.0", latest.Tag)
	assert.Equal(t, "v1.1.0", latest.Previous)
	assert.Equal(t, "notes for v1.2.0", latest.Notes)
	require.Len(t, latest.Downloads, 1)
	assert.Equal(t, int32(2), srv.requests.Load(), "the pointer and one manifest, nothing else")
}

func TestChannel_ByTagIsOneFetch(t *testing.T) {
	t.Parallel()

	srv := newChannelServer(t)
	srv.chain()

	ch, err := static.New(srv.base(), srv.Client())
	require.NoError(t, err)

	m, err := ch.ByTag(context.Background(), "v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", m.Tag)
	assert.Empty(t, m.Previous, "the first release starts the chain")
	assert.Equal(t, int32(1), srv.requests.Load(), "pinning a tag does not read the pointer")

	srv.put("/acme/tool/v1.5.0/"+static.ManifestFile, manifestFor(srv.base(), "v1.0.0", ""))

	_, err = ch.ByTag(context.Background(), "v1.5.0")
	require.ErrorIs(t, err, static.ErrOtherRelease)
	assert.Contains(t, err.Error(), "v1.5.0/release.json is tagged v1.0.0")

	_, err = ch.ByTag(context.Background(), "v9.9.9")
	require.ErrorIs(t, err, static.ErrReleaseNotFound)

	// A tag is a path segment; anything that is not a tag never becomes a URL.
	_, err = ch.ByTag(context.Background(), "../latest.json")
	require.ErrorIs(t, err, static.ErrInvalidManifest)
}

func TestChannel_ListWalksTheChainNewestFirst(t *testing.T) {
	t.Parallel()

	srv := newChannelServer(t)
	srv.chain()

	ch, err := static.New(srv.base(), srv.Client())
	require.NoError(t, err)

	all, err := ch.List(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, []string{"v1.2.0", "v1.1.0", "v1.0.0"}, []string{all[0].Tag, all[1].Tag, all[2].Tag})

	srv.requests.Store(0)

	two, err := ch.List(context.Background(), 2)
	require.NoError(t, err)
	require.Len(t, two, 2)
	assert.Equal(t, "v1.1.0", two[1].Tag)
	assert.Equal(t, int32(3), srv.requests.Load(), "the walk stops at the limit: pointer plus two manifests")
}

func TestChannel_ListStopsOnABrokenOrLoopedChain(t *testing.T) {
	t.Parallel()

	t.Run("a previous that does not resolve ends the list with an error", func(t *testing.T) {
		t.Parallel()

		srv := newChannelServer(t)
		srv.chain()
		delete(srv.objects, "/acme/tool/v1.0.0/release.json")

		ch, err := static.New(srv.base(), srv.Client())
		require.NoError(t, err)

		got, err := ch.List(context.Background(), 0)
		require.ErrorIs(t, err, static.ErrBrokenChain)
		assert.Contains(t, err.Error(), "v1.0.0")
		assert.Len(t, got, 2, "what was read before the break is returned with the error")
	})

	t.Run("a chain that loops is refused, not walked forever", func(t *testing.T) {
		t.Parallel()

		srv := newChannelServer(t)
		srv.chain()
		srv.put("/acme/tool/v1.0.0/release.json", manifestFor(srv.base(), "v1.0.0", "v1.2.0"))

		ch, err := static.New(srv.base(), srv.Client())
		require.NoError(t, err)

		_, err = ch.List(context.Background(), 0)
		require.ErrorIs(t, err, static.ErrBrokenChain)
		assert.Contains(t, err.Error(), "loops")
	})
}

func TestChannel_RefusesWhatItCannotTrust(t *testing.T) {
	t.Parallel()

	t.Run("no pointer means nothing has been published", func(t *testing.T) {
		t.Parallel()

		srv := newChannelServer(t)

		ch, err := static.New(srv.base(), srv.Client())
		require.NoError(t, err)

		_, err = ch.Latest(context.Background())
		require.ErrorIs(t, err, static.ErrNoReleasesPublished)
		assert.Contains(t, err.Error(), "/acme/tool/latest.json", "the URL a person would check is in the message")
	})

	t.Run("a host that cannot be reached is unreachable, with the URL", func(t *testing.T) {
		t.Parallel()

		srv := newChannelServer(t)
		base := srv.base()
		srv.Close()

		ch, err := static.New(base, srv.Client())
		require.NoError(t, err)

		_, err = ch.Latest(context.Background())
		require.ErrorIs(t, err, static.ErrUnreachable)
		assert.Contains(t, err.Error(), "/acme/tool/latest.json")
	})

	t.Run("a cancelled request is not reported as unreachable", func(t *testing.T) {
		t.Parallel()

		srv := newChannelServer(t)

		ch, err := static.New(srv.base(), srv.Client())
		require.NoError(t, err)

		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(errors.New("the user pressed ctrl-c"))

		_, err = ch.Latest(ctx)
		require.Error(t, err)
		assert.NotErrorIs(t, err, static.ErrUnreachable)
	})

	t.Run("a host that answered is not unreachable", func(t *testing.T) {
		t.Parallel()

		untrusted := httptest.NewTLSServer(http.NotFoundHandler())
		t.Cleanup(untrusted.Close)

		failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		t.Cleanup(failing.Close)

		for name, base := range map[string]string{
			"an untrusted certificate": untrusted.URL + "/acme/tool",
			"a server error":           failing.URL + "/acme/tool",
		} {
			ch, err := static.New(base, &http.Client{})
			require.NoError(t, err, name)

			_, err = ch.Latest(context.Background())
			require.Error(t, err, name)
			assert.NotErrorIs(t, err, static.ErrUnreachable, name)
		}
	})

	t.Run("a pointer whose manifest does not resolve names both URLs", func(t *testing.T) {
		t.Parallel()

		srv := newChannelServer(t)
		srv.chain()
		delete(srv.objects, "/acme/tool/v1.2.0/release.json")

		ch, err := static.New(srv.base(), srv.Client())
		require.NoError(t, err)

		_, err = ch.Latest(context.Background())
		require.ErrorIs(t, err, static.ErrBrokenChain)
		assert.Contains(t, err.Error(), "/acme/tool/latest.json")
		assert.Contains(t, err.Error(), "/acme/tool/v1.2.0/release.json")
	})

	t.Run("an unknown schema is refused", func(t *testing.T) {
		t.Parallel()

		srv := newChannelServer(t)
		srv.chain()
		srv.objects["/acme/tool/latest.json"] = []byte(`{"schema":2,"tool":"tool","tag":"v1.2.0","manifest":"` + srv.base() + `/v1.2.0/release.json","published_at":"2026-09-19T12:20:00Z"}`)

		ch, err := static.New(srv.base(), srv.Client())
		require.NoError(t, err)

		_, err = ch.Latest(context.Background())
		require.ErrorIs(t, err, static.ErrUnsupportedSchema)
	})

	t.Run("a manifest URL off the base is refused before it is fetched", func(t *testing.T) {
		t.Parallel()

		srv := newChannelServer(t)
		srv.chain()
		srv.put("/acme/tool/latest.json", static.Pointer{
			Schema: 1, Tool: "tool", Tag: "v1.2.0",
			Manifest: srv.URL + "/acme/other/v1.2.0/release.json", PublishedAt: "2026-09-19T12:20:00Z",
		})

		ch, err := static.New(srv.base(), srv.Client())
		require.NoError(t, err)

		_, err = ch.Latest(context.Background())
		require.ErrorIs(t, err, static.ErrEscapesBase)
		assert.Equal(t, int32(1), srv.requests.Load(), "only the pointer was read")
	})

	t.Run("an oversized document is refused at the bound", func(t *testing.T) {
		t.Parallel()

		srv := newChannelServer(t)
		srv.chain()
		srv.objects["/acme/tool/latest.json"] = []byte(strings.Repeat(" ", static.MaxPointerBytes+1))

		ch, err := static.New(srv.base(), srv.Client())
		require.NoError(t, err)

		_, err = ch.Latest(context.Background())
		require.ErrorIs(t, err, static.ErrTooLarge)
	})

	t.Run("a status that is neither 200 nor 404 is an error naming it", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv.Close)

		ch, err := static.New(srv.URL+"/acme/tool", srv.Client())
		require.NoError(t, err)

		_, err = ch.Latest(context.Background())
		require.ErrorIs(t, err, static.ErrUnexpectedStatus)
		assert.Contains(t, err.Error(), "503")
	})

	t.Run("a redirect that climbs out of the base is not followed", func(t *testing.T) {
		t.Parallel()

		var escaped atomic.Bool

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/"+static.PointerFile) {
				escaped.Store(true)
			}

			http.Redirect(w, r, "/acme/fixtool/%2e%2e/%2e%2e/other", http.StatusFound)
		}))
		t.Cleanup(srv.Close)

		ch, err := static.New(srv.URL+"/acme/fixtool", srv.Client())
		require.NoError(t, err)

		_, err = ch.Latest(context.Background())
		require.ErrorIs(t, err, static.ErrEscapesBase)
		assert.False(t, escaped.Load(), "nothing outside the base was asked for")
	})

	t.Run("a redirect off the base is not followed", func(t *testing.T) {
		t.Parallel()

		elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"schema":1}`))
		}))
		t.Cleanup(elsewhere.Close)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, elsewhere.URL+"/latest.json", http.StatusFound)
		}))
		t.Cleanup(srv.Close)

		ch, err := static.New(srv.URL+"/acme/tool", srv.Client())
		require.NoError(t, err)

		_, err = ch.Latest(context.Background())
		require.ErrorIs(t, err, static.ErrEscapesBase)
	})

	t.Run("a redirect one difference off the base is not followed", func(t *testing.T) {
		t.Parallel()

		const path = "/acme/tool/hop"

		var asked atomic.Int32

		elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { asked.Add(1) }))
		t.Cleanup(elsewhere.Close)

		var target string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			asked.Add(1)
			http.Redirect(w, r, target, http.StatusFound)
		}))
		t.Cleanup(srv.Close)

		for name, to := range map[string]string{
			"host":          strings.Replace(srv.URL, "127.0.0.1", "localhost", 1) + path,
			"port":          elsewhere.URL + path,
			"a longer name": srv.URL + "/acme/tool2/hop",
			"further in":    srv.URL + "/evil/acme/tool/hop",
			"letter case":   srv.URL + "/Acme/tool/hop",
		} {
			target = to

			asked.Store(0)

			ch, err := static.New(srv.URL+"/acme/tool", srv.Client())
			require.NoError(t, err)

			_, err = ch.Latest(context.Background())
			require.ErrorIs(t, err, static.ErrEscapesBase, name)
			assert.EqualValues(t, 1, asked.Load(), name)
		}
	})

	t.Run("a redirect off the base after one under it is not followed", func(t *testing.T) {
		t.Parallel()

		var strayed atomic.Bool

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/acme/tool/latest.json":
				http.Redirect(w, r, "/acme/tool/hop", http.StatusFound)
			case "/acme/tool/hop":
				http.Redirect(w, r, "/elsewhere/latest.json", http.StatusFound)
			default:
				strayed.Store(true)
			}
		}))
		t.Cleanup(srv.Close)

		ch, err := static.New(srv.URL+"/acme/tool", srv.Client())
		require.NoError(t, err)

		_, err = ch.Latest(context.Background())
		require.ErrorIs(t, err, static.ErrEscapesBase)
		assert.False(t, strayed.Load())
	})
}

func TestChannel_RedirectsAreBounded(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		redirects    int
		wantRequests int
		refused      bool
	}{
		"as many as the bound": {redirects: 10, wantRequests: 11},
		"one more":             {redirects: 11, wantRequests: 11, refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var requests atomic.Int64

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := int(requests.Add(1))
				if n <= tt.redirects {
					http.Redirect(w, r, fmt.Sprintf("/acme/tool/hop%d", n), http.StatusFound)

					return
				}

				_, _ = w.Write([]byte("sums"))
			}))
			t.Cleanup(srv.Close)

			ch, err := static.New(srv.URL+"/acme/tool", srv.Client())
			require.NoError(t, err)

			got, err := ch.Fetch(context.Background(), srv.URL+"/acme/tool/v1.0.0/checksums.txt", 1<<10)
			assert.EqualValues(t, tt.wantRequests, requests.Load())

			if !tt.refused {
				require.NoError(t, err)
				assert.Equal(t, "sums", string(got))

				return
			}

			require.ErrorIs(t, err, static.ErrTooManyRedirects)
			require.NotErrorIs(t, err, static.ErrUnreachable)
			assert.Contains(t, err.Error(), srv.URL+"/acme/tool/v1.0.0/checksums.txt")
		})
	}
}

func TestChannel_FetchAndOpenStayUnderTheBase(t *testing.T) {
	t.Parallel()

	srv := newChannelServer(t)
	srv.chain()

	ch, err := static.New(srv.base(), srv.Client())
	require.NoError(t, err)

	ctx := context.Background()
	m, err := ch.ByTag(ctx, "v1.2.0")
	require.NoError(t, err)

	sums, err := ch.Fetch(ctx, m.Checksums, 1<<10)
	require.NoError(t, err)
	assert.Contains(t, string(sums), "tool_Linux_x86_64.tar.gz")

	_, err = ch.Fetch(ctx, m.Checksums, 10)
	require.ErrorIs(t, err, static.ErrTooLarge)

	_, err = ch.Fetch(ctx, srv.URL+"/acme/other/checksums.txt", 1<<10)
	require.ErrorIs(t, err, static.ErrEscapesBase)

	_, err = ch.Fetch(ctx, m.Signature+".missing", 1<<10)
	require.ErrorIs(t, err, static.ErrNotFound)

	rc, err := ch.Open(ctx, m.Downloads[0].URL)
	require.NoError(t, err)

	body, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.Equal(t, "archive v1.2.0", string(body))

	_, err = ch.Open(ctx, "https://elsewhere.example.com/tool.tar.gz")
	require.ErrorIs(t, err, static.ErrEscapesBase)
}
