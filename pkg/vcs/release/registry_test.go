package release_test

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// stubProvider is a minimal release.Provider used in registry tests.
type stubProvider struct{}

func (s *stubProvider) GetLatestRelease(_ context.Context, _, _ string) (release.Release, error) {
	return nil, nil
}

func (s *stubProvider) GetReleaseByTag(_ context.Context, _, _, _ string) (release.Release, error) {
	return nil, nil
}

func (s *stubProvider) ListReleases(_ context.Context, _, _ string, _ int) ([]release.Release, error) {
	return nil, nil
}

func (s *stubProvider) DownloadReleaseAsset(_ context.Context, _, _ string, _ release.ReleaseAsset) (io.ReadCloser, string, error) {
	return nil, "", nil
}

func stubFactory(_ release.ReleaseSourceConfig, _ config.Containable) (release.Provider, error) {
	return &stubProvider{}, nil
}

func TestRegistry_Register_And_Lookup(t *testing.T) {
	t.Parallel()

	const testType = "test-register-lookup"

	release.Register(testType, stubFactory)

	factory, err := release.Lookup(testType)
	require.NoError(t, err)
	require.NotNil(t, factory)

	provider, err := factory(release.ReleaseSourceConfig{}, nil)
	require.NoError(t, err)
	assert.NotNil(t, provider)
}

func TestRegistry_Lookup_NotFound(t *testing.T) {
	t.Parallel()

	_, err := release.Lookup("this-type-does-not-exist-xyz")
	require.ErrorIs(t, err, release.ErrProviderNotFound)
}

func TestRegistry_RegisteredTypes_IncludesRegistered(t *testing.T) {
	t.Parallel()

	const testType = "test-registered-types"

	release.Register(testType, stubFactory)

	types := release.RegisteredTypes()
	assert.Contains(t, types, testType)
}

func TestRegistry_RegisteredTypes_IsSorted(t *testing.T) {
	t.Parallel()

	types := release.RegisteredTypes()

	for i := 1; i < len(types); i++ {
		assert.LessOrEqual(t, types[i-1], types[i], "RegisteredTypes should be sorted")
	}
}

func TestRegistry_Overwrite(t *testing.T) {
	t.Parallel()

	const testType = "test-overwrite"

	var called int

	release.Register(testType, func(_ release.ReleaseSourceConfig, _ config.Containable) (release.Provider, error) {
		called = 1
		return &stubProvider{}, nil
	})
	release.Register(testType, func(_ release.ReleaseSourceConfig, _ config.Containable) (release.Provider, error) {
		called = 2
		return &stubProvider{}, nil
	})

	factory, err := release.Lookup(testType)
	require.NoError(t, err)

	_, err = factory(release.ReleaseSourceConfig{}, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, called, "second registration should overwrite the first")
}

func TestRegistry_Concurrent(t *testing.T) {
	t.Parallel()

	const goroutines = 20

	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()

			typeKey := fmt.Sprintf("test-concurrent-%d", n)
			release.Register(typeKey, stubFactory)
			_, _ = release.Lookup(typeKey)
			_ = release.RegisteredTypes()
		}(i)
	}

	wg.Wait()
}
