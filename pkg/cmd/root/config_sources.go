package root

import (
	"context"
	"io"
	"sync/atomic"
	"time"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// bootstrapLayers are what a source's settings may be read from (spec 0204
// D5): never the project file, so a repository cannot choose where
// configuration comes from, and never another source.
var bootstrapLayers = []p.ConfigLayer{p.LayerDefaults, p.LayerFiles, p.LayerEnv, p.LayerFlags}

// addSourceLayers builds each declared source slot into byLayer (spec 0204
// D4). The bootstrap pass is a store from the layers of D5 alone; its view is
// what every factory reads its slot's settings from.
func addSourceLayers(ctx context.Context, opts ConfigLoadOptions, spec p.ConfigSpec, byLayer map[p.ConfigLayer][]config.StoreOption) (err error) {
	set := opts.Props.GetFeatures()
	kinds := setup.ConfigSourceKindsIn(set)
	overrides := setup.ConfigSourceOverridesIn(set)

	if err := refuseUndeclaredOverrides(spec, overrides); err != nil {
		return err
	}

	store, err := bootstrapStore(ctx, opts, spec.Layers)
	if err != nil {
		return err
	}

	bootstrap := sourceBootstrap{Store: store, fs: opts.Props.GetConfigFS(), codecs: setup.ConfigCodecsIn(set)}
	opts.Props.SourceStatuses = nil

	// Until the store exists to own them, what the factories built is closed
	// here if a later slot stops the build.
	var owned []io.Closer

	defer func() {
		if err != nil {
			closeAll(owned)
		}
	}()

	for i, layer := range spec.Layers {
		for _, src := range spec.Sources {
			if p.ConfigLayer(src.Name) != layer {
				continue
			}

			if opts.SourcesOptional {
				src.Required = new(false)
			}

			slot := &slotBootstrap{sourceBootstrap: bootstrap}

			opt, status, err := sourceLayer(ctx, opts.Props, src, sourcePlace{index: i + 1, of: len(spec.Layers)}, kinds, overrides, slot)
			if err != nil {
				return err
			}

			opts.Props.SourceStatuses = append(opts.Props.SourceStatuses, status)

			if opt != nil {
				owned = append(owned, slot.closers...)
				byLayer[layer] = append([]config.StoreOption{opt}, closerOptions(slot.closers)...)
			}
		}
	}

	return nil
}

func refuseUndeclaredOverrides(spec p.ConfigSpec, overrides map[string]setup.SourceFactory) error {
	for name := range overrides {
		declared := false

		for _, src := range spec.Sources {
			declared = declared || src.Name == name
		}

		if !declared {
			return errors.WithHint(errors.Wrapf(setup.ErrConfigSourceOverrideUndeclared, "%q", name),
				"remove the override, or declare the slot in properties.config.sources and regenerate")
		}
	}

	return nil
}

// bootstrapStore is the first pass: fresh backends for the D5 layers, in
// their declared order, so no backend is shared with the full store.
func bootstrapStore(ctx context.Context, opts ConfigLoadOptions, layers []p.ConfigLayer) (*config.Store, error) {
	byLayer, err := configLayerOpts(opts)
	if err != nil {
		return nil, err
	}

	storeOpts := []config.StoreOption{config.WithReaders(config.NamedSource{Name: "bootstrap"})}

	for _, layer := range layers {
		for _, trusted := range bootstrapLayers {
			if layer == trusted {
				storeOpts = append(storeOpts, byLayer[layer]...)
			}
		}
	}

	store, err := config.NewStore(ctx, storeOpts...)

	return store, errors.Wrap(err, "loading the configuration a config source is read from")
}

// sourceBootstrap is what a factory is given: the bootstrap view, the tool's
// config filesystem, and its linked codecs.
type sourceBootstrap struct {
	*config.Store
	fs     config.FS
	codecs []setup.ConfigCodec
}

func (b sourceBootstrap) FS() config.FS { return b.fs }

func (b sourceBootstrap) CodecFor(path string) (config.Codec, error) {
	return setup.ConfigCodecFor(b.codecs, path)
}

// slotBootstrap is one slot's bootstrap: it keeps the credential rung the
// slot's factory reports (spec 0204 D10), and what it hands the store to
// close.
type slotBootstrap struct {
	sourceBootstrap
	credential string
	closers    []io.Closer
}

func (b *slotBootstrap) ReportCredential(origin string) { b.credential = origin }

func (b *slotBootstrap) CloseWithStore(c io.Closer) { b.closers = append(b.closers, c) }

func closerOptions(closers []io.Closer) []config.StoreOption {
	opts := make([]config.StoreOption, 0, len(closers))
	for _, c := range closers {
		opts = append(opts, config.WithCloser(c))
	}

	return opts
}

// closeAll closes what no store will; the errors have nowhere useful to go
// beside the one that stopped the build.
func closeAll(closers []io.Closer) {
	for _, c := range closers {
		_ = c.Close()
	}
}

// sourcePlace is a slot's position in the stack, for messages.
type sourcePlace struct{ index, of int }

// sourceLayer builds one slot, or nothing when an optional slot is absent
// (spec 0204 D6), and records how it fared.
func sourceLayer(ctx context.Context, props *p.Props, src p.ConfigSource, at sourcePlace,
	kinds map[string]setup.ConfigSourceKind, overrides map[string]setup.SourceFactory, slot *slotBootstrap,
) (config.StoreOption, p.ConfigSourceStatus, error) {
	status := p.ConfigSourceStatus{Slot: src}

	factory, overridden, err := sourceFactory(src, kinds, overrides)
	if err != nil {
		return nil, status, err
	}

	var settings config.Reader
	if sub := slot.View().Sub("config.sources." + src.Name); sub != nil {
		settings = sub
	}

	if settings == nil && !overridden {
		if src.IsRequired() {
			return nil, status, setup.ConfigSourceUnconfiguredError(props, src)
		}

		props.Logger.Warn("optional config source is not configured; continuing without it",
			"source", src.Name, "kind", src.Kind, "hint", "run `"+props.Tool.Name+" init config "+src.Name+"`")

		status.State = p.ConfigSourceUnconfigured

		return nil, status, nil
	}

	backend, err := factory(ctx, settings, slot)
	status.Credential = slot.credential

	if err != nil {
		closeAll(slot.closers)
		slot.closers = nil

		if src.IsRequired() {
			return nil, status, errors.WithHint(
				errors.Wrapf(setup.ErrConfigSourceUnavailable, "%q (%s, layer %d of %d): %v", src.Name, src.Kind, at.index, at.of, err),
				"mark the source required: false if the tool may run without it")
		}

		props.Logger.Warn("optional config source is unavailable; continuing without it",
			"source", src.Name, "kind", src.Kind, "layer", at.index, "error", err)

		status.State, status.Err = p.ConfigSourceUnavailable, err.Error()

		return nil, status, nil
	}

	status.State = p.ConfigSourceBuilt
	status.Writable = src.IsWritable(kinds[src.Kind].WritableByDefault)
	status.Sensitive = backend.Capabilities().Sensitive

	load := &sourceLoad{src: src, at: at, log: props.Logger, leftOut: leftOutRecorder(props, len(props.SourceStatuses))}

	return config.WithBackend(wrapSource(backend, status.Writable, load)), status, nil
}

// sourceFactory picks the slot's factory: its override, else its kind's.
func sourceFactory(src p.ConfigSource, kinds map[string]setup.ConfigSourceKind, overrides map[string]setup.SourceFactory) (setup.SourceFactory, bool, error) {
	if f, ok := overrides[src.Name]; ok {
		return f, true, nil
	}

	if setup.IsOverrideOnlyKind(src.Kind) {
		return nil, false, errors.WithHintf(errors.Wrapf(setup.ErrConfigSourceNeedsOverride, "%q (%s)", src.Name, src.Kind),
			"a %s source cannot be built from configuration; register one with setup.OverrideConfigSource(%q, ...) in the tool's main package",
			src.Kind, src.Name)
	}

	kind, ok := kinds[src.Kind]
	if !ok || kind.Factory == nil {
		return nil, false, errors.WithHintf(errors.Wrapf(setup.ErrConfigSourceKindNotLinked, "%q (%s)", src.Name, src.Kind),
			"blank-import gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/%s, or regenerate", src.Kind)
	}

	return kind.Factory, false, nil
}

// wrapSource applies D7 and D11 to a source's backend: it is a write target
// only when writable, and an optional one's watch failure drops it out of
// watching alone. Like config.Filtered, the result implements exactly the
// optional interfaces the inner backend does, bar the ones taken away.
func wrapSource(b config.Backend, writable bool, load *sourceLoad) config.Backend {
	writer, isWritable := b.(config.WritableBackend)
	watcher, isWatchable := b.(config.WatchableBackend)

	if !writable || !isWritable {
		writer = nil
	}

	optional := !load.src.IsRequired()

	return assembleSource(sourceBackend{inner: b, readOnly: !writable, load: load},
		writer, sourceWatch(watcher, isWatchable, optional, load.log, load.src.Name))
}

// sourceLoad applies a slot's policy where an unreachable source is found:
// a client-building factory such as Consul's or Vault's contacts nothing, so
// the first failure is the store's first load (spec 0204 D6).
type sourceLoad struct {
	src     p.ConfigSource
	at      sourcePlace
	log     logger.Logger
	leftOut func(error)
	loaded  atomic.Bool
}

// failed is a source's answer to a load that failed. A required source stops
// the build, named. An optional one is left out on the first load; on a later
// reload the error stands, so the store keeps its last snapshot rather than
// dropping the source's values over a blip.
func (l *sourceLoad) failed(err error) ([]config.Layer, error) {
	if l.src.IsRequired() {
		return nil, errors.WithHint(
			errors.Wrapf(setup.ErrConfigSourceUnavailable, "%q (%s, layer %d of %d): %v", l.src.Name, l.src.Kind, l.at.index, l.at.of, err),
			"mark the source required: false if the tool may run without it")
	}

	if l.loaded.Load() {
		return nil, err
	}

	l.log.Warn("optional config source is unavailable; continuing without it",
		"source", l.src.Name, "kind", l.src.Kind, "layer", l.at.index, "error", err)
	l.leftOut(err)

	return nil, nil
}

// leftOutRecorder marks the slot recorded at index as unavailable.
func leftOutRecorder(props *p.Props, index int) func(error) {
	return func(err error) {
		props.SourceStatuses[index].State = p.ConfigSourceUnavailable
		props.SourceStatuses[index].Err = err.Error()
	}
}

// sourceWatch is the watch a wrapped source offers: none for an unwatchable
// backend, an isolated one for an optional source, the backend's own
// otherwise.
func sourceWatch(w config.WatchableBackend, watchable, optional bool, log logger.Logger, name string) watchFunc {
	switch {
	case !watchable:
		return nil
	case optional:
		return isolatedWatch(w, log, name)
	default:
		return w.Watch
	}
}

// assembleSource picks the wrapper type implementing exactly the halves it
// is given.
func assembleSource(base sourceBackend, writer config.WritableBackend, watch watchFunc) config.Backend {
	switch {
	case writer != nil && watch != nil:
		return sourceWritableWatchable{sourceWritable: sourceWritable{sourceBackend: base, write: writer}, watch: watch}
	case writer != nil:
		return sourceWritable{sourceBackend: base, write: writer}
	case watch != nil:
		return sourceWatchable{sourceBackend: base, watch: watch}
	default:
		return base
	}
}

type watchFunc func(ctx context.Context, interval time.Duration, onChange func()) (func(), error)

// isolatedWatch turns an optional source's watch failure into a warning and
// a no-op stop, so it cannot make the store's whole watch fail (spec 0204
// D11: Store.Watch is all-or-nothing).
func isolatedWatch(w config.WatchableBackend, log logger.Logger, name string) watchFunc {
	return func(ctx context.Context, interval time.Duration, onChange func()) (func(), error) {
		stop, err := w.Watch(ctx, interval, onChange)
		if err != nil {
			log.Warn("optional config source cannot be watched; its changes need a restart", "source", name, "error", err)

			return func() {}, nil
		}

		return stop, nil
	}
}

// sourceBackend forwards what config.Filtered forwards: ID, so writes route
// back; Capabilities, so the sensitive-leak guard stays armed; the kind and
// the poll hint. A read-only one also clears each layer's Writable, which is
// what routing walks, as go/config's own read-only nested store does.
type sourceBackend struct {
	inner    config.Backend
	readOnly bool
	load     *sourceLoad
}

func (s sourceBackend) ID() string { return s.inner.ID() }

func (s sourceBackend) Load(ctx context.Context, below []config.Layer) ([]config.Layer, error) {
	layers, err := s.inner.Load(ctx, below)
	if err != nil {
		return s.load.failed(err)
	}

	s.load.loaded.Store(true)

	if s.readOnly {
		for i := range layers {
			layers[i].Source.Writable = false
		}
	}

	return layers, nil
}

func (s sourceBackend) Capabilities() config.Capabilities { return s.inner.Capabilities() }

func (s sourceBackend) SourceKind() config.SourceKind {
	if d, ok := s.inner.(config.SourceKindDeclarer); ok {
		return d.SourceKind()
	}

	return config.SourceFile
}

func (s sourceBackend) PollInterval() time.Duration {
	if h, ok := s.inner.(config.PollIntervalHinter); ok {
		return h.PollInterval()
	}

	return 0
}

type sourceWritable struct {
	sourceBackend
	write config.WritableBackend
}

func (s sourceWritable) Prepare(ctx context.Context, edits []config.Edit) (config.Pending, error) {
	return s.write.Prepare(ctx, edits)
}

type sourceWatchable struct {
	sourceBackend
	watch watchFunc
}

func (s sourceWatchable) Watch(ctx context.Context, interval time.Duration, onChange func()) (func(), error) {
	return s.watch(ctx, interval, onChange)
}

type sourceWritableWatchable struct {
	sourceWritable
	watch watchFunc
}

func (s sourceWritableWatchable) Watch(ctx context.Context, interval time.Duration, onChange func()) (func(), error) {
	return s.watch(ctx, interval, onChange)
}
