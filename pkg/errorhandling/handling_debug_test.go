package errorhandling

import (
	"bytes"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"

	"github.com/phpboyscout/gtb/pkg/logger"
)

func TestCheckDebug(t *testing.T) {
	var buf bytes.Buffer
	l := logger.NewCharm(&buf, logger.WithLevel(logger.DebugLevel))

	h := New(l, nil)

	err := errors.New("debug error")
	h.Check(err, "", LevelError)

	assert.Contains(t, buf.String(), "debug error")
	assert.Contains(t, buf.String(), "stacktrace")
}

func TestCheckStacktrace(t *testing.T) {
	var buf bytes.Buffer
	l := logger.NewCharm(&buf)

	h := New(l, nil)

	err := errors.New("stacktrace error")
	h.Check(err, "", LevelError)

	assert.NotContains(t, buf.String(), "stacktrace=")
}
