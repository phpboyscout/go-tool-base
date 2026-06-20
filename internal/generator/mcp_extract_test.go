package generator

import (
	"bytes"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// renderPostCmd renders a real cmd.go for a "post" command at the given
// exposure, so the extractor is tested against genuine template output.
func renderPostCmd(t *testing.T, exposure setup.MCPExposure) []byte {
	t.Helper()

	data := templates.CommandData{
		Package:     "post",
		PascalName:  "Post",
		Name:        "post",
		Short:       "publish",
		Long:        "publish",
		OmitRun:     true,
		MCPExposure: exposure,
	}

	var buf bytes.Buffer
	require.NoError(t, templates.CommandRegistration(data).Render(&buf))

	return buf.Bytes()
}

func TestDetectMCPExposure_RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		exposure setup.MCPExposure
		assert   func(t *testing.T, got *bool)
	}{
		{
			name:     "excluded marker -> mcp_enabled false",
			exposure: setup.MCPExposureExcluded,
			assert: func(t *testing.T, got *bool) {
				t.Helper()
				require.NotNil(t, got)
				assert.False(t, *got)
			},
		},
		{
			name:     "exposed marker -> mcp_enabled true",
			exposure: setup.MCPExposureExposed,
			assert: func(t *testing.T, got *bool) {
				t.Helper()
				require.NotNil(t, got)
				assert.True(t, *got)
			},
		},
		{
			name:     "no marker -> mcp_enabled nil",
			exposure: setup.MCPExposureInherit,
			assert: func(t *testing.T, got *bool) {
				t.Helper()
				assert.Nil(t, got)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			p := &props.Props{FS: fs, Logger: logger.NewNoop()}
			path := "/work/pkg/cmd/post/cmd.go"
			require.NoError(t, afero.WriteFile(fs, path, renderPostCmd(t, tc.exposure), 0o644))

			g := New(p, &Config{Path: "/work"})

			cmd, _, _, err := g.extractCommandMetadata(path)
			require.NoError(t, err)
			require.Equal(t, "post", cmd.Name)

			tc.assert(t, cmd.MCPEnabled)
		})
	}
}
