package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go/errors"
)

func TestPersistError_HintsOnlyWithNoHome(t *testing.T) {
	t.Parallel()

	cause := errors.New("permission denied")

	assert.Contains(t, errors.FlattenHints(persistError(cause, "")), "No home directory")
	assert.Empty(t, errors.FlattenHints(persistError(cause, "/home/someone/.tool")))
	assert.ErrorIs(t, persistError(cause, ""), cause)
}
