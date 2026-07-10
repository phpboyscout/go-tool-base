package generator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestGeneratedProjectCompiles is the regression catcher Q4 from the
// command-composition-registration spec calls for. The previous test suite
// asserted file-content shapes but never tried to `go build` the generated
// module, so the nested-command path that referenced undefined
// `props.<Name>Cmd` symbols compiled cleanly in tests and broke only when
// downstream users built their tools.
//
// This test scaffolds a project on the real filesystem, adds a nested
// subcommand, and runs `go build ./...` to prove the generated module
// compiles end-to-end. A `replace` directive points go-tool-base at the
// local checkout so the test passes against the module under development.
func TestGeneratedProjectCompiles(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "generator", "generator_build")

	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("`go` not on PATH: %v", err)
	}

	localModule := localGoToolBasePath(t)

	path := t.TempDir()
	fs := afero.NewOsFs()
	l := logger.NewNoop()

	p := &props.Props{
		FS:     fs,
		Logger: l,
		Config: config.NewFilesContainer(fs, config.WithLogger(logger.ToSlog(l))),
	}

	// Use a no-op runCommand so the skeleton does not auto-run
	// `go mod tidy` / `golangci-lint` — the test injects a replace
	// directive between generation and the build step.
	g := New(p, &Config{})
	g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
		return nil, nil
	}

	cfg := SkeletonConfig{
		Name:        "compile-tool",
		Repo:        "test/compile-tool",
		Host:        "github.com",
		Description: "Compile-time regression test project",
		Path:        path,
		// Disable changelog so the skeleton does not emit
		// //go:generate go tool changelog ... directives that
		// require the changelog tool to be reachable.
		Features: []ManifestFeature{
			{Name: "changelog", Enabled: false},
			{Name: "docs", Enabled: false},
		},
	}

	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg), "skeleton generation must succeed")

	// Add a root-level command followed by a nested subcommand. The nested
	// case is the bug class this test exists to catch.
	addCmd(t, p, path, "deploy", "root")
	addCmd(t, p, path, "canary", "deploy")

	injectGoToolBaseReplace(t, path, localModule)

	runGo(t, path, "mod", "tidy")
	runGo(t, path, "build", "./...")
}

// runGo runs a `go <args...>` command in dir and fails the test on any
// non-zero exit. Combined stderr+stdout is captured so failures show the
// actual compiler / loader output.
func runGo(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("go", args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %v failed in %s: %v\noutput:\n%s",
			args, dir, err, string(out))
	}
}

// localGoToolBasePath resolves the absolute filesystem path of the
// go-tool-base checkout the test is running inside. It walks up from this
// test file's location until it finds the repo's go.mod.
func localGoToolBasePath(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine test file location via runtime.Caller")
	}

	dir := filepath.Dir(file)

	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}

		dir = parent
	}

	t.Fatal("could not locate go-tool-base go.mod walking up from test file")

	return ""
}

// injectGoToolBaseReplace appends a `replace gitlab.com/phpboyscout/go-tool-base => <local>`
// directive to the generated project's go.mod so that `go mod tidy` /
// `go build` resolve against the current checkout rather than the
// publicly-released module version.
func injectGoToolBaseReplace(t *testing.T, projectPath, localGoToolBase string) {
	t.Helper()

	goModPath := filepath.Join(projectPath, "go.mod")

	existing, err := afero.ReadFile(afero.NewOsFs(), goModPath)
	require.NoError(t, err, "generated go.mod must exist")

	replaceLine := "\nreplace gitlab.com/phpboyscout/go-tool-base => " + localGoToolBase + "\n"

	require.NoError(t,
		afero.WriteFile(afero.NewOsFs(), goModPath, append(existing, []byte(replaceLine)...), 0o644),
		"failed to append replace directive")
}
