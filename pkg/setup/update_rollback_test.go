package setup

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgetest "gitlab.com/phpboyscout/go/forge/test"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Spec 0104 D2: the release before the running one is the highest version
// below it, not the publish order a static manifest's previous records.
func TestReleaseBefore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current string
		listed  []string // newest published first
		want    string
	}{
		{
			name:    "a backport published after the running release is still the highest below it",
			current: "v1.3.0",
			listed:  []string{"v1.2.1", "v1.3.0", "v1.2.0"},
			want:    "v1.2.1",
		},
		{
			name:    "from a stable release, prereleases are not candidates",
			current: "v1.3.0",
			listed:  []string{"v1.3.0", "v1.3.0-rc.1", "v1.2.0"},
			want:    "v1.2.0",
		},
		{
			name:    "from a prerelease, prereleases are candidates",
			current: "v1.3.0-rc.2",
			listed:  []string{"v1.3.0-rc.2", "v1.3.0-rc.1", "v1.2.0"},
			want:    "v1.3.0-rc.1",
		},
		{
			name:    "newer releases are ignored",
			current: "v1.1.0",
			listed:  []string{"v1.3.0", "v1.2.0", "v1.1.0", "v1.0.0"},
			want:    "v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := make([]forgetest.Option, 0, len(tt.listed))
			for _, tag := range tt.listed {
				opts = append(opts, forgetest.WithRelease(tag))
			}

			s, _ := newE2EUpdater(t, forgetest.New(opts...))

			got, err := s.ReleaseBefore(context.Background(), tt.current)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReleaseBefore_NothingEarlier(t *testing.T) {
	t.Parallel()

	s, _ := newE2EUpdater(t, forgetest.New(forgetest.WithRelease("v1.0.0")))

	_, err := s.ReleaseBefore(context.Background(), "v1.0.0")

	require.ErrorIs(t, err, ErrNoEarlierRelease)
	assert.Contains(t, err.Error(), "v1.0.0", "the error names the running version")
	assert.Contains(t, hintOf(t, err), "--rollback=")
	assert.NotContains(t, hintOf(t, err), "--from-file", "a forge has no static floor")
}

// Spec 0104 D8: the static channel only reaches releases published since the
// tool moved to it, and the hint says how to install an older one.
func TestReleaseBefore_NothingEarlierOnTheStaticChannelNamesTheFloor(t *testing.T) {
	t.Parallel()

	s, _ := newE2EUpdater(t, forgetest.New(forgetest.WithRelease("v1.0.0")))
	s.Tool.ReleaseSource = props.ReleaseSource{Type: props.ReleaseSourceStatic, BaseURL: "https://pkg.example.internal/acme/tool"}

	_, err := s.ReleaseBefore(context.Background(), "v1.0.0")

	require.ErrorIs(t, err, ErrNoEarlierRelease)
	assert.Contains(t, hintOf(t, err), "--from-file")
}

func TestRollback_OnTheStaticChannel(t *testing.T) {
	t.Parallel()

	store := newStaticStore(t)
	for _, tag := range []string{"v1.0.0", "v1.1.0", "v1.2.0"} {
		archive := forgetest.TarGzAsset(e2eToolName, e2eToolName, "binary-"+tag)
		store.publish(t, tag, archive, forgetest.Manifest(false, archive), nil)
	}

	ch, err := newStaticChannel(store.base(), store.Client())
	require.NoError(t, err)

	s, currentBin := newE2EUpdater(t, nil)
	withReleaseChannel(ch)(s)
	s.Tool.ReleaseSource = props.ReleaseSource{Type: props.ReleaseSourceStatic, BaseURL: store.base()}
	s.CurrentVersion = "v1.2.0"
	s.requireChecksum = true

	target, err := s.ReleaseBefore(context.Background(), s.CurrentVersion)
	require.NoError(t, err)
	assert.Equal(t, "v1.1.0", target)

	s.version = target
	_, err = s.Update(context.Background())
	require.NoError(t, err)

	got, err := afero.ReadFile(s.Fs, currentBin)
	require.NoError(t, err)
	assert.Equal(t, "binary-v1.1.0", string(got))
}

func TestRollback_ATagBelowTheStaticFloorPointsAtFromFile(t *testing.T) {
	t.Parallel()

	store := newStaticStore(t)
	archive := forgetest.TarGzAsset(e2eToolName, e2eToolName, "binary")
	store.publish(t, "v1.2.0", archive, forgetest.Manifest(false, archive), nil)

	ch, err := newStaticChannel(store.base(), store.Client())
	require.NoError(t, err)

	s, _ := newE2EUpdater(t, nil)
	withReleaseChannel(ch)(s)
	s.Tool.ReleaseSource = props.ReleaseSource{Type: props.ReleaseSourceStatic, BaseURL: store.base()}
	s.CurrentVersion = "v1.2.0"
	s.version = "v0.44.0"

	_, err = s.Update(context.Background())

	require.Error(t, err)
	assert.Contains(t, hintOf(t, err), "--from-file")
}

func TestIsLatestVersion_NamesARequestedOlderVersionAsRequested(t *testing.T) {
	t.Parallel()

	s, _ := newE2EUpdater(t, forgetest.New(forgetest.WithRelease("v1.1.0"), forgetest.WithRelease("v1.0.0")))
	s.CurrentVersion = "v1.1.0"
	s.version = "v1.0.0"

	latest, message, err := s.IsLatestVersion(context.Background())
	require.NoError(t, err)
	assert.False(t, latest)
	assert.NotContains(t, message, "as latest")
	assert.Contains(t, message, "v1.0.0")
}
