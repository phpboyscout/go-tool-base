package setup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/forge"
	mockRelease "gitlab.com/phpboyscout/go/forge/mocks"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestGetReleaseNotes_Real(t *testing.T) {
	t.Parallel()

	// Setup Mocks
	mockClient := mockRelease.NewMockProvider(t)

	mockRelease1 := mockRelease.NewMockRelease(t)
	mockRelease1.EXPECT().GetTagName().Return("v1.2.0")
	mockRelease1.On("GetBody").Return("New feature").Maybe()
	mockRelease1.EXPECT().GetDraft().Return(false)

	mockRelease2 := mockRelease.NewMockRelease(t)
	mockRelease2.EXPECT().GetTagName().Return("v1.1.0")
	mockRelease2.On("GetBody").Return("Fix stuff").Maybe()
	mockRelease2.EXPECT().GetDraft().Return(false)

	mockRelease3 := mockRelease.NewMockRelease(t)
	mockRelease3.EXPECT().GetTagName().Return("v1.0.0")
	mockRelease3.On("GetBody").Return("Initial").Maybe()
	mockRelease3.EXPECT().GetDraft().Return(false)

	mockClient.EXPECT().ListReleases(mock.Anything, "org", "repo", 100).Return([]forge.Release{
		mockRelease1, mockRelease2, mockRelease3,
	}, nil).Once()

	updater := &SelfUpdater{
		Tool: props.Tool{
			ReleaseSource: props.ReleaseSource{Type: "github", Owner: "org", Repo: "repo"},
		},
		releaseClient: mockClient,
	}

	// Test
	notes, err := updater.GetReleaseNotes(context.Background(), "v1.0.0", "v1.2.0")
	require.NoError(t, err)
	assert.Contains(t, notes, "New feature")
	assert.Contains(t, notes, "Fix stuff")
	assert.Contains(t, notes, "# v1.1.0")
	assert.Contains(t, notes, "# v1.2.0")

	// Test No notes
	mockClient.EXPECT().ListReleases(mock.Anything, "org", "repo", 100).Return([]forge.Release{}, nil).Once()
	notes, err = updater.GetReleaseNotes(context.Background(), "v1.0.0", "v1.2.0")
	require.NoError(t, err)
	assert.Contains(t, notes, "No release notes found between")
}

// TestGetStructuredReleaseNotes_APIFallback covers the no-archive path: notes
// are fetched per-release via the provider and parsed into a Changelog whose
// From/To versions are stamped from the call arguments.
func TestGetStructuredReleaseNotes_APIFallback(t *testing.T) {
	t.Parallel()

	mockClient := mockRelease.NewMockProvider(t)

	r1 := mockRelease.NewMockRelease(t)
	r1.EXPECT().GetTagName().Return("v1.2.0")
	r1.On("GetBody").Return("New feature").Maybe()
	r1.EXPECT().GetDraft().Return(false)

	r2 := mockRelease.NewMockRelease(t)
	r2.EXPECT().GetTagName().Return("v1.0.0")
	r2.On("GetBody").Return("Initial").Maybe()
	r2.EXPECT().GetDraft().Return(false)

	mockClient.EXPECT().ListReleases(mock.Anything, "org", "repo", 100).
		Return([]forge.Release{r1, r2}, nil).Once()

	updater := &SelfUpdater{
		Tool: props.Tool{
			ReleaseSource: props.ReleaseSource{Type: "github", Owner: "org", Repo: "repo"},
		},
		releaseClient: mockClient,
	}

	cl, err := updater.GetStructuredReleaseNotes(context.Background(), "v1.0.0", "v1.2.0")
	require.NoError(t, err)
	require.NotNil(t, cl)
	assert.Equal(t, "v1.0.0", cl.FromVersion)
	assert.Equal(t, "v1.2.0", cl.ToVersion)
}
