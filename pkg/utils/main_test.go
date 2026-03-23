package utils

import (
	"testing"

	"github.com/phpboyscout/gtb/pkg/logger"
	"github.com/stretchr/testify/assert"
)

func TestGracefulGetPath_Success(t *testing.T) {
	l := logger.NewNoop()
	path, err := GracefulGetPath("ls", l)
	assert.NoError(t, err)
	assert.NotEmpty(t, path)
}

func TestGracefulGetPath_Failure(t *testing.T) {
	l := logger.NewNoop()
	path, err := GracefulGetPath("non_existent_cmd_xyz_123", l, "Instructions")
	assert.Error(t, err)
	assert.Empty(t, path)
}
