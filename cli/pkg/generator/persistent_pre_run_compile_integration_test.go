package generator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

const tracingMain = `package %[1]s

import (
	"context"
	"fmt"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func PersistentPreRun%[2]s(_ context.Context, props *props.Props, _ *%[2]sOptions, _ []string) error {
	_, err := fmt.Fprintln(props.GetIO().Out(), "trace: pre %[1]s")

	return err
}

func Run%[2]s(_ context.Context, props *props.Props, _ *%[2]sOptions, _ []string) error {
	_, err := fmt.Fprintln(props.GetIO().Out(), "trace: run %[1]s")

	return err
}
`

// TestGeneratedPersistentPreRunRunsOnce builds a tool whose commands have
// persistent pre-runs at two levels, the second below a command without one,
// and checks each ancestor's runs once, outermost first (#236).
func TestGeneratedPersistentPreRunRunsOnce(t *testing.T) {
	t.Parallel()

	testutil.SkipIfNotIntegration(t, "generator", "generator_build")

	path := t.TempDir()
	p := &props.Props{FS: afero.NewOsFs(), Logger: logger.NewNoop(), Config: emptyTestStore(t)}
	hooked := func(c *Config) { c.PersistentPreRun = true }

	g := New(p, &Config{Path: path})
	g.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil }
	require.NoError(t, g.GenerateSkeleton(context.Background(), SkeletonConfig{
		Name: "hook-tool", Repo: "test/hook-tool", Host: "github.com", Description: "d", Path: path,
	}))

	addCmd(t, p, path, "outer", "root", hooked)
	addCmd(t, p, path, "plain", "outer")
	addCmd(t, p, path, "inner", "outer/plain", hooked)
	addCmd(t, p, path, "leaf", "outer/plain/inner")

	for dir, name := range map[string]string{"outer": "Outer", "outer/plain/inner": "Inner", "outer/plain/inner/leaf": "Leaf"} {
		require.NoError(t, os.WriteFile(filepath.Join(path, "pkg", "cmd", dir, "main.go"),
			fmt.Appendf(nil, tracingMain, filepath.Base(dir), name), 0o600))
	}

	injectGoToolBaseReplace(t, path, localGoToolBasePath(t))
	runGo(t, path, "mod", "tidy")

	bin := filepath.Join(t.TempDir(), "hook-tool")
	runGo(t, path, "build", "-buildvcs=false", "-o", bin, "./cmd/hook-tool")

	// An untrusted project file is warned about once for each framework pre-run.
	work := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(work, ".hook-tool.yaml"), []byte("update:\n  require_signature: false\n"), 0o600))

	run := newHookToolRunner(t, bin, work)
	run("init")

	for _, tt := range []struct{ args, want []string }{
		{[]string{"outer"}, []string{"pre outer"}},
		{[]string{"outer", "plain"}, []string{"pre outer"}},
		{[]string{"outer", "plain", "inner"}, []string{"pre outer", "pre inner"}},
		{[]string{"outer", "plain", "inner", "leaf"}, []string{"pre outer", "pre inner", "run leaf"}},
	} {
		out := run(tt.args...)

		var got []string

		for line := range strings.Lines(out) {
			if step, ok := strings.CutPrefix(strings.TrimSpace(line), "trace: "); ok {
				got = append(got, step)
			}
		}

		assert.Equal(t, tt.want, got, "%v:\n%s", tt.args, out)
		assert.Equal(t, 1, strings.Count(out, ".hook-tool.yaml"), "%v:\n%s", tt.args, out)
	}
}
