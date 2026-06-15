package propstest_test

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props/propstest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

func TestNew_AllFieldsNonNil(t *testing.T) {
	t.Parallel()

	p := propstest.New()
	require.NotNil(t, p)

	assert.NotNil(t, p.Logger, "Logger must be non-nil")
	assert.NotNil(t, p.Config, "Config must be non-nil")
	assert.NotNil(t, p.Assets, "Assets must be non-nil")
	assert.NotNil(t, p.FS, "FS must be non-nil")
	assert.NotNil(t, p.Version, "Version must be non-nil")
	assert.NotNil(t, p.ErrorHandler, "ErrorHandler must be non-nil")
	assert.NotNil(t, p.Collector, "Collector must be non-nil")

	// Tool defaults are benign-but-valid.
	assert.NotEmpty(t, p.Tool.Name)
	assert.NotEmpty(t, p.Tool.EnvPrefix)
	assert.NotEmpty(t, p.Tool.ReleaseSource.Type)
}

func TestNew_CollectorIsNoop(t *testing.T) {
	t.Parallel()

	p := propstest.New()

	// The default Collector is the disabled NoopCollector and upholds the
	// non-nil-Collector invariant without emitting telemetry.
	assert.IsType(t, props.NoopCollector{}, p.Collector)
	assert.False(t, p.Collector.Enabled())

	// Every method is safe to call.
	p.Collector.Track(props.EventFeatureUsed, "x", nil)
	p.Collector.TrackCommand("x", 0, 0, nil)
	p.Collector.TrackCommandExtended("x", nil, 0, 0, "", nil)
	require.NoError(t, p.Collector.Flush(context.Background()))
	require.NoError(t, p.Collector.Close(context.Background()))
	require.NoError(t, p.Collector.Drop())
	assert.Equal(t, "noop (disabled)", p.Collector.BackendInfo())
}

func TestNew_FSIsInMemoryAndWritable(t *testing.T) {
	t.Parallel()

	p := propstest.New()

	// The default FS is an in-memory afero filesystem and is writable.
	require.NoError(t, afero.WriteFile(p.FS, "/tmp/file.txt", []byte("hello"), 0o600))

	data, err := afero.ReadFile(p.FS, "/tmp/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))

	// It is a MemMapFs, not the OS filesystem.
	_, isMem := p.FS.(*afero.MemMapFs)
	assert.True(t, isMem, "default FS should be an in-memory MemMapFs")
}

func TestNew_ConfigGettersAreSafe(t *testing.T) {
	t.Parallel()

	p := propstest.New()

	// Get* accessors on the default empty container are safe and return zero values.
	assert.Nil(t, p.Config.Get("missing"))
	assert.Empty(t, p.Config.GetString("missing"))
	assert.Equal(t, 0, p.Config.GetInt("missing"))
	assert.InDelta(t, 0.0, p.Config.GetFloat("missing"), 0)
	assert.False(t, p.Config.GetBool("missing"))
	assert.False(t, p.Config.Has("missing"))

	// And it is round-trip usable: Set then Get.
	p.Config.Set("key", "value")
	assert.Equal(t, "value", p.Config.GetString("key"))
}

func TestNew_VersionIsDeterministic(t *testing.T) {
	t.Parallel()

	p := propstest.New()

	assert.NotEmpty(t, p.Version.GetVersion())
	assert.NotEmpty(t, p.Version.String())
}

func TestNew_ErrorHandlerDoesNotExitOnFatal(t *testing.T) {
	t.Parallel()

	p := propstest.New()

	// The default error handler's Exit and Writer are inert: a Fatal call
	// neither terminates the test process nor panics.
	assert.NotPanics(t, func() {
		p.ErrorHandler.Fatal(assert.AnError, "test")
	})
}

func TestNew_IndependentInstances(t *testing.T) {
	t.Parallel()

	p1 := propstest.New()
	p2 := propstest.New()

	// Distinct Props pointers.
	assert.NotSame(t, p1, p2)

	// Writing to one filesystem does not affect the other.
	require.NoError(t, afero.WriteFile(p1.FS, "/only-in-p1.txt", []byte("x"), 0o600))

	exists, err := afero.Exists(p2.FS, "/only-in-p1.txt")
	require.NoError(t, err)
	assert.False(t, exists, "instances must not share filesystem state")
}

func TestNew_OptionsOverride(t *testing.T) {
	t.Parallel()

	customFS := afero.NewMemMapFs()
	customLogger := logger.NewNoop()
	customCollector := props.NoopCollector{}
	customVersion := version.NewInfo("v9.9.9", "deadbeef", "2020-01-01")
	customAssets := props.NewAssets()
	customConfig := config.NewReaderContainer(afero.NewMemMapFs())
	customTool := props.Tool{Name: "overridden", EnvPrefix: "OV"}

	p := propstest.New(
		propstest.WithFS(customFS),
		propstest.WithLogger(customLogger),
		propstest.WithCollector(customCollector),
		propstest.WithVersion(customVersion),
		propstest.WithAssets(customAssets),
		propstest.WithConfig(customConfig),
		propstest.WithTool(customTool),
	)

	assert.Same(t, customFS, p.FS)
	assert.Same(t, customConfig, p.Config)
	assert.Equal(t, "overridden", p.Tool.Name)
	assert.Equal(t, "OV", p.Tool.EnvPrefix)
	assert.Equal(t, "v9.9.9", p.Version.GetVersion())
}

func TestNew_WithErrorHandlerOverride(t *testing.T) {
	t.Parallel()

	called := false
	p := propstest.New(propstest.WithErrorHandler(&recordingHandler{onError: func() { called = true }}))

	p.ErrorHandler.Error(assert.AnError)
	assert.True(t, called, "custom error handler should be used")
}

// recordingHandler is a minimal ErrorHandler used to verify WithErrorHandler.
type recordingHandler struct {
	onError func()
}

func (h *recordingHandler) Check(error, string, string, ...*cobra.Command) {}
func (h *recordingHandler) Fatal(error, ...string)                         {}
func (h *recordingHandler) Error(error, ...string)                         { h.onError() }
func (h *recordingHandler) Warn(error, ...string)                          {}
func (h *recordingHandler) SetUsage(func() error)                          {}
