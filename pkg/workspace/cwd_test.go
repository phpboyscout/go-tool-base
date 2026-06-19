package workspace_test

import (
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/workspace"
)

func TestDetectFromCWD(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	require.NoError(t, err)

	tests := []struct {
		name       string
		setup      func(fs afero.Fs)
		markers    []string
		wantErr    error
		wantRoot   string
		wantMarker string
	}{
		{
			name: "marker at cwd",
			setup: func(fs afero.Fs) {
				require.NoError(t, afero.WriteFile(fs, cwd+"/go.mod", []byte("module test"), 0o644))
			},
			markers:    workspace.DefaultMarkers,
			wantRoot:   cwd,
			wantMarker: "go.mod",
		},
		{
			name: "no marker found",
			setup: func(fs afero.Fs) {
				require.NoError(t, fs.MkdirAll(cwd, 0o755))
			},
			markers: workspace.DefaultMarkers,
			wantErr: workspace.ErrNotFound,
		},
		{
			name: "empty markers yields not found",
			setup: func(fs afero.Fs) {
				require.NoError(t, fs.MkdirAll(cwd, 0o755))
			},
			markers: nil,
			wantErr: workspace.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			tt.setup(fs)

			ws, err := workspace.DetectFromCWD(fs, tt.markers)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantRoot, ws.Root)
			assert.Equal(t, tt.wantMarker, ws.Marker)
		})
	}
}

func TestDetectFromCWD_WithMaxDepth(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	require.NoError(t, err)

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, cwd+"/go.mod", []byte("module test"), 0o644))

	ws, err := workspace.DetectFromCWD(fs, workspace.DefaultMarkers, workspace.WithMaxDepth(1))
	require.NoError(t, err)

	assert.Equal(t, cwd, ws.Root)
	assert.Equal(t, "go.mod", ws.Marker)
}
