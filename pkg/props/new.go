package props

import (
	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errorhandling"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// Option configures a Props built by New.
type Option func(*Props)

// WithAssets sets the embedded assets bundle.
func WithAssets(a Assets) Option { return func(p *Props) { p.Assets = a } }

// WithVersion sets the runtime/ldflags version info.
func WithVersion(v version.Version) Option { return func(p *Props) { p.Version = v } }

// WithErrorHandler sets the structured error handler. When omitted, New
// defaults one built from the logger and Tool.Help.
func WithErrorHandler(eh errorhandling.ErrorHandler) Option {
	return func(p *Props) { p.ErrorHandler = eh }
}

// WithConfig sets the live configuration store. It is normally left unset at
// construction — the root pre-run loads and assigns it — so New does not
// require it (see Validate's init-path exemption).
func WithConfig(store *config.Store) Option { return func(p *Props) { p.Config = store } }

// WithCollector sets the telemetry collector. When omitted, New defaults a
// NoopCollector so callers can invoke Props.Collector unconditionally.
func WithCollector(c TelemetryCollector) Option { return func(p *Props) { p.Collector = c } }

// New constructs a Props from the required dependencies plus options for the
// rest, applying the framework defaults and validating the nil-field contract.
// It is the blessed construction path: the required fields (a named Tool, a
// Logger, and a filesystem) must be present, while the optional fields
// (Collector, ErrorHandler, Version) are defaulted when omitted. Config is left
// nil unless supplied — the pre-run assigns it, and the init path runs with it
// nil by design (see Validate).
//
// Most of the codebase still builds Props as a struct literal; New exists so a
// downstream tool (or a test) can get the nil-field contract checked at one
// place instead of discovering a nil Logger as a panic deep in a command.
func New(tool Tool, log logger.Logger, fs afero.Fs, opts ...Option) (*Props, error) {
	p := &Props{
		Tool:   tool,
		Logger: log,
		FS:     fs,
	}

	for _, opt := range opts {
		opt(p)
	}

	p.ApplyDefaults()

	if err := p.Validate(); err != nil {
		return nil, err
	}

	return p, nil
}

// applyDefaults fills in the optional fields that the framework guarantees are
// non-nil: Collector (a NoopCollector), ErrorHandler (built from the logger and
// Tool.Help), and Version (an empty, development-flavoured Info). It never
// touches Config — that is the caller's / pre-run's responsibility. Idempotent
// and nil-safe on the optional fields, so it is safe to call from both New and
// the root bootstrap.
func (p *Props) ApplyDefaults() {
	if p.Collector == nil {
		p.Collector = NoopCollector{}
	}

	if p.ErrorHandler == nil && p.Logger != nil {
		p.ErrorHandler = errorhandling.New(logger.ToSlog(p.Logger), p.Tool.Help)
	}

	if p.Version == nil {
		p.Version = version.NewInfo("", "", "")
	}
}

// Validate reports whether the required Props fields are present. It is the
// enforcement half of the nil-field contract that used to live only in doc
// comments.
//
// Required: a named Tool, a Logger, and a filesystem — every command path reads
// these. NOT required: Config. It is deliberately nil on the init path (init is
// what CREATES the configuration) and until the root pre-run loads it, so a
// blanket "Config must be non-nil" check would be wrong; code that touches
// Config on those paths must guard it (Store.View is not nil-receiver-safe —
// use ViewOrNil). The optional fields are defaulted by applyDefaults, so
// Validate does not police them.
func (p *Props) Validate() error {
	if p == nil {
		return errors.New("props: nil Props")
	}

	if p.Tool.Name == "" {
		return errors.New("props: Tool.Name is required")
	}

	if p.Logger == nil {
		return errors.New("props: Logger is required")
	}

	if p.FS == nil {
		return errors.New("props: FS is required")
	}

	return nil
}
