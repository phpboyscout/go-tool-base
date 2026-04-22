// Package credtest provides test-only credential backends so
// downstream tests can exercise keychain-mode behaviour without
// pulling in github.com/zalando/go-keyring. Useful for:
//
//   - Tests of resolvers (pkg/chat, pkg/vcs) and setup wizards
//     (pkg/setup/ai) that want deterministic behaviour for Store /
//     Retrieve / Delete without touching the host keychain.
//   - Downstream tools that unit-test their own credential flows.
//
// MemoryBackend stores everything in an in-process map guarded by
// a mutex — nothing ever reaches a session bus or platform API,
// so this package is safe to link into a binary that must prove
// (via SBOM or similar audit) that it performs no IPC to the
// keychain infrastructure.
package credtest

import (
	"sync"

	"github.com/phpboyscout/go-tool-base/pkg/credentials"
)

// MemoryBackend is a concurrency-safe in-memory implementation of
// [credentials.Backend]. The zero value is usable; Store on an
// uninitialised MemoryBackend works correctly.
type MemoryBackend struct {
	mu    sync.RWMutex
	store map[string]string
}

// key computes the composite map key for a service/account pair.
// Internal so the map shape stays a private implementation detail.
func key(service, account string) string {
	return service + "\x00" + account
}

// Store writes a secret. Overwrites existing entries for the same
// service/account pair, matching the real keychain Backend.
func (m *MemoryBackend) Store(service, account, secret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.store == nil {
		m.store = make(map[string]string)
	}

	m.store[key(service, account)] = secret

	return nil
}

// Retrieve reads a previously-stored secret. Returns
// [credentials.ErrCredentialNotFound] when the pair has never been
// written — matching the real Backend's contract so resolvers behave
// identically against either implementation.
func (m *MemoryBackend) Retrieve(service, account string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	v, ok := m.store[key(service, account)]
	if !ok {
		return "", credentials.ErrCredentialNotFound
	}

	return v, nil
}

// Delete removes a secret. Idempotent: deleting a non-existent
// entry returns nil, matching the real Backend.
func (m *MemoryBackend) Delete(service, account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.store, key(service, account))

	return nil
}

// Available reports true so [credentials.KeychainAvailable] returns
// true and setup wizards offer the keychain option under this
// backend. Tests that want the "unavailable" path should not register
// this backend — the default stub already reports false.
func (*MemoryBackend) Available() bool {
	return true
}

// Install registers a fresh [MemoryBackend] as the process-wide
// credentials backend for the duration of the test, restoring a
// stub backend via t.Cleanup. Call this at the top of any test
// that wants to exercise real Store / Retrieve / Delete without
// actually touching the OS keychain.
//
// Returns the backend so tests can pre-populate entries directly
// if they want to seed data without going through credentials.Store.
func Install(t testingT) *MemoryBackend {
	t.Helper()

	b := &MemoryBackend{}
	credentials.RegisterBackend(b)

	t.Cleanup(func() {
		credentials.RegisterBackend(stubBackend{})
	})

	return b
}

// testingT is the subset of testing.TB this package needs. Avoids
// importing the testing package at compile time so non-test code
// that vendors credtest does not accidentally link against it.
type testingT interface {
	Helper()
	Cleanup(func())
}

// stubBackend mirrors the default stub in pkg/credentials — copied
// here so tests can restore it on cleanup without round-tripping
// through a test-only export from the main package.
type stubBackend struct{}

func (stubBackend) Store(_, _, _ string) error {
	return credentials.ErrCredentialUnsupported
}

func (stubBackend) Retrieve(_, _ string) (string, error) {
	return "", credentials.ErrCredentialUnsupported
}

func (stubBackend) Delete(_, _ string) error {
	return credentials.ErrCredentialUnsupported
}

func (stubBackend) Available() bool {
	return false
}
