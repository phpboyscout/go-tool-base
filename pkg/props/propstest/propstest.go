// Package propstest provides a public test-fixture helper for constructing a
// fully-wired *props.Props with hermetic, safe defaults.
//
// Tools built on go-tool-base — and GTB's own tests — frequently need a Props
// instance to drive commands and bootstrap-style code under test. Hand-assembling
// the logger, in-memory filesystem, telemetry collector, error handler, tool
// metadata and config container in every test is repetitive and easy to get
// wrong (for example, leaving Collector nil and violating the documented
// non-nil-Collector invariant).
//
// propstest.New returns a Props with every field wired to a hermetic default:
// a noop logger, an in-memory afero filesystem, a NoopCollector, an error
// handler whose Exit and Writer are inert, benign Tool metadata, a deterministic
// Version, empty-but-valid Assets, and a usable empty Config container. Each
// field may be overridden with a functional Option.
//
// The helper performs no real filesystem, network, keychain or os.Exit side
// effects, and every call returns a fresh, independent instance, so it is safe
// to use from t.Parallel() tests.
//
//	func TestSomething(t *testing.T) {
//		t.Parallel()
//		p := propstest.New(propstest.WithTool(props.Tool{Name: "mytool"}))
//		// ... drive code that needs a *props.Props ...
//	}
package propstest

import (
	"io"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/errorhandling"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// options accumulates the configurable defaults applied by New. It is internal:
// callers mutate it exclusively through the exported Option helpers.
type options struct {
	tool       *props.Tool
	logger     logger.Logger
	fs         afero.Fs
	collector  props.TelemetryCollector
	version    version.Version
	assets     props.Assets
	config     config.Containable
	errHandler errorhandling.ErrorHandler
}

// Option customises a single field of the Props produced by New.
type Option func(*options)

// WithTool overrides the default Tool metadata.
func WithTool(t props.Tool) Option {
	return func(o *options) { o.tool = &t }
}

// WithLogger overrides the default noop logger.
func WithLogger(l logger.Logger) Option {
	return func(o *options) { o.logger = l }
}

// WithFS overrides the default in-memory filesystem.
func WithFS(fs afero.Fs) Option {
	return func(o *options) { o.fs = fs }
}

// WithCollector overrides the default NoopCollector.
func WithCollector(c props.TelemetryCollector) Option {
	return func(o *options) { o.collector = c }
}

// WithVersion overrides the default deterministic test version.
func WithVersion(v version.Version) Option {
	return func(o *options) { o.version = v }
}

// WithAssets overrides the default empty Assets container.
func WithAssets(a props.Assets) Option {
	return func(o *options) { o.assets = a }
}

// WithConfig overrides the default empty config container.
func WithConfig(c config.Containable) Option {
	return func(o *options) { o.config = c }
}

// WithErrorHandler overrides the default inert error handler.
func WithErrorHandler(h errorhandling.ErrorHandler) Option {
	return func(o *options) { o.errHandler = h }
}

// defaultTool returns benign, valid Tool metadata so bootstrap-style code under
// test does not choke on empty identity or release-source fields.
func defaultTool() props.Tool {
	return props.Tool{
		Name:      "testtool",
		Summary:   "Test tool fixture",
		EnvPrefix: "TESTTOOL",
		ReleaseSource: props.ReleaseSource{
			Type:  "github",
			Owner: "example",
			Repo:  "testtool",
		},
	}
}

// New constructs a fully-wired *props.Props with hermetic defaults, applying any
// supplied options. Every field is non-nil and safe to use, and each call
// returns an independent instance suitable for t.Parallel() tests.
func New(opts ...Option) *props.Props {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	if o.logger == nil {
		o.logger = logger.NewNoop()
	}

	if o.fs == nil {
		o.fs = afero.NewMemMapFs()
	}

	if o.collector == nil {
		o.collector = props.NoopCollector{}
	}

	if o.version == nil {
		o.version = version.NewInfo("v0.0.0-test", "test", "1970-01-01T00:00:00Z")
	}

	if o.assets == nil {
		o.assets = props.NewAssets()
	}

	if o.config == nil {
		// NewReaderContainer with no readers yields an empty-but-usable
		// container: every Get* accessor is safe and returns the zero value.
		o.config = config.NewReaderContainer(o.fs)
	}

	if o.errHandler == nil {
		// A real handler wired to the noop logger, with Exit and Writer made
		// inert so a Fatal under test neither terminates the process nor
		// pollutes test output.
		o.errHandler = errorhandling.New(
			logger.ToSlog(o.logger),
			nil, // nil HelpConfig is safe — the handler nil-guards it.
			errorhandling.WithExitFunc(func(int) {}),
			errorhandling.WithWriter(io.Discard),
		)
	}

	tool := defaultTool()
	if o.tool != nil {
		tool = *o.tool
	}

	return &props.Props{
		Tool:         tool,
		Logger:       o.logger,
		Config:       o.config,
		Assets:       o.assets,
		FS:           o.fs,
		Version:      o.version,
		ErrorHandler: o.errHandler,
		Collector:    o.collector,
	}
}
