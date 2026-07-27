package setup

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// recordingCollector captures TrackCommand calls for assertions. It embeds
// props.NoopCollector so the other TelemetryCollector methods are satisfied as
// no-ops; only TrackCommand is overridden.
type recordingCollector struct {
	props.NoopCollector
	calls    int
	name     string
	duration int64
	exitCode int
}

func (r *recordingCollector) TrackCommand(name string, durationMs int64, exitCode int, _ map[string]string) {
	r.calls++
	r.name = name
	r.duration = durationMs
	r.exitCode = exitCode
}

func TestWithTiming(t *testing.T) {
	t.Parallel()

	log := logger.NewBuffer()

	mw := WithTiming(log)

	t.Run("Success", func(t *testing.T) {
		log.Reset()
		handler := mw(func(cmd *cobra.Command, args []string) error {
			time.Sleep(10 * time.Millisecond)
			return nil
		})

		err := handler(&cobra.Command{Use: "test-cmd"}, nil)
		require.NoError(t, err)

		entries := log.Entries()
		require.Len(t, entries, 1)
		assert.Equal(t, logger.DebugLevel, entries[0].Level)
		assert.Equal(t, "command completed", entries[0].Message)
		assert.Contains(t, entries[0].Keyvals, "command")
		assert.Contains(t, entries[0].Keyvals, "test-cmd")
		assert.Contains(t, entries[0].Keyvals, "duration")
		// error key should not be present on success
		assert.NotContains(t, entries[0].Keyvals, "error")
	})

	t.Run("Error", func(t *testing.T) {
		log.Reset()
		expectedErr := fmt.Errorf("handler failed")
		handler := mw(func(cmd *cobra.Command, args []string) error {
			return expectedErr
		})

		err := handler(&cobra.Command{Use: "test-cmd"}, nil)
		require.ErrorIs(t, err, expectedErr)

		entries := log.Entries()
		require.Len(t, entries, 1)
		assert.Equal(t, "command completed", entries[0].Message)
		assert.Contains(t, entries[0].Keyvals, "error")
		assert.Contains(t, entries[0].Keyvals, "handler failed")
	})
}

