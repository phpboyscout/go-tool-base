package props_test

import (
	"log/slog"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestNew_FillsLogLevel: a Props built the blessed way carries the level the
// root's --debug and config reload move, so a command that builds its own slog
// handler follows them without the root threading a LevelVar (spec 0202 D4).
func TestNew_FillsLogLevel(t *testing.T) {
	t.Parallel()

	p, err := props.New(props.Tool{Name: "t"}, logger.NewNoop(), afero.NewMemMapFs())
	require.NoError(t, err)

	require.NotNil(t, p.LogLevel)
	assert.Equal(t, slog.LevelInfo, p.LogLevel.Level(), "the level starts at info, slog's zero")
	assert.Same(t, p.LogLevel, p.GetLogLevel())
}

// TestApplyDefaults_FillsLogLevelOnce: a literal Props gets a level from
// ApplyDefaults, and a second call keeps the same var, so a handler built
// between the two calls still follows it.
func TestApplyDefaults_FillsLogLevelOnce(t *testing.T) {
	t.Parallel()

	p := &props.Props{Logger: logger.NewNoop()}
	p.ApplyDefaults()
	first := p.LogLevel
	require.NotNil(t, first)

	first.Set(slog.LevelDebug)
	p.ApplyDefaults()

	assert.Same(t, first, p.LogLevel)
	assert.Equal(t, slog.LevelDebug, p.GetLogLevel().Level())
}

// TestGetLogLevel_NilFieldIsStable: reading the level on a Props nobody
// defaulted creates it once, so two readers share one var rather than each
// getting a fresh one that the other's Set never reaches.
func TestGetLogLevel_NilFieldIsStable(t *testing.T) {
	t.Parallel()

	p := &props.Props{}
	first := p.GetLogLevel()
	require.NotNil(t, first)
	assert.Same(t, first, p.GetLogLevel())

	var nilProps *props.Props
	assert.NotNil(t, nilProps.GetLogLevel(), "a nil Props answers with a throwaway var rather than panicking")
}
