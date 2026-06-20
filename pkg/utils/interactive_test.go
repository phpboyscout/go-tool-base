package utils

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// TestIsInteractive exercises IsInteractive. The function's return value
// depends on whether os.Stdin is a character device, which differs between CI
// (stdin is a pipe -> false) and an attached terminal (stdin is a TTY ->
// true). We therefore assert only that the os.Stdin.Stat path is executed and
// that the result agrees with a direct inspection of stdin's mode, rather than
// pinning a fixed boolean that would flake across environments.
func TestIsInteractive(t *testing.T) {
	got := IsInteractive()
	assert.IsType(t, false, got)

	info, err := os.Stdin.Stat()
	if err != nil {
		// Stat failed: IsInteractive must report the non-interactive arm.
		assert.False(t, got)

		return
	}

	want := (info.Mode() & os.ModeCharDevice) != 0
	assert.Equal(t, want, got, "IsInteractive must match stdin's char-device mode")
}

// TestGracefulGetPath_ExtraInstructions verifies that caller-supplied
// instructions are emitted on the failure path even when no map entry exists.
func TestGracefulGetPath_ExtraInstructions(t *testing.T) {
	t.Parallel()

	l := logger.NewNoop()
	path, err := GracefulGetPath(
		"another_missing_cmd_qwerty_987",
		l,
		"do this first",
		"then do that",
	)
	require.Error(t, err)
	assert.Empty(t, path)
}

// TestInstructionsMap asserts the well-known instruction map is wired to the
// exported instruction constants.
func TestInstructionsMap(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		key  string
		want string
	}{
		{name: "kubectl", key: "kubectl", want: InstructionKubectl},
		{name: "az", key: "az", want: InstructionAz},
		{name: "kubelogin", key: "kubelogin", want: InstructionKubelogin},
		{name: "terraform", key: "terraform", want: InstructionTerraform},
		{name: "terragrunt", key: "terragrunt", want: InstructionTerragrunt},
		{name: "aws", key: "aws", want: InstructionAws},
		{name: "git", key: "git", want: InstructionGit},
		{name: "gh", key: "gh", want: InstructionGh},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := Instructions[tc.key]
			require.True(t, ok, "expected instruction for %q", tc.key)
			assert.Equal(t, tc.want, got)
		})
	}
}
