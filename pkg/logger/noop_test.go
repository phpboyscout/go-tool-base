package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNoopBackend_AllMethods(t *testing.T) {
	l := NewNoop()

	// None of these should panic
	l.Debug("debug")
	l.Info("info")
	l.Warn("warn")
	l.Error("error")
	l.Debugf("debug %d", 1)
	l.Infof("info %d", 1)
	l.Warnf("warn %d", 1)
	l.Errorf("error %d", 1)
	l.Print("print")
	l.With("key", "value").Info("with")
	l.WithPrefix("prefix").Info("prefixed")
	l.SetLevel(DebugLevel)
	l.SetFormatter(JSONFormatter)
}

func TestNoopBackend_GetLevel(t *testing.T) {
	l := NewNoop()
	assert.Equal(t, InfoLevel, l.GetLevel())

	l.SetLevel(ErrorLevel)
	assert.Equal(t, ErrorLevel, l.GetLevel())
}

func TestNoopBackend_Handler(t *testing.T) {
	l := NewNoop()
	h := l.Handler()
	assert.NotNil(t, h)
}

func TestNoopBackend_InterfaceSatisfaction(t *testing.T) {
	_ = NewNoop()
}
