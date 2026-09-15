package generator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseOverwriteMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		want    OverwriteMode
		wantErr bool
	}{
		{"", OverwriteAsk, false},
		{"ask", OverwriteAsk, false},
		{"allow", OverwriteAllow, false},
		{"deny", OverwriteDeny, false},
		{"Allow", "", true},
		{"yes", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			got, err := ParseOverwriteMode(tt.in)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidOverwriteMode)

				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestValidateHelpType(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"", "none", "slack", "teams"} {
		require.NoError(t, ValidateHelpType(ok), ok)
	}

	require.Error(t, ValidateHelpType("bogus"))
	require.Error(t, ValidateHelpType("Slack"))
}

// TestValidateManifest_RefusesUnknownHelpType pins that the manifest
// validator, not only the flag, refuses a help type the template cannot
// render.
func TestValidateManifest_RefusesUnknownHelpType(t *testing.T) {
	t.Parallel()

	m := &Manifest{Properties: ManifestProperties{Name: "tool", Help: ManifestHelp{Type: "bogus"}}}
	require.Error(t, ValidateManifest(m))
}
