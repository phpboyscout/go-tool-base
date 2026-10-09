//go:build !windows

package root

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSubscribedSignals: SIGTERM is always taken; SIGINT and SIGHUP are taken
// unless the process started with them ignored (spec 0207 D2, D3).
func TestSubscribedSignals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ignored []os.Signal
		want    []os.Signal
	}{
		{name: "nothing ignored", want: []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}},
		{name: "a background job", ignored: []os.Signal{os.Interrupt}, want: []os.Signal{syscall.SIGTERM, syscall.SIGHUP}},
		{name: "under nohup", ignored: []os.Signal{syscall.SIGHUP}, want: []os.Signal{os.Interrupt, syscall.SIGTERM}},
		{name: "SIGTERM cannot stay ignored", ignored: []os.Signal{syscall.SIGTERM}, want: []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := subscribedSignals(func(sig os.Signal) bool { return slices.Contains(tt.ignored, sig) })

			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

// terminateHelperEnv makes the test binary act as a process that ends itself
// with terminateBySignal, so a parent can read how it died.
const terminateHelperEnv = "GTB_TEST_TERMINATE_HELPER"

// terminateHelperBinary is a hard link to this test binary, taken before any
// test runs: TestPerformUpdate_Success self-updates over os.Args[0] by rename,
// and the link keeps the original (#114). It sits beside the binary, in go
// test's work directory, so it goes when that does.
var terminateHelperBinary = linkTestBinary()

func linkTestBinary() string {
	if os.Getenv(terminateHelperEnv) != "" {
		return os.Args[0]
	}

	link := filepath.Join(filepath.Dir(os.Args[0]), fmt.Sprintf("terminate-helper-%d", os.Getpid()))
	if err := os.Link(os.Args[0], link); err != nil {
		return os.Args[0]
	}

	return link
}

func TestTerminateHelperProcess(t *testing.T) {
	sig := os.Getenv(terminateHelperEnv)
	if sig == "" {
		t.Skip("subprocess helper for TestTerminateBySignal")
	}

	if sig == "INT" {
		terminateBySignal(syscall.SIGINT, signalExitBase+int(syscall.SIGINT))
	}

	terminateBySignal(syscall.SIGTERM, signalExitBase+int(syscall.SIGTERM))
}

// TestTerminateBySignal: the default terminate step ends the process by the
// signal, so a parent reading the wait status sees a death by it; when the
// signal is ignored, the bounded wait falls back to exiting 128+signum
// (spec 0207 D1).
func TestTerminateBySignal(t *testing.T) {
	t.Parallel()

	t.Run("dies by the signal", func(t *testing.T) {
		t.Parallel()

		status := runTerminateHelper(t, exec.Command(terminateHelperBinary, "-test.run=^TestTerminateHelperProcess$"), "TERM")

		require.True(t, status.Signaled(), "the helper must die by a signal, got exit status %d", status.ExitStatus())
		assert.Equal(t, syscall.SIGTERM, status.Signal())
	})

	t.Run("falls back to 128+signum when the signal is ignored", func(t *testing.T) {
		t.Parallel()

		sh, err := exec.LookPath("sh")
		if err != nil {
			t.Skip("needs sh to start the helper with SIGINT ignored")
		}

		// exec.Cmd cannot set an ignored disposition, so sh does it and execs.
		cmd := exec.Command(sh, "-c", `trap "" INT; exec "$0" -test.run='^TestTerminateHelperProcess$'`, terminateHelperBinary)
		status := runTerminateHelper(t, cmd, "INT")

		require.False(t, status.Signaled(), "an ignored signal cannot end the helper")
		assert.Equal(t, signalExitBase+int(syscall.SIGINT), status.ExitStatus())
	})
}

func runTerminateHelper(t *testing.T, cmd *exec.Cmd, sig string) syscall.WaitStatus {
	t.Helper()

	cmd.Env = append(os.Environ(), terminateHelperEnv+"="+sig)

	err := cmd.Run()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "the helper must not exit zero")

	status, ok := exitErr.Sys().(syscall.WaitStatus)
	require.True(t, ok)

	return status
}
