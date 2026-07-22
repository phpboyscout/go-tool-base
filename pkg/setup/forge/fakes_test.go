package forge

import (
	"context"
	"testing"

	"gitlab.com/phpboyscout/go/config"
	forgeapi "gitlab.com/phpboyscout/go/forge"
)

// fakeAuthProvider satisfies forgeapi.Provider (via the embedded nil interface,
// whose release methods are never exercised on the login path) and the optional
// forgeapi.Authenticator capability. It lets the auth tests drive captureToken
// through the capability seam without a real OAuth device flow.
type fakeAuthProvider struct {
	forgeapi.Provider
	token string
	err   error
}

func (f fakeAuthProvider) Login(_ context.Context, _ forgeapi.Prompter) (string, error) {
	return f.token, f.err
}

// authProviderFactory builds a WithProviderFactory input yielding a
// fakeAuthProvider with the given Login result.
func authProviderFactory(token string, err error) func(config.Reader) (forgeapi.Provider, error) {
	return func(config.Reader) (forgeapi.Provider, error) {
		return fakeAuthProvider{token: token, err: err}, nil
	}
}

// noAuthProvider satisfies forgeapi.Provider but NOT forgeapi.Authenticator, so
// the type assertion in authenticator() misses and yields ErrNotSupported.
type noAuthProvider struct {
	forgeapi.Provider
}

// fatalAuthProvider fails the test if Login is ever called — for asserting the
// login path is not taken (e.g. FetchToken=false).
type fatalAuthProvider struct {
	forgeapi.Provider
	t *testing.T
}

func (f fatalAuthProvider) Login(context.Context, forgeapi.Prompter) (string, error) {
	f.t.Fatal("OAuth login must not run on this path")

	return "", nil
}

// fatalOnLoginProvider builds a WithProviderFactory input whose Login fails the
// test if invoked.
func fatalOnLoginProvider(t *testing.T) func(config.Reader) (forgeapi.Provider, error) {
	t.Helper()

	return func(config.Reader) (forgeapi.Provider, error) {
		return fatalAuthProvider{t: t}, nil
	}
}

// fakeKeyManager records UploadKey calls and returns a preset error, standing in
// for the forge provider's forgeapi.KeyManager capability.
type fakeKeyManager struct {
	err        error
	uploaded   bool
	lastName   string
	lastPubKey []byte
}

func (f *fakeKeyManager) UploadKey(_ context.Context, name string, publicKey []byte) error {
	f.uploaded = true
	f.lastName = name
	f.lastPubKey = publicKey

	return f.err
}

// keyManagerFactory builds a WithKeyManager input yielding km.
func keyManagerFactory(km forgeapi.KeyManager, err error) func(config.Reader) (forgeapi.KeyManager, error) {
	return func(config.Reader) (forgeapi.KeyManager, error) {
		return km, err
	}
}
