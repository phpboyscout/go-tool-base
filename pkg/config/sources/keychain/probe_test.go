package keychain_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/credentials"
	"gitlab.com/phpboyscout/go/features"

	keychain "gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/keychain"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// hungKeychain is a keychain behind a wedged D-Bus session: it claims to be
// available, and every call blocks without regard to its context.
type hungKeychain struct{}

func (hungKeychain) Store(context.Context, string, string, string) error {
	select {}
}

func (hungKeychain) Retrieve(context.Context, string, string) (string, error) {
	select {}
}

func (hungKeychain) Delete(context.Context, string, string) error {
	select {}
}

func (hungKeychain) Available() bool { return true }

// noKeychain restores the registry to a keychain-less process after a test.
type noKeychain struct{}

func (noKeychain) Store(context.Context, string, string, string) error { return nil }

func (noKeychain) Retrieve(context.Context, string, string) (string, error) { return "", nil }

func (noKeychain) Delete(context.Context, string, string) error { return nil }

func (noKeychain) Available() bool { return false }

// A hung keychain must fail the source within the probe's bound, not hold the
// command for config-keychain's ten-second per-call timeout (gtb#130). Not
// parallel: it swaps the process-wide credentials backend.
func TestRegisteredFactory_HungKeychainFailsFast(t *testing.T) {
	credentials.RegisterBackend(hungKeychain{})
	t.Cleanup(func() { credentials.RegisterBackend(noKeychain{}) })

	set, err := features.Resolve(features.Default().Snapshot(), nil)
	require.NoError(t, err)

	settings, err := config.NewStore(t.Context(), config.WithReaders(config.NamedSource{
		Name: "settings", Content: []byte("service: mytool\nkeys:\n  token: token\n"),
	}))
	require.NoError(t, err)

	factory := setup.ConfigSourceKindsIn(set)[keychain.Kind].Factory

	done := make(chan error, 1)

	go func() {
		backend, err := factory(t.Context(), settings.View(), nil)
		if err == nil {
			_, err = config.NewStore(t.Context(), config.WithBackend(backend))
		}
		done <- err
	}()

	select {
	case err := <-done:
		require.ErrorIs(t, err, keychain.ErrKeychainUnreachable)
	case <-time.After(5 * time.Second):
		t.Fatal("a hung keychain held the source past 5s; it must fail at the probe")
	}
}
