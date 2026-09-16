package flags_test

import (
	"context"
	"sync/atomic"

	"gitlab.com/phpboyscout/go/features"
)

type recordingBackend struct {
	inited, closed atomic.Bool
}

func (r *recordingBackend) Resolve(_ context.Context, _ features.ID, fallback bool, _ features.EvalContext) (features.Decision, error) {
	return features.Decision{Enabled: fallback, Reason: features.ReasonDefault}, nil
}
func (r *recordingBackend) Init(context.Context) error                   { r.inited.Store(true); return nil }
func (r *recordingBackend) Ready() bool                                  { return r.inited.Load() }
func (r *recordingBackend) Watch(context.Context) <-chan features.Change { return nil }
func (r *recordingBackend) Close() error                                 { r.closed.Store(true); return nil }
