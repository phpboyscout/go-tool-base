package root

import (
	"context"
	"slices"
	"sync"

	"gitlab.com/phpboyscout/go/config"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// envLayer is the env layer's store option. With denials it filters the
// environment and returns the recorder that reports what the filter dropped
// (spec 0214 D3, D6); without, it is the plain env backend and no recorder.
func envLayer(prefix string, denials []string) (config.StoreOption, *envRecorder) {
	if len(denials) == 0 {
		return config.WithEnv(prefix), nil
	}

	rec := &envRecorder{inner: config.NewEnvBackend(prefix)}

	return config.WithBackend(config.Filtered(rec, config.Deny(denials...))), rec
}

// envRecorder sits under the filter and records the variable behind every
// layer the env backend loads. Each variable is one layer, so a name missing
// from the store's layers is one the filter dropped.
type envRecorder struct {
	inner config.Backend

	mu    sync.Mutex
	names []string
}

func (r *envRecorder) ID() string { return r.inner.ID() }

func (r *envRecorder) Capabilities() config.Capabilities { return r.inner.Capabilities() }

func (r *envRecorder) BindKeyCase(caseSensitive bool) error {
	if b, ok := r.inner.(config.KeyCaseBinder); ok {
		return b.BindKeyCase(caseSensitive)
	}

	return nil
}

func (r *envRecorder) Load(ctx context.Context, below []config.Layer) ([]config.Layer, error) {
	layers, err := r.inner.Load(ctx, below)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(layers))
	for _, layer := range layers {
		names = append(names, layer.Source.Name)
	}

	r.mu.Lock()
	r.names = names
	r.mu.Unlock()

	return layers, nil
}

// withheld is the sorted variables the env backend loaded that snap does not
// hold. Nil on a nil recorder, which is a tool with nothing file-only.
func (r *envRecorder) withheld(snap *config.Snapshot) []string {
	if r == nil {
		return nil
	}

	kept := map[string]bool{}

	for _, k := range setup.EnvKeys(snap) {
		kept[k.Variable] = true
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var out []string

	for _, name := range r.names {
		if !kept[name] {
			out = append(out, name)
		}
	}

	slices.Sort(out)

	return out
}

// reportWithheldEnv records the withheld variables for doctor and warns once,
// naming them and never their values (spec 0214 D8).
func reportWithheldEnv(props *p.Props, withheld []string) {
	props.WithheldEnv = withheld

	if len(withheld) > 0 {
		props.Logger.Warn("ignoring environment variables aimed at file-only config keys; set these keys in a config file instead",
			"variables", withheld)
	}
}
