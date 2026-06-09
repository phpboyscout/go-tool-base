package setup

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWKDLocalPartHash_KnownVector(t *testing.T) {
	t.Parallel()

	// Canonical test vector from draft-koch-openpgp-webkey-service: the
	// local-part "joe.doe" hashes to "iy9q119eutrkn8s1mk4r39qejnbu3n5q".
	assert.Equal(t, "iy9q119eutrkn8s1mk4r39qejnbu3n5q", wkdLocalPartHash("joe.doe"))
	// Casing on the local-part input must not affect the output.
	assert.Equal(t, "iy9q119eutrkn8s1mk4r39qejnbu3n5q", wkdLocalPartHash("Joe.Doe"))
}

func TestWKDURLs_ReleaseEmail(t *testing.T) {
	t.Parallel()

	advanced, direct, advancedHost, err := WKDURLs("release@phpboyscout.uk")
	require.NoError(t, err)

	assert.Contains(t, advanced, "https://openpgpkey.phpboyscout.uk/.well-known/openpgpkey/phpboyscout.uk/hu/")
	assert.Contains(t, advanced, "?l=release")
	assert.Contains(t, direct, "https://phpboyscout.uk/.well-known/openpgpkey/hu/")
	assert.Contains(t, direct, "?l=release")
	assert.Equal(t, "openpgpkey.phpboyscout.uk", advancedHost)
}

func TestWKDURLs_DomainLowercased(t *testing.T) {
	t.Parallel()

	advanced, direct, advancedHost, err := WKDURLs("Joe.Doe@Example.ORG")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(advanced, "https://openpgpkey.example.org/"),
		"domain must be lowercased in URL")
	assert.True(t, strings.HasPrefix(direct, "https://example.org/"),
		"domain must be lowercased in URL")
	assert.Equal(t, "openpgpkey.example.org", advancedHost)
}

func TestWKDURLs_InvalidEmail(t *testing.T) {
	t.Parallel()

	cases := []string{"", "no-at-sign", "@nolocal.example", "nodomain@"}
	for _, in := range cases {
		_, _, _, err := WKDURLs(in)
		require.Error(t, err, "expected error for %q", in)
	}
}

func TestNewWKDResolver_MalformedURLOverride(t *testing.T) {
	t.Parallel()

	_, err := NewWKDResolver(WKDResolverConfig{
		Email:       "release@phpboyscout.uk",
		HTTPClient:  http.DefaultClient,
		URLOverride: "://no-scheme",
	})
	require.Error(t, err)
}

func TestNewWKDResolver_Validation(t *testing.T) {
	t.Parallel()

	_, err := NewWKDResolver(WKDResolverConfig{HTTPClient: http.DefaultClient})
	require.Error(t, err)

	_, err = NewWKDResolver(WKDResolverConfig{Email: "release@phpboyscout.uk"})
	require.Error(t, err)

	_, err = NewWKDResolver(WKDResolverConfig{
		Email:      "bad-email",
		HTTPClient: http.DefaultClient,
	})
	require.Error(t, err)
}

func TestWKDResolver_Name_UsesCanonicalHost(t *testing.T) {
	t.Parallel()

	r, err := NewWKDResolver(WKDResolverConfig{
		Email:       "release@phpboyscout.uk",
		HTTPClient:  http.DefaultClient,
		URLOverride: "https://127.0.0.1:55555",
	})
	require.NoError(t, err)
	assert.Equal(t, "wkd:openpgpkey.phpboyscout.uk", r.Name(),
		"Name must use the canonical WKD host even with URL override")
}

// serializeBinaryKey returns the binary (non-armored) public key for
// an entity, which is the WKD wire format.
func serializeBinaryKey(t *testing.T, ent *openpgp.Entity) []byte {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, ent.Serialize(&buf))

	return buf.Bytes()
}

// newWKDTestServer returns a TLS test server whose handler responds
// from the supplied map of path-prefix → handler. The returned client
// trusts the server's self-signed cert.
func newWKDTestServer(t *testing.T, routes map[string]http.HandlerFunc) (*httptest.Server, *http.Client) {
	t.Helper()

	mux := http.NewServeMux()
	for prefix, h := range routes {
		mux.HandleFunc(prefix, h)
	}

	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)

	return server, server.Client()
}

