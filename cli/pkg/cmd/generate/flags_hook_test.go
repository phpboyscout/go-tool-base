package generate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The hook is off unless asked for (spec 0224 D1).
func TestNewCmdCommand_FlagsHookIsOffUnlessAskedFor(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		args []string
		want bool
	}{
		"by default": {},
		"asked for":  {args: []string{"--flags-hook"}, want: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p, _ := commandProject(t)

			cmd := NewCmdCommand(p, &SharedFlags{})
			cmd.SetArgs(append([]string{"--name", "sign", "--short", "Sign", "--path", commandProjectRoot}, tc.args...))
			require.NoError(t, cmd.ExecuteContext(context.Background()))

			assert.Equal(t, tc.want, findManifestCommand(t, loadCommandManifest(t, p), "sign").FlagsHook)
		})
	}
}
