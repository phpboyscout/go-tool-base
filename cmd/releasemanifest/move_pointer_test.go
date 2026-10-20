package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The scaffold ships its own copy of the publish script; both must behave
// the same, so they are kept byte-identical.
var movePointerScripts = map[string]string{
	"repository": "../../scripts/move-pointer.sh",
	"scaffold":   "../../cli/pkg/generator/assets/skeleton/scripts/move-pointer.sh",
}

func TestMovePointerScripts_AreIdentical(t *testing.T) {
	t.Parallel()

	own, err := os.ReadFile(movePointerScripts["repository"])
	require.NoError(t, err)

	scaffold, err := os.ReadFile(movePointerScripts["scaffold"])
	require.NoError(t, err)

	assert.True(t, bytes.Equal(own, scaffold), "scripts/move-pointer.sh and the scaffold's copy have drifted")
}

// A prerelease tag leaves no dist/latest.json (writeDocuments), and the
// publisher still runs the script, so an absent file is a clean no-op. The
// check runs before the credential check: with no credentials set, reaching
// that check would exit 1.
func TestMovePointerScripts_AbsentPointerIsANoOp(t *testing.T) {
	t.Parallel()

	for name, script := range movePointerScripts {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			absent := filepath.Join(t.TempDir(), "latest.json")

			cmd := exec.CommandContext(t.Context(), "bash", script, absent, "https://store.invalid/bucket/latest.json")
			cmd.Env = []string{"PATH=" + os.Getenv("PATH")}

			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
			assert.Contains(t, string(out), "no pointer to move")
		})
	}
}
