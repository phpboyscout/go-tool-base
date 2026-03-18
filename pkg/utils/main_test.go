package utils

import (
	"io"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/assert"
)

func TestGracefulGetPath_Success(t *testing.T) {
	logger := log.New(io.Discard)
	path, err := GracefulGetPath("ls", logger)
	assert.NoError(t, err)
	assert.NotEmpty(t, path)
}

func TestGracefulGetPath_Failure(t *testing.T) {
	logger := log.New(io.Discard)
	path, err := GracefulGetPath("non_existent_cmd_xyz_123", logger, "Instructions")
	assert.Error(t, err)
	assert.Empty(t, path)
}
