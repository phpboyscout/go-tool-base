package root

import (
	"context"
	"os"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errorhandling"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// recordingWatcher is a config.Watcher whose returned stop func records that it
// was invoked. It stands in for a real fsnotify/poll watcher so a test can
// observe whether the retained stop was actually called on shutdown, rather
// than only whether the goroutine happened to unwind via context cancellation.
type recordingWatcher struct {
	started atomic.Bool
	stopped atomic.Bool
}

func (w *recordingWatcher) Watch(_ context.Context, _ []string, _ func()) (func(), error) {
	w.started.Store(true)

	return func() { w.stopped.Store(true) }, nil
}

// TestExecute_InvokesRetainedWatcherStopOnBackgroundContext proves F10: the
// config watcher's stop handle must be retained and invoked on shutdown. It
// reproduces the embedder scenario — the tree driven with a background context
// that never cancels — where the only thing that can tear the watcher down is
// the retained stop. Before the fix the stop was discarded, so the watcher was
// never stopped by teardown (the recordingWatcher's stop stays false).
func TestExecute_InvokesRetainedWatcherStopOnBackgroundContext(t *testing.T) {
	// A file-backed store is watchable; the injected watcher stands in for the
	// real one so the returned stop is observable.
	cfg := testutil.FileStoreFromYAML(t, "log:\n  level: info\n")

	watcher := &recordingWatcher{}
	state := newRootState()
	state.watchOpts = []config.WatchOption{config.WithWatcher(watcher)}

	props := &p.Props{
		Logger:       logger.NewNoop(),
		Config:       cfg,
		Collector:    p.NoopCollector{},
		ErrorHandler: errorhandling.New(logger.ToSlog(logger.NewNoop()), nil),
	}

	// A stub root command that wires the watcher in its RunE the way the real
	// PersistentPreRunE does, then returns cleanly — the embedder path.
	cmd := &cobra.Command{
		Use: "stub",
		RunE: func(c *cobra.Command, _ []string) error {
			startConfigWatch(props, cfg, c, state)

			return nil
		},
	}
	cmd.SetArgs([]string{})

	// An injected (empty) signal channel keeps execute off the process-global
	// signal handler; no signal is ever sent, so this is the no-signal path.
	execute(setup.Wrap("", cmd), props, executeOptions{signals: make(chan os.Signal, signalBuffer)})

	require.True(t, watcher.started.Load(), "the watcher must have started")
	require.NotNil(t, state.watchStop, "the stop handle must be retained on rootState")
	assert.True(t, watcher.stopped.Load(),
		"execute must invoke the retained watcher stop on shutdown so a "+
			"background-context embedder does not leak the watcher goroutines")
}
