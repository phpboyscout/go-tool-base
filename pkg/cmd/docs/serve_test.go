package docs

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func newTestServeFS(t *testing.T) fstest.MapFS {
	t.Helper()

	return fstest.MapFS{
		"assets/site/index.html": {Data: []byte("<h1>docs</h1>")},
	}
}

// TestNewCmdDocsServe_UsesRunE asserts the serve command exposes its handler
// via RunE (not Run). RunE is the prerequisite for routing through the
// middleware chain wired by setup.Command.Register — a Run handler is
// invisible to that wiring and bypasses recovery/timing/telemetry.
func TestNewCmdDocsServe_UsesRunE(t *testing.T) {
	t.Parallel()

	cmd := NewCmdDocsServe(&props.Props{}, newTestServeFS(t))

	assert.NotNil(t, cmd.RunE, "serve must expose RunE so middleware can wrap it")
	assert.Nil(t, cmd.Run, "serve must not use Run — that bypasses the middleware chain")
}

// TestNewCmdDocsServe_HostFlagDefaultsToLoopback asserts the new --host flag
// exists and defaults to the loopback interface, matching the secure-by-
// default bind in pkg/docs.Serve.
func TestNewCmdDocsServe_HostFlagDefaultsToLoopback(t *testing.T) {
	t.Parallel()

	cmd := NewCmdDocsServe(&props.Props{}, newTestServeFS(t))

	flag := cmd.Flags().Lookup("host")
	require.NotNil(t, flag, "serve must expose a --host flag to widen the bind")
	assert.Equal(t, "127.0.0.1", flag.DefValue, "default bind must be loopback")
}

// TestNewCmdDocsServe_RoutesThroughMiddleware proves that registering the
// serve command via setup.Command.Register wraps its RunE with the
// middleware chain. A sentinel middleware records its invocation; if the
// command used Run instead of RunE the wrapping would be skipped and the
// sentinel never fire.
func TestNewCmdDocsServe_RoutesThroughMiddleware(t *testing.T) {
	t.Parallel()

	called := false

	sentinel := func(next func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, args []string) error {
			called = true

			return next(cmd, args)
		}
	}

	// The parent carries the chain, the way the root does (spec 0199 D3).
	set, err := features.Resolve(features.Default().Snapshot(), nil)
	require.NoError(t, err)

	parent := setup.Wrap(props.DocsCmd, &cobra.Command{Use: "docs"})
	parent.UseChain(setup.NewMiddlewareChain([]setup.Middleware{sentinel}, set))
	serve := NewCmdDocsServe(&props.Props{}, newTestServeFS(t))
	parent.Register(setup.Wrap(props.DocsCmd, serve))

	// Invoke the (now-wrapped) RunE with a flag combination that returns
	// before binding a socket: --open=false and an out-of-range port make
	// Serve fail fast, but the sentinel middleware must already have run.
	require.NoError(t, serve.Flags().Set("open", "false"))
	require.NoError(t, serve.Flags().Set("port", "99999"))

	_ = serve.RunE(serve, nil)

	assert.True(t, called, "serve RunE must be wrapped by the middleware chain")
}

// TestNewCmdDocsServe_OpenWithCancelledContext drives the auto-open branch
// without launching a browser: browser.OpenURL refuses a cancelled context
// before invoking the OS opener, and the out-of-range port fails the bind.
func TestNewCmdDocsServe_OpenWithCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	cmd := NewCmdDocsServe(&props.Props{}, newTestServeFS(t))
	cmd.SetContext(ctx)
	require.NoError(t, cmd.Flags().Set("open", "true"))
	require.NoError(t, cmd.Flags().Set("port", "99999"))

	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start listener")
}
