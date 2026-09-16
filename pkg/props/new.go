package props

import (
	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errorhandling"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// Option configures a Props built by New.
type Option func(*Props)

// WithAssets sets the embedded assets bundle.
func WithAssets(a *Assets) Option { return func(p *Props) { p.Assets = a } }

// WithVersion sets the runtime/ldflags version info.
func WithVersion(v version.Info) Option { return func(p *Props) { p.Version = v } }

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

// WithFeatures names the snapshot the feature set is resolved from. When
// omitted, New snapshots the default registry, which is every feature this
// binary's imports declared.
func WithFeatures(s features.Snapshot) Option { return func(p *Props) { p.featureSnapshot = s } }

// WithResolver replaces the default Resolver (defaults, then the tool's states
// in order) for a consumer with another precedence.
func WithResolver(r features.Resolver) Option { return func(p *Props) { p.featureResolver = r } }

// WithSet supplies a resolved Set directly, bypassing resolution; a test or a
// consumer with its own Set implementation uses this.
func WithSet(s features.Set) Option { return func(p *Props) { p.Features = s } }

// WithFlags sets the request-time Evaluator. When omitted it is Features.
func WithFlags(e features.Evaluator) Option { return func(p *Props) { p.Flags = e } }

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

	if err := p.resolveFeatures(); err != nil {
		return nil, err
	}

	p.ApplyDefaults()

	if err := p.Validate(); err != nil {
		return nil, err
	}

	return p, nil
}

// resolveFeatures fills Features from the snapshot and resolver chosen (or
// their defaults) and Tool.Features, unless a Set was supplied. Enabling an
// undeclared feature is the one way this fails (spec 0199 OQ2).
func (p *Props) resolveFeatures() error {
	if p.Features != nil {
		return nil
	}

	snapshot := p.featureSnapshot
	if snapshot == nil {
		snapshot = features.Default().Snapshot()
	}

	resolver := p.featureResolver
	if resolver == nil {
		resolver = features.DefaultResolver()
	}

	set, err := resolver.Resolve(snapshot, StatesOf(p.Tool.Features))
	if err != nil {
		return err
	}

	p.Features = set

	return nil
}

// StatesOf converts the manifest's feature list to the core's states.
func StatesOf(fs []Feature) []features.State {
	out := make([]features.State, len(fs))
	for i, f := range fs {
		out[i] = features.State{ID: f.ID, Enabled: f.Enabled}
	}

	return out
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

	// A literal Props (tests, a downstream main that skipped New) resolves its
	// set here; the one failure, enabling an undeclared feature, cannot be
	// returned from this path, so the undeclared state is dropped and the
	// tool's remaining states apply. New reports it as an error.
	if p.Features == nil {
		if err := p.resolveFeatures(); err != nil {
			snapshot := features.Default().Snapshot()
			p.Features, _ = features.Resolve(snapshot, withoutUnknownEnables(snapshot, StatesOf(p.Tool.Features)))
		}
	}

	if p.Flags == nil {
		p.Flags = p.Features
	}

	if p.ErrorHandler == nil && p.Logger != nil {
		p.ErrorHandler = errorhandling.New(logger.ToSlog(p.Logger), p.Tool.Help)
	}
}

// withoutUnknownEnables drops the enable states naming a feature the snapshot
// lacks, the one thing Resolve refuses; unknown disables stay so the Set can
// list them as ignored (spec 0199 OQ2).
func withoutUnknownEnables(s features.Snapshot, states []features.State) []features.State {
	out := make([]features.State, 0, len(states))

	for _, st := range states {
		if _, ok := s.Lookup(st.ID); ok || !st.Enabled {
			out = append(out, st)
		}
	}

	return out
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
