package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func TestGracefulGetPath_Success(t *testing.T) {
	l := logger.NewNoop()
	path, err := GracefulGetPath("ls", l)
	require.NoError(t, err)
	assert.NotEmpty(t, path)
}

func TestGracefulGetPath_Failure(t *testing.T) {
	l := logger.NewNoop()
	path, err := GracefulGetPath("non_existent_cmd_xyz_123", l, "Instructions")
	require.Error(t, err)
	assert.Empty(t, path)
}

func TestGracefulGetPath_KnownInstruction(t *testing.T) {
	// Add a temporary entry to the Instructions map so the known-instruction
	// branch is exercised without depending on system tooling.
	const testCmd = "gtb-test-tool-xyz"
	Instructions[testCmd] = "Install test tool: example.com/install"
	t.Cleanup(func() { delete(Instructions, testCmd) })

	l := logger.NewNoop()
	path, err := GracefulGetPath(testCmd, l)
	require.Error(t, err)
	assert.Empty(t, path)
}
