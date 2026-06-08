package signing_test

import (
	"context"
	"crypto"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/signing"
)

// fakeBackend is a minimal Backend implementation for registry tests.
// NewSigner returns a nil signer because none of the registry paths
// invoke it; tests that need a real signer wrap a real backend.
type fakeBackend struct {
	name string
}

func (f *fakeBackend) Name() string                  { return f.name }
func (f *fakeBackend) RegisterFlags(_ *pflag.FlagSet) {}
func (f *fakeBackend) NewSigner(_ context.Context, _ string) (crypto.Signer, error) {
	return nil, nil //nolint:nilnil // test stub
}

func TestRegister_AddsBackend(t *testing.T) {
	signing.ResetForTesting()
	t.Cleanup(signing.ResetForTesting)

	signing.Register(&fakeBackend{name: "test-backend"})

	got, err := signing.Get("test-backend")
	require.NoError(t, err)
	assert.Equal(t, "test-backend", got.Name())
}

func TestRegister_NilBackend_Panics(t *testing.T) {
	signing.ResetForTesting()
	t.Cleanup(signing.ResetForTesting)

	assert.PanicsWithValue(t, "signing: Register called with nil Backend", func() {
		signing.Register(nil)
	})
}

func TestRegister_EmptyName_Panics(t *testing.T) {
	signing.ResetForTesting()
	t.Cleanup(signing.ResetForTesting)

	assert.PanicsWithValue(t, "signing: Backend.Name() returned empty string", func() {
		signing.Register(&fakeBackend{name: ""})
	})
}

func TestRegister_Duplicate_Panics(t *testing.T) {
	signing.ResetForTesting()
	t.Cleanup(signing.ResetForTesting)

	signing.Register(&fakeBackend{name: "dup"})

	assert.PanicsWithValue(t, "signing: duplicate Backend registration: dup", func() {
		signing.Register(&fakeBackend{name: "dup"})
	})
}

func TestGet_UnknownBackend_ListsAvailable(t *testing.T) {
	signing.ResetForTesting()
	t.Cleanup(signing.ResetForTesting)

	signing.Register(&fakeBackend{name: "alpha"})
	signing.Register(&fakeBackend{name: "beta"})

	_, err := signing.Get("gamma")
	require.Error(t, err)
	assert.ErrorIs(t, err, signing.ErrUnknownBackend)

	msg := err.Error()
	assert.Contains(t, msg, `"gamma"`, "error must quote the requested backend name")
	assert.Contains(t, msg, "alpha", "error must list available alpha")
	assert.Contains(t, msg, "beta", "error must list available beta")

	// Available list is alphabetical so the message is stable.
	alphaIdx := strings.Index(msg, "alpha")
	betaIdx := strings.Index(msg, "beta")
	assert.Less(t, alphaIdx, betaIdx, "available list must be sorted alphabetically")
}

func TestGet_NoBackendsRegistered_HintsAtCompileTime(t *testing.T) {
	signing.ResetForTesting()
	t.Cleanup(signing.ResetForTesting)

	_, err := signing.Get("aws-kms")
	require.Error(t, err)
	assert.ErrorIs(t, err, signing.ErrUnknownBackend)
	assert.Contains(t, err.Error(), "no backends are registered",
		"empty-registry error must point operators at the compile-time blank-import requirement")
}

func TestNames_SortedAlphabetically(t *testing.T) {
	signing.ResetForTesting()
	t.Cleanup(signing.ResetForTesting)

	signing.Register(&fakeBackend{name: "zulu"})
	signing.Register(&fakeBackend{name: "alpha"})
	signing.Register(&fakeBackend{name: "mike"})

	names := signing.Names()
	assert.Equal(t, []string{"alpha", "mike", "zulu"}, names,
		"Names() must return backends in alphabetical order for stable --help output")
}

func TestNames_EmptyRegistry(t *testing.T) {
	signing.ResetForTesting()
	t.Cleanup(signing.ResetForTesting)

	names := signing.Names()
	assert.Empty(t, names)
}

// TestRegister_Concurrent verifies that Register tolerates parallel
// invocation. init() functions in different packages can run on
// different goroutines (Go spec doesn't pin init ordering across
// packages), so concurrent calls are a real possibility.
func TestRegister_Concurrent(t *testing.T) {
	signing.ResetForTesting()
	t.Cleanup(signing.ResetForTesting)

	const N = 32

	var wg sync.WaitGroup

	wg.Add(N)

	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			signing.Register(&fakeBackend{name: backendName(i)})
		}(i)
	}

	wg.Wait()

	assert.Len(t, signing.Names(), N, "all N concurrent registrations must succeed")
}

func backendName(i int) string {
	if i < 10 {
		return "backend-0" + string(rune('0'+i))
	}

	return "backend-" + string(rune('0'+(i/10))) + string(rune('0'+(i%10)))
}