func TestWKDResolver_ResolveAdvanced(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	keyBytes := serializeBinaryKey(t, testEd25519.entity)

	server, client := newWKDTestServer(t, map[string]http.HandlerFunc{
		"/.well-known/openpgpkey/phpboyscout.uk/hu/": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(keyBytes)
		},
		"/.well-known/openpgpkey/hu/": func(w http.ResponseWriter, _ *http.Request) {
			t.Errorf("direct URL should not be hit when advanced succeeds")
			w.WriteHeader(http.StatusInternalServerError)
		},
	})

	r, err := NewWKDResolver(WKDResolverConfig{
		Email:       "release@phpboyscout.uk",
		HTTPClient:  client,
		URLOverride: server.URL,
	})
	require.NoError(t, err)

	ts, err := r.Resolve(context.Background())
	require.NoError(t, err)
	require.NotNil(t, ts)
	assert.Len(t, ts.Fingerprints(), 1)
}

func TestWKDResolver_FallbackToDirect(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	keyBytes := serializeBinaryKey(t, testEd25519.entity)

	var (
		advancedHits int
		directHits   int
	)

	server, client := newWKDTestServer(t, map[string]http.HandlerFunc{
		"/.well-known/openpgpkey/phpboyscout.uk/hu/": func(w http.ResponseWriter, _ *http.Request) {
			advancedHits++
			http.NotFound(w, nil)
		},
		"/.well-known/openpgpkey/hu/": func(w http.ResponseWriter, _ *http.Request) {
			directHits++
			_, _ = w.Write(keyBytes)
		},
	})

	r, err := NewWKDResolver(WKDResolverConfig{
		Email:       "release@phpboyscout.uk",
		HTTPClient:  client,
		URLOverride: server.URL,
	})
	require.NoError(t, err)

	ts, err := r.Resolve(context.Background())
	require.NoError(t, err)
	require.NotNil(t, ts)
	assert.Equal(t, 1, advancedHits, "advanced URL must be tried first")
	assert.Equal(t, 1, directHits, "direct URL must be tried on advanced 404")
}

func TestWKDResolver_BothNotFound(t *testing.T) {
	t.Parallel()

	server, client := newWKDTestServer(t, map[string]http.HandlerFunc{
		"/": func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) },
	})

	r, err := NewWKDResolver(WKDResolverConfig{
		Email:       "release@phpboyscout.uk",
		HTTPClient:  client,
		URLOverride: server.URL,
	})
	require.NoError(t, err)

	_, err = r.Resolve(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyResolverUnavailable)
}

func TestWKDResolver_Non200DoesNotFallBack(t *testing.T) {
	t.Parallel()

	var directHits int

	server, client := newWKDTestServer(t, map[string]http.HandlerFunc{
		"/.well-known/openpgpkey/phpboyscout.uk/hu/": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "internal", http.StatusInternalServerError)
		},
		"/.well-known/openpgpkey/hu/": func(w http.ResponseWriter, _ *http.Request) {
			directHits++
			w.WriteHeader(http.StatusOK)
		},
	})

	r, err := NewWKDResolver(WKDResolverConfig{
		Email:       "release@phpboyscout.uk",
		HTTPClient:  client,
		URLOverride: server.URL,
	})
	require.NoError(t, err)

	_, err = r.Resolve(context.Background())
	require.ErrorIs(t, err, ErrKeyResolverUnavailable)
	assert.Zero(t, directHits, "direct URL must not be tried on non-404 advanced failure")
}

func TestWKDResolver_RejectsLargeResponse(t *testing.T) {
	t.Parallel()

	server, client := newWKDTestServer(t, map[string]http.HandlerFunc{
		"/.well-known/openpgpkey/phpboyscout.uk/hu/": func(w http.ResponseWriter, _ *http.Request) {
			// One byte over the cap.
			body := make([]byte, MaxWKDResponseSize+1)
			_, _ = w.Write(body)
		},
	})

	r, err := NewWKDResolver(WKDResolverConfig{
		Email:       "release@phpboyscout.uk",
		HTTPClient:  client,
		URLOverride: server.URL,
	})
	require.NoError(t, err)

	_, err = r.Resolve(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrWKDResponseTooLarge)
}

func TestWKDResolver_RejectsWeakKey(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	keyBytes := serializeBinaryKey(t, testRSA1024.entity)

	server, client := newWKDTestServer(t, map[string]http.HandlerFunc{
		"/.well-known/openpgpkey/phpboyscout.uk/hu/": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(keyBytes)
		},
	})

	r, err := NewWKDResolver(WKDResolverConfig{
		Email:       "release@phpboyscout.uk",
		HTTPClient:  client,
		URLOverride: server.URL,
	})
	require.NoError(t, err)

	_, err = r.Resolve(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrWeakKey,
		"WKD-fetched weak key must fail with ErrWeakKey, matching embedded behaviour")
}

