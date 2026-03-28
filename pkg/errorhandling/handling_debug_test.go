package errorhandling

import (
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func TestCheckDebug(t *testing.T) {
	log := logger.NewBuffer()
	log.SetLevel(logger.DebugLevel)

	h := New(log, nil)

	err := errors.New("debug error")
	h.Check(err, "", LevelError)

	entries := log.Entries()
	require.NotEmpty(t, entries)
	assert.Contains(t, entries[0].Message, "debug error")
	assert.Contains(t, entries[0].Keyvals, KeyStacktrace)
}

func TestCheckStacktrace(t *testing.T) {
	log := logger.NewBuffer()
	log.SetLevel(logger.InfoLevel)

	h := New(log, nil)

	err := errors.New("stacktrace error")
	h.Check(err, "", LevelError)

	entries := log.Entries()
	require.NotEmpty(t, entries)
	assert.NotContains(t, entries[0].Keyvals, KeyStacktrace)
}
