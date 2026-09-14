package attach

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func subcommandNames(t *testing.T, p *props.Props) map[string]bool {
	t.Helper()

	got := map[string]bool{}
	for _, c := range NewCmdAttach(p).Commands() {
		got[c.Name()] = true
	}

	return got
}

func TestNewCmdAttach_Structure(t *testing.T) {
	t.Parallel()

	cmd := NewCmdAttach(&props.Props{}).Command
	assert.Equal(t, "attach", cmd.Use)

	names := subcommandNames(t, &props.Props{})
	for _, want := range []string{"command", "adapter", "list"} {
		assert.Truef(t, names[want], "expected 'attach %s' subcommand", want)
	}
}

func TestNewCmdAttachCommand_Flags(t *testing.T) {
	t.Parallel()

	cmd := newCmdAttachCommand(&props.Props{}).Command
	for _, f := range []string{"constructor", "arg", "wrap", "import-path", "alias", "name", "path"} {
		assert.NotNilf(t, cmd.Flags().Lookup(f), "must have --%s flag", f)
	}

	// constructor is required.
	require.NoError(t, cmd.ParseFlags(nil))
	assert.Error(t, cmd.ValidateRequiredFlags(), "constructor must be required")
}

func TestSplitModuleVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in          string
		module, ver string
		wantErr     bool
	}{
		{in: "gitlab.com/phpboyscout/go/signing-cli@v0.1.0", module: "gitlab.com/phpboyscout/go/signing-cli", ver: "v0.1.0"},
		{in: "example.com/x@v1.2.3-rc.1", module: "example.com/x", ver: "v1.2.3-rc.1"},
		{in: "no-version", wantErr: true},
		{in: "@v1.0.0", wantErr: true},
		{in: "mod@", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			m, v, err := splitModuleVersion(tc.in)
			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.module, m)
			assert.Equal(t, tc.ver, v)
		})
	}
}
