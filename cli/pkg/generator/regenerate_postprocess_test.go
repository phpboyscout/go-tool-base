package generator

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestRunPostRegenerationProcessing_RunsGoModTidyBeforeLint locks in the fix for
// the regenerate clean-run invariant: `regenerate project` rewrites go.mod from
// the embedded template snapshot, which omits the `tool` directives' transitive
// dependencies. Post-processing must therefore run `go mod tidy` (as generate's
// runSkeletonPostProcessing does) — and before golangci-lint and the hash
// refresh — or a freshly generated, unchanged project would not survive a
// regenerate unchanged (go.mod stripped to the template snapshot, then the
// manifest recording that stripped hash).
func TestRunPostRegenerationProcessing_RunsGoModTidyBeforeLint(t *testing.T) {
	t.Parallel()

	var (
		mu  sync.Mutex
		got [][]string
	)

	p := &props.Props{
		// The post-processing is a no-op unless the FS is a real OsFs; use a
		// real temp dir but fake every command so nothing shells out.
		FS:     afero.NewOsFs(),
		Logger: logger.NewNoop(),
	}

	g := New(p, &Config{Path: t.TempDir()})
	g.runCommand = func(_ context.Context, _, name string, args ...string) ([]byte, error) {
		mu.Lock()
		got = append(got, append([]string{name}, args...))
		mu.Unlock()

		return nil, nil
	}

	g.runPostRegenerationProcessing(context.Background(), map[string]string{})

	tidyIdx := slices.IndexFunc(got, func(c []string) bool {
		return slices.Equal(c, []string{"go", "mod", "tidy"})
	})
	lintIdx := slices.IndexFunc(got, func(c []string) bool {
		return len(c) > 0 && c[0] == "golangci-lint"
	})

	require.NotEqualf(t, -1, tidyIdx,
		"regenerate post-processing must run `go mod tidy`; recorded commands: %v", got)
	require.NotEqual(t, -1, lintIdx, "regenerate post-processing must still run golangci-lint")
	require.Lessf(t, tidyIdx, lintIdx,
		"`go mod tidy` must run before golangci-lint; recorded commands: %v", got)
}
