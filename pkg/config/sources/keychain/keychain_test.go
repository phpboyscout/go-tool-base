package keychain

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"
)

// memKeychain is a credentials.Backend held in memory.
type memKeychain struct {
	available bool
	entries   map[string]string
}

func (m *memKeychain) Store(_ context.Context, service, account, secret string) error {
	m.entries[service+"/"+account] = secret

	return nil
}

func (m *memKeychain) Retrieve(_ context.Context, service, account string) (string, error) {
	return m.entries[service+"/"+account], nil
}

func (m *memKeychain) Delete(_ context.Context, service, account string) error {
	delete(m.entries, service+"/"+account)

	return nil
}

func (m *memKeychain) Available() bool { return m.available }

func reachable(context.Context) bool { return true }

func settings(t *testing.T, yaml string) config.Reader {
	t.Helper()

	store, err := config.NewStore(t.Context(), config.WithReaders(config.NamedSource{Name: "settings", Content: []byte(yaml)}))
	require.NoError(t, err)

	return store.View()
}

// Spec 0204 D12: a keychain source reads the accounts its keys map names,
// under its service, and is writable and sensitive.
func TestFactory_ReadsTheDeclaredKeys(t *testing.T) {
	t.Parallel()

	kc := &memKeychain{available: true, entries: map[string]string{"mytool/instagram-token": "tok"}}

	backend, err := factoryFor(kc, reachable)(t.Context(), settings(t, "service: mytool\nkeys:\n  platforms.instagram.token: instagram-token\ntimeout: 30s\n"), nil)
	require.NoError(t, err)

	store, err := config.NewStore(t.Context(), config.WithBackend(backend))
	require.NoError(t, err)
	assert.Equal(t, "tok", store.View().GetString("platforms.instagram.token"))
	assert.True(t, backend.Capabilities().Sensitive)
}

// A binary with no keychain linked fails the factory naming the link, so an
// optional slot drops out and a required one stops the tool (D6, D12).
func TestFactory_WithoutAKeychainLinked(t *testing.T) {
	t.Parallel()

	_, err := factoryFor(&memKeychain{}, reachable)(t.Context(), settings(t, "service: mytool\n"), nil)
	require.ErrorIs(t, err, ErrNoKeychain)
	assert.Contains(t, errors.FlattenHints(err), "pkg/setup/keychain")
}

func TestFactory_NeedsAService(t *testing.T) {
	t.Parallel()

	_, err := factoryFor(&memKeychain{available: true}, reachable)(t.Context(), settings(t, "keys: {}\n"), nil)
	require.ErrorIs(t, err, ErrNoService)
}

// A keychain that fails its probe fails the factory, so an optional slot drops
// out and a required one is refused, without a single read attempted (gtb#130).
func TestFactory_UnreachableKeychain(t *testing.T) {
	t.Parallel()

	kc := &memKeychain{available: true, entries: map[string]string{}}
	unreachable := func(context.Context) bool { return false }

	_, err := factoryFor(kc, unreachable)(t.Context(), settings(t, "service: mytool\n"), nil)
	require.ErrorIs(t, err, ErrKeychainUnreachable)
	assert.Contains(t, errors.FlattenHints(err), "timeout")
}

// The probe is bounded by DefaultProbeTimeout, or by the source's own timeout
// when it sets one, so a slot that allows for an unlock prompt still gets it.
func TestFactory_ProbeDeadline(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		yaml string
		want time.Duration
	}{
		"default":    {yaml: "service: mytool\n", want: DefaultProbeTimeout},
		"configured": {yaml: "service: mytool\ntimeout: 45s\n", want: 45 * time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var got time.Duration

			probe := func(ctx context.Context) bool {
				deadline, ok := ctx.Deadline()
				require.True(t, ok)

				got = time.Until(deadline)

				return true
			}

			_, err := factoryFor(&memKeychain{available: true}, probe)(t.Context(), settings(t, tc.yaml), nil)
			require.NoError(t, err)
			assert.InDelta(t, tc.want, got, float64(time.Second))
		})
	}
}
