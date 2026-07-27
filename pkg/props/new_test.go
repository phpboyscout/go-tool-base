package props

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errorhandling"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

func TestNew_DefaultsAndOptions(t *testing.T) {
	t.Parallel()

	assets := NewAssets()
	ver := version.NewInfo("v1.2.3", "abc", "2026-01-01")
	eh := errorhandling.New(logger.ToSlog(logger.NewNoop()), nil)
	col := NoopCollector{}

	p, err := New(
		Tool{Name: "demo"},
		logger.NewNoop(),
		afero.NewMemMapFs(),
		WithAssets(assets),
		WithVersion(ver),
		WithErrorHandler(eh),
		WithCollector(col),
	)
	require.NoError(t, err)

	assert.Equal(t, "demo", p.Tool.Name)
	assert.NotNil(t, p.Logger)
	assert.NotNil(t, p.FS)
	assert.Equal(t, assets, p.Assets)
	assert.Equal(t, "v1.2.3", p.Version.GetVersion())
	assert.Equal(t, eh, p.ErrorHandler)
	assert.Equal(t, col, p.Collector)
	// Config is left nil by design (the pre-run assigns it).
	assert.Nil(t, p.Config)
}

func TestNew_AppliesDefaultsForOmittedOptionals(t *testing.T) {
	t.Parallel()

	p, err := New(Tool{Name: "demo"}, logger.NewNoop(), afero.NewMemMapFs())
	require.NoError(t, err)

	// Collector, ErrorHandler and Version are defaulted, never nil.
	assert.NotNil(t, p.Collector)
	assert.NotNil(t, p.ErrorHandler)
	require.NotNil(t, p.Version)
	assert.True(t, p.Version.IsDevelopment(), "the defaulted Version is development-flavoured")
}

func TestNew_WithConfigOption(t *testing.T) {
	t.Parallel()

	p := &Props{Tool: Tool{Name: "demo"}, Logger: logger.NewNoop(), FS: afero.NewMemMapFs()}
	require.Nil(t, p.Config)
	// Exercise the WithConfig option through applying it directly (a nil store
	// is a valid "not yet loaded" value).
	WithConfig(nil)(p)
	assert.Nil(t, p.Config)
}

func TestNew_ValidationErrors(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	_, err := New(Tool{}, logger.NewNoop(), fs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Tool.Name is required")

	_, err = New(Tool{Name: "demo"}, nil, fs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Logger is required")

	_, err = New(Tool{Name: "demo"}, logger.NewNoop(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FS is required")
}

func TestValidate_NilProps(t *testing.T) {
	t.Parallel()

	var p *Props
	err := p.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil Props")
}

func TestApplyDefaults_Idempotent(t *testing.T) {
	t.Parallel()

	p := &Props{Tool: Tool{Name: "demo"}, Logger: logger.NewNoop()}
	p.ApplyDefaults()

	col, eh, ver := p.Collector, p.ErrorHandler, p.Version
	require.NotNil(t, col)
	require.NotNil(t, eh)
	require.NotNil(t, ver)

	// A second call must not replace the already-set fields.
	p.ApplyDefaults()
	assert.Equal(t, col, p.Collector)
	assert.Equal(t, eh, p.ErrorHandler)
	assert.Equal(t, ver, p.Version)
}

// TestApplyDefaults_SkipsErrorHandlerWithoutLogger covers the branch where a nil
// Logger leaves ErrorHandler unset (it cannot be built without a logger).
func TestApplyDefaults_SkipsErrorHandlerWithoutLogger(t *testing.T) {
	t.Parallel()

	p := &Props{Tool: Tool{Name: "demo"}}
	p.ApplyDefaults()

	assert.NotNil(t, p.Collector)
	assert.NotNil(t, p.Version)
	assert.Nil(t, p.ErrorHandler, "ErrorHandler needs a logger; stays nil without one")
}
