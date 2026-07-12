package chat

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The HTTP and keychain seams are what keep the chat core free of any specific
// framework. These tests lock in the injection contract the GTB adapter relies
// on and a bare consumer can use.

func TestChatHTTPClient_PrefersInjectedClient(t *testing.T) {
	t.Parallel()

	injected := &http.Client{Timeout: 42 * time.Second}

	got := chatHTTPClient(Config{HTTPClient: injected})

	assert.Same(t, injected, got, "an injected Config.HTTPClient must be used verbatim")
}

func TestChatHTTPClient_DefaultsToPlainBoundedClient(t *testing.T) {
	t.Parallel()

	got := chatHTTPClient(Config{RequestTimeout: 90 * time.Second})

	require.NotNil(t, got)
	assert.Equal(t, 90*time.Second, got.Timeout, "default client honours Config.RequestTimeout")
}

func TestChatHTTPClient_DefaultTimeoutFallback(t *testing.T) {
	t.Parallel()

	got := chatHTTPClient(Config{})

	require.NotNil(t, got)
	assert.Equal(t, DefaultChatRequestTimeout, got.Timeout)
}

func TestResolveFromCredentialConfig_NilLookupSkipsKeychain(t *testing.T) {
	t.Parallel()

	// Keychain reference set, but no lookup injected: the step is skipped and
	// resolution falls through to the literal Key.
	got := resolveFromCredentialConfig(context.Background(), CredentialConfig{
		Keychain: "svc/acct",
		Key:      "literal-fallback",
	})

	assert.Equal(t, "literal-fallback", got)
}

func TestResolveFromCredentialConfig_UsesInjectedLookup(t *testing.T) {
	t.Parallel()

	var gotService, gotAccount string
	lookup := func(_ context.Context, service, account string) (string, error) {
		gotService, gotAccount = service, account

		return "  secret-from-keychain  ", nil
	}

	got := resolveFromCredentialConfig(context.Background(), CredentialConfig{
		Keychain: "my-svc/my-acct",
		Key:      "should-not-win",
		Lookup:   lookup,
	})

	assert.Equal(t, "secret-from-keychain", got, "keychain wins over the literal Key and is trimmed")
	assert.Equal(t, "my-svc", gotService)
	assert.Equal(t, "my-acct", gotAccount)
}

func TestResolveFromCredentialConfig_LookupErrorFallsThrough(t *testing.T) {
	t.Parallel()

	lookup := func(_ context.Context, _, _ string) (string, error) {
		return "", errors.New("keychain unavailable")
	}

	got := resolveFromCredentialConfig(context.Background(), CredentialConfig{
		Keychain: "svc/acct",
		Key:      "literal-fallback",
		Lookup:   lookup,
	})

	assert.Equal(t, "literal-fallback", got, "a lookup error falls through to the next source")
}

func TestRetrieveFromKeychainRef_MalformedRefReturnsEmpty(t *testing.T) {
	t.Parallel()

	lookup := func(_ context.Context, _, _ string) (string, error) {
		return "should-not-be-called", nil
	}

	for _, ref := range []string{"noslash", "/leading", "trailing/"} {
		assert.Empty(t, retrieveFromKeychainRef(context.Background(), ref, lookup), "ref %q", ref)
	}
}
