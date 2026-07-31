package root

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// signalOptOutSettleDelay bounds how long a command waits to see whether its
// context gets cancelled. Long enough that a live watcher goroutine would have
// acted; short enough not to slow the suite.
const signalOptOutSettleDelay = 100 * time.Millisecond

// TestWithoutSignals_SetsTheFlag pins the exported option to the internal seam
// it drives, so the plumbing cannot silently come apart.
func TestWithoutSignals_SetsTheFlag(t *testing.T) {
	t.Parallel()

	var opts executeOptions

	require.False(t, opts.disableSignals, "the zero value must wire signals ON")

	WithoutSignals()(&opts)

	assert.True(t, opts.disableSignals,
		"WithoutSignals must disable the framework's signal handling")
}

// TestExecute_WithoutSignals_LeavesTheContextAlone is spec 0001 D5: a tool that
// owns signals itself must be able to decline the framework's handler. With the
// opt-out in force, a signal arriving on the notification channel must not
// cancel the command context — nothing is watching it.
func TestExecute_WithoutSignals_LeavesTheContextAlone(t *testing.T) {
	props, spy := newSignalTestProps()

	sigCh := make(chan os.Signal, signalBuffer)
	cancelled := make(chan struct{})

	cmd := &cobra.Command{
		Use: "root",
		RunE: func(c *cobra.Command, _ []string) error {
			// Would cancel the context if the framework were still watching.
			sigCh <- syscall.SIGINT

			select {
			case <-c.Context().Done():
				close(cancelled)
			case <-time.After(signalOptOutSettleDelay):
			}

			return nil
		},
	}

	execute(setup.Wrap("", cmd), props, executeOptions{
		signals:        sigCh,
		disableSignals: true,
	})

	select {
	case <-cancelled:
		t.Fatal("WithoutSignals must leave the command context uncancelled by signals")
	default:
	}

	assert.Empty(t, spy.codes,
		"an opted-out run must not exit 128+signum: no signal was handled")
}

// TestExecute_DefaultStillHandlesSignals guards the other side of D5 — the
// opt-out must not become the default by accident.
func TestExecute_DefaultStillHandlesSignals(t *testing.T) {
	props, spy := newSignalTestProps()

	sigCh := make(chan os.Signal, signalBuffer)
	cancelled := make(chan struct{})

	cmd := &cobra.Command{
		Use: "root",
		RunE: func(c *cobra.Command, _ []string) error {
			sigCh <- syscall.SIGINT

			select {
			case <-c.Context().Done():
				close(cancelled)
			case <-time.After(executeTestTimeout):
			}

			return nil
		},
	}

	execute(setup.Wrap("", cmd), props, executeOptions{signals: sigCh})

	select {
	case <-cancelled:
	default:
		t.Fatal("without the opt-out, a signal must still cancel the command context")
	}

	assert.Equal(t, []int{signalExitBase + int(syscall.SIGINT)}, spy.codes,
		"a signal-terminated run must exit 128+signum")
}
