package errorhandling

import (
	"bytes"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
)

func TestCheckDebug(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewWithOptions(&buf, log.Options{
		Level:     log.DebugLevel,
		Formatter: log.TextFormatter,
	})

	h := New(logger, nil)

	err := errors.New("debug error")
	h.Check(err, "", LevelError)

	assert.Contains(t, buf.String(), "debug error")
	assert.Contains(t, buf.String(), "stacktrace")
}

func TestCheckStacktrace(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewWithOptions(&buf, log.Options{
		Level:     log.InfoLevel,
		Formatter: log.TextFormatter,
	})

	h := New(logger, nil)

	err := errors.New("stacktrace error")
	h.Check(err, "", LevelError)

	assert.NotContains(t, buf.String(), "stacktrace=")
}
