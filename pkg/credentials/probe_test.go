package credentials_test

import (
	"context"
	"sync"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentials"
)

// fakeBackend is a configurable credentials.Backend for driving Probe's
// branches. An error can be injected per operation; otherwise it behaves like a
// working in-memory store.
type fakeBackend struct {
	available   bool
	storeErr    error
	retrieveErr error
	deleteErr   error

	mu    sync.Mutex
	store map[string]string
}

func fakeKey(service, account string) string { return service + "\x00" + account }

func (b *fakeBackend) Store(_ context.Context, service, account, secret string) error {
	if b.storeErr != nil {
		return b.storeErr
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.store == nil {
		b.store = make(map[string]string)
	}

	b.store[fakeKey(service, account)] = secret

	return nil
}

func (b *fakeBackend) Retrieve(_ context.Context, service, account string) (string, error) {
	if b.retrieveErr != nil {
		return "", b.retrieveErr
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	v, ok := b.store[fakeKey(service, account)]
	if !ok {
		return "", credentials.ErrCredentialNotFound
	}

	return v, nil
}

func (b *fakeBackend) Delete(_ context.Context, service, account string) error {
	if b.deleteErr != nil {
		return b.deleteErr
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.store, fakeKey(service, account))

	return nil
}

func (b *fakeBackend) Available() bool { return b.available }

// unsupportedBackend mirrors the package's default stub so cleanup restores the
// out-of-the-box behaviour (keychain reported unavailable; operations
// unsupported), matching what credtest.Install does on teardown.
type unsupportedBackend struct{}

func (unsupportedBackend) Store(context.Context, string, string, string) error {
	return credentials.ErrCredentialUnsupported
}

func (unsupportedBackend) Retrieve(context.Context, string, string) (string, error) {
	return "", credentials.ErrCredentialUnsupported
}

func (unsupportedBackend) Delete(context.Context, string, string) error {
	return credentials.ErrCredentialUnsupported
}

func (unsupportedBackend) Available() bool { return false }

// installBackend registers b for the duration of the test and restores the
// default-equivalent stub on cleanup. Tests using it must NOT call t.Parallel:
// the backend is a process-wide singleton (see credtest.Install's same caveat).
func installBackend(t *testing.T, b credentials.Backend) {
	t.Helper()

	credentials.RegisterBackend(b)
	t.Cleanup(func() { credentials.RegisterBackend(unsupportedBackend{}) })
}

func TestProbe_RoundTripSucceeds(t *testing.T) {
	installBackend(t, &fakeBackend{available: true})

	assert.True(t, credentials.Probe(context.Background()),
		"Probe must return true when the backend stores, reads back, and deletes cleanly")
}

func TestProbe_StoreFailureReturnsFalse(t *testing.T) {
	installBackend(t, &fakeBackend{available: true, storeErr: errors.New("store boom")})

	assert.False(t, credentials.Probe(context.Background()),
		"a backend whose Store fails is not a usable keychain")
}

func TestProbe_RetrieveFailureReturnsFalse(t *testing.T) {
	installBackend(t, &fakeBackend{available: true, retrieveErr: errors.New("retrieve boom")})

	assert.False(t, credentials.Probe(context.Background()),
		"a write-only sink (Store ok, Retrieve fails) is not a usable keychain")
}

func TestProbe_DeleteFailureReturnsFalse(t *testing.T) {
	installBackend(t, &fakeBackend{available: true, deleteErr: errors.New("delete boom")})

	assert.False(t, credentials.Probe(context.Background()),
		"a backend that cannot delete the probe entry is not a usable keychain")
}
