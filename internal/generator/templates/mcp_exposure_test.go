package templates

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func renderCommand(t *testing.T, exposure setup.MCPExposure) string {
	t.Helper()

	data := CommandData{
		Package:     "post",
		PascalName:  "Post",
		Name:        "post",
		Short:       "publish",
		Long:        "publish",
		MCPExposure: exposure,
	}

	var buf bytes.Buffer
	require.NoError(t, CommandRegistration(data).Render(&buf))

	return buf.String()
}

func TestCommandRegistration_MCPExposureMarker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		exposure    setup.MCPExposure
		wantPresent string
		wantAbsent  string
	}{
		{
			name:        "excluded emits ExcludeFromMCP",
			exposure:    setup.MCPExposureExcluded,
			wantPresent: "setup.ExcludeFromMCP(cmd)",
			wantAbsent:  "setup.IncludeInMCP(cmd)",
		},
		{
			name:        "exposed emits IncludeInMCP",
			exposure:    setup.MCPExposureExposed,
			wantPresent: "setup.IncludeInMCP(cmd)",
			wantAbsent:  "setup.ExcludeFromMCP(cmd)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := renderCommand(t, tc.exposure)
			assert.Contains(t, src, tc.wantPresent)
			assert.NotContains(t, src, tc.wantAbsent)
			// The marker must follow the cmd assignment, not replace it.
			assert.Contains(t, src, "cmd := setup.Wrap(\"post\", &cobra.Command{")
		})
	}
}

func TestCommandRegistration_MCPExposureInherit_EmitsNoMarker(t *testing.T) {
	t.Parallel()

	src := renderCommand(t, setup.MCPExposureInherit)

	assert.NotContains(t, src, "ExcludeFromMCP", "inherit must not stamp an exclusion marker")
	assert.NotContains(t, src, "IncludeInMCP", "inherit must not stamp an inclusion marker")
}

// TestCommandRegistration_MCPExposureZeroValueIsInherit asserts that the
// CommandData zero value (no MCPExposure set) emits no marker, so existing
// generated commands are unchanged.
func TestCommandRegistration_MCPExposureZeroValueIsInherit(t *testing.T) {
	t.Parallel()

	data := CommandData{Package: "x", PascalName: "X", Name: "x", Short: "x", Long: "x"}

	var buf bytes.Buffer
	require.NoError(t, CommandRegistration(data).Render(&buf))
	src := buf.String()

	assert.NotContains(t, src, "ExcludeFromMCP")
	assert.NotContains(t, src, "IncludeInMCP")
}