func TestWithRecovery(t *testing.T) {
	t.Parallel()

	log := logger.NewBuffer()

	mw := WithRecovery(log)

	t.Run("NoPanic", func(t *testing.T) {
		log.Reset()
		handler := mw(func(cmd *cobra.Command, args []string) error {
			return nil
		})

		err := handler(&cobra.Command{}, nil)
		require.NoError(t, err)
		assert.Equal(t, 0, log.Len())
	})

	t.Run("Panic", func(t *testing.T) {
		log.Reset()
		handler := mw(func(cmd *cobra.Command, args []string) error {
			panic("something went terribly wrong")
		})

		err := handler(&cobra.Command{Use: "test-cmd"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "panic in command \"test-cmd\": something went terribly wrong")

		entries := log.Entries()
		require.Len(t, entries, 2)

		// First entry: error-level summary (no stack trace)
		assert.Equal(t, logger.ErrorLevel, entries[0].Level)
		assert.Equal(t, "panic recovered in command", entries[0].Message)
		assert.Contains(t, entries[0].Keyvals, "command")
		assert.Contains(t, entries[0].Keyvals, "test-cmd")
		assert.Contains(t, entries[0].Keyvals, "panic")
		assert.Contains(t, entries[0].Keyvals, "something went terribly wrong")
		assert.NotContains(t, entries[0].Keyvals, "stack")

		// Second entry: debug-level stack trace
		assert.Equal(t, logger.DebugLevel, entries[1].Level)
		assert.Equal(t, "panic stack trace", entries[1].Message)
		assert.Contains(t, entries[1].Keyvals, "command")
		assert.Contains(t, entries[1].Keyvals, "test-cmd")
		assert.Contains(t, entries[1].Keyvals, "stack")
	})
}

func TestWithAuthCheck(t *testing.T) {
	t.Parallel()

	propsWith := func(t *testing.T, yaml string) *props.Props {
		t.Helper()

		return &props.Props{Config: testutil.StoreFromYAML(t, yaml)}
	}

	t.Run("AllKeysPresent", func(t *testing.T) {
		t.Parallel()

		p := propsWith(t, "test:\n  key1: value1\n  key2: value2\n")

		mw := WithAuthCheck(p, "test.key1", "test.key2")
		handler := mw(func(cmd *cobra.Command, args []string) error {
			return nil
		})

		err := handler(&cobra.Command{}, nil)
		assert.NoError(t, err)
	})

	t.Run("MissingKey", func(t *testing.T) {
		t.Parallel()

		p := propsWith(t, "test:\n  key1: value1\n")

		mw := WithAuthCheck(p, "test.key1", "test.missing")
		handler := mw(func(cmd *cobra.Command, args []string) error {
			return nil
		})

		err := handler(&cobra.Command{}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "required configuration \"test.missing\" is not set")
	})

	t.Run("EmptyKey", func(t *testing.T) {
		t.Parallel()

		p := propsWith(t, "test:\n  key1: \"\"\n")

		mw := WithAuthCheck(p, "test.key1")
		handler := mw(func(cmd *cobra.Command, args []string) error {
			return nil
		})

		err := handler(&cobra.Command{}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "required configuration \"test.key1\" is not set")
	})

	t.Run("NoKeysIsNoOp", func(t *testing.T) {
		t.Parallel()

		mw := WithAuthCheck(nil)
		handler := mw(func(cmd *cobra.Command, args []string) error {
			return nil
		})

		assert.NoError(t, handler(&cobra.Command{}, nil))
	})

	t.Run("NoConfigFailsCheck", func(t *testing.T) {
		t.Parallel()

		mw := WithAuthCheck(&props.Props{}, "test.key1")
		handler := mw(func(cmd *cobra.Command, args []string) error {
			return nil
		})

		require.Error(t, handler(&cobra.Command{}, nil))
	})
}
func TestWithTelemetry(t *testing.T) {
	t.Run("RecordsSuccessfulCommand", func(t *testing.T) {
		rc := &recordingCollector{}
		p := &props.Props{Collector: rc}

		handler := WithTelemetry(p)(func(_ *cobra.Command, _ []string) error { return nil })

		err := handler(&cobra.Command{Use: "deploy"}, nil)

		require.NoError(t, err)
		assert.Equal(t, 1, rc.calls)
		assert.Equal(t, "deploy", rc.name)
		assert.Zero(t, rc.exitCode)
		assert.GreaterOrEqual(t, rc.duration, int64(0))
	})

	t.Run("RecordsExitCodeOneOnError", func(t *testing.T) {
		rc := &recordingCollector{}
		p := &props.Props{Collector: rc}

		wantErr := fmt.Errorf("boom")
		handler := WithTelemetry(p)(func(_ *cobra.Command, _ []string) error { return wantErr })

		err := handler(&cobra.Command{Use: "build"}, nil)

		require.ErrorIs(t, err, wantErr)
		assert.Equal(t, 1, rc.calls)
		assert.Equal(t, "build", rc.name)
		assert.Equal(t, 1, rc.exitCode)
	})

	// The process-global registry binds the FIRST root's Props into the
	// middleware closure. When a second root stamps its own Props onto the
	// command context, telemetry must report through THAT root's collector, not
	// the first root's — otherwise two roots in one process cross-contaminate.
	t.Run("ResolvesCollectorFromContext", func(t *testing.T) {
		rootA := &recordingCollector{}
		rootB := &recordingCollector{}

		// Middleware was registered by root A (captures rootA's Props).
		handler := WithTelemetry(&props.Props{Collector: rootA})(
			func(_ *cobra.Command, _ []string) error { return nil })

		// A command dispatched under root B carries root B's Props on its context.
		cmd := &cobra.Command{Use: "b-cmd"}
		cmd.SetContext(ContextWithProps(context.Background(), &props.Props{Collector: rootB}))

		require.NoError(t, handler(cmd, nil))

		assert.Equal(t, 0, rootA.calls, "root A's collector must not be used for root B's command")
		assert.Equal(t, 1, rootB.calls, "root B's own collector records the command")
		assert.Equal(t, "b-cmd", rootB.name)
	})
}