func TestWKDResolver_MalformedKey(t *testing.T) {
	t.Parallel()

	server, client := newWKDTestServer(t, map[string]http.HandlerFunc{
		"/.well-known/openpgpkey/phpboyscout.uk/hu/": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not a real key"))
		},
	})

	r, err := NewWKDResolver(WKDResolverConfig{
		Email:       "release@phpboyscout.uk",
		HTTPClient:  client,
		URLOverride: server.URL,
	})
	require.NoError(t, err)

	_, err = r.Resolve(context.Background())
	require.Error(t, err)
	// Malformed response surfaces the parser error directly (not the
	// unavailable sentinel); the manifest signature check will still
	// fail because no trust set was loaded.
	assert.NotErrorIs(t, err, ErrKeyResolverUnavailable,
		"malformed response is distinct from network failure")
}

func TestWKDResolver_NetworkError(t *testing.T) {
	t.Parallel()

	// Start a server, capture its URL, then close it to guarantee
	// connection refused on dial.
	server := httptest.NewTLSServer(http.NotFoundHandler())
	closedURL := server.URL
	client := server.Client()
	server.Close()

	r, err := NewWKDResolver(WKDResolverConfig{
		Email:       "release@phpboyscout.uk",
		HTTPClient:  client,
		URLOverride: closedURL,
	})
	require.NoError(t, err)

	_, err = r.Resolve(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyResolverUnavailable)
}

func TestWKDResolver_ContextCanceled(t *testing.T) {
	t.Parallel()

	// Handler blocks on the request's own context, so when the client
	// cancels its context the server side unblocks and Close() can
	// drain in-flight requests without needing a separate channel.
	server, client := newWKDTestServer(t, map[string]http.HandlerFunc{
		"/.well-known/openpgpkey/phpboyscout.uk/hu/": func(_ http.ResponseWriter, req *http.Request) {
			<-req.Context().Done()
		},
	})

	r, err := NewWKDResolver(WKDResolverConfig{
		Email:       "release@phpboyscout.uk",
		HTTPClient:  client,
		URLOverride: server.URL,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	t.Cleanup(cancel)

	_, err = r.Resolve(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyResolverUnavailable,
		"context cancellation must surface as ErrKeyResolverUnavailable")
}

func TestWKDResolver_MultipleKeysRotation(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	// Concatenate two binary keys — WKD's rotation-overlap pattern.
	rotation := append(serializeBinaryKey(t, testEd25519.entity), serializeBinaryKey(t, testRSA4096.entity)...)

	server, client := newWKDTestServer(t, map[string]http.HandlerFunc{
		"/.well-known/openpgpkey/phpboyscout.uk/hu/": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(rotation)
		},
	})

	r, err := NewWKDResolver(WKDResolverConfig{
		Email:       "release@phpboyscout.uk",
		HTTPClient:  client,
		URLOverride: server.URL,
	})
	require.NoError(t, err)

	ts, err := r.Resolve(context.Background())
	require.NoError(t, err)
	assert.Len(t, ts.Fingerprints(), 2,
		"rotation-overlap WKD response should yield both fingerprints")

	manifest := []byte("abc123  artifact.tar.gz\n")
	sigEd := detachSign(t, testEd25519.entity, manifest)
	sigRSA := detachSign(t, testRSA4096.entity, manifest)
	assert.NoError(t, ts.VerifyManifestSignature(manifest, sigEd))
	assert.NoError(t, ts.VerifyManifestSignature(manifest, sigRSA))
}

func TestWKDResolver_PathDerivation(t *testing.T) {
	t.Parallel()

	// Confirm the path written to the request matches the WKD draft
	// shape: hu/<32-char-zbase32> with ?l=<local> query.
	var (
		gotPath  string
		gotQuery url.Values
	)

	server, client := newWKDTestServer(t, map[string]http.HandlerFunc{
		"/.well-known/openpgpkey/phpboyscout.uk/hu/": func(w http.ResponseWriter, req *http.Request) {
			gotPath = req.URL.Path
			gotQuery = req.URL.Query()
			http.NotFound(w, nil)
		},
		"/": func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) },
	})

	r, err := NewWKDResolver(WKDResolverConfig{
		Email:       "release@phpboyscout.uk",
		HTTPClient:  client,
		URLOverride: server.URL,
	})
	require.NoError(t, err)

	_, _ = r.Resolve(context.Background())

	require.NotEmpty(t, gotPath, "advanced URL must have been hit")
	assert.True(t, strings.HasPrefix(gotPath, "/.well-known/openpgpkey/phpboyscout.uk/hu/"),
		"path should follow advanced WKD shape, got %q", gotPath)
	assert.Equal(t, "release", gotQuery.Get("l"),
		"?l= query must carry the original local part")

	hashSegment := strings.TrimPrefix(gotPath, "/.well-known/openpgpkey/phpboyscout.uk/hu/")
	assert.Len(t, hashSegment, 32, "z-base-32 hash segment must be 32 chars")
}
