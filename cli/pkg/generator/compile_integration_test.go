package generator

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
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
	t.Parallel()

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
		Config: emptyTestStore(t),
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

	// Spec 0201 D5/D8: an annotated nested command renders, builds, and its
	// hints reach the MCP export alongside the built-in defaults (D6).
	require.NoError(t, New(p, &Config{Path: path}).SetMCPHints(context.Background(), "deploy/canary",
		setup.MCPHints{Title: "Canary deploy", ReadOnly: new(false), OpenWorld: new(true)}, false))

	injectGoToolBaseReplace(t, path, localModule)

	runGo(t, path, "mod", "tidy")
	// -buildvcs=false: the scaffold lives in a bare temp dir, and VCS
	// stamping would shell out to git and fail there rather than build.
	runGo(t, path, "build", "-buildvcs=false", "./...")

	// The scaffold's own lint config, without --fix: what a downstream
	// project sees on day one must be clean as emitted (#30).
	runLintClean(t, path)

	// Spec 0200 D5: the seeded require lines are what tidy keeps, so a
	// tidied scaffold regenerated and tidied again is byte-identical.
	assertGoModFixedPoint(t, g, path)

	assertMCPExportCarriesHints(t, path)
}

// assertMCPExportCarriesHints builds the scaffold binary and reads its
// `mcp tools` export: the annotated command carries what the manifest said,
// and a built-in carries the framework's default.
func assertMCPExportCarriesHints(t *testing.T, path string) {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "compile-tool")
	runGo(t, path, "build", "-buildvcs=false", "-o", bin, "./cmd/compile-tool")

	work := t.TempDir()
	env := []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH")}

	// The export runs under the framework's config gate, so bootstrap first.
	initCmd := exec.Command(bin, "init")
	initCmd.Env = env
	out, err := initCmd.CombinedOutput()
	require.NoErrorf(t, err, "init:\n%s", out)

	cmd := exec.Command(bin, "mcp", "tools")
	cmd.Dir = work
	cmd.Env = env
	out, err = cmd.CombinedOutput()
	require.NoErrorf(t, err, "mcp tools:\n%s", out)

	raw, err := os.ReadFile(filepath.Join(work, "mcp-tools.json"))
	require.NoError(t, err)

	var tools []struct {
		Name        string         `json:"name"`
		Title       string         `json:"title"`
		Annotations map[string]any `json:"annotations"`
	}
	require.NoError(t, json.Unmarshal(raw, &tools))

	byName := map[string]map[string]any{}
	titles := map[string]string{}

	for _, tool := range tools {
		byName[tool.Name] = tool.Annotations
		titles[tool.Name] = tool.Title
	}

	require.Contains(t, byName, "compile-tool_deploy_canary")
	assert.Equal(t, "Canary deploy", titles["compile-tool_deploy_canary"], "the title is the tool's own, not an annotation")
	assert.Equal(t, false, byName["compile-tool_deploy_canary"]["readOnlyHint"])
	assert.Equal(t, true, byName["compile-tool_deploy_canary"]["openWorldHint"])
	assert.NotContains(t, byName["compile-tool_deploy_canary"], "destructiveHint", "an unset hint is absent, not false")

	require.Contains(t, byName, "compile-tool_version")
	assert.Equal(t, true, byName["compile-tool_version"]["readOnlyHint"], "a built-in carries the framework default")
}

func assertGoModFixedPoint(t *testing.T, g *Generator, path string) {
	t.Helper()

	before, err := os.ReadFile(filepath.Join(path, "go.mod"))
	require.NoError(t, err)

	g.config.Path = path
	g.config.Overwrite = OverwriteAllow
	g.config.NoVerify = true
	require.NoError(t, g.RegenerateProject(context.Background()))

	runGo(t, path, "mod", "tidy")

	after, err := os.ReadFile(filepath.Join(path, "go.mod"))
	require.NoError(t, err)
	require.Equal(t, string(before), string(after), "go.mod must be a fixed point of regenerate + tidy")
}

// runLintClean runs golangci-lint in dir with the scaffold's config and
// fails the test on any finding. Skipped when golangci-lint is not on PATH.
func runLintClean(t *testing.T, dir string) {
	t.Helper()

	if _, err := exec.LookPath("golangci-lint"); err != nil {
		t.Skipf("golangci-lint not on PATH: %v", err)
	}

	cmd := exec.Command("golangci-lint", "run", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("golangci-lint run failed in %s: %v\noutput:\n%s", dir, err, string(out))
	}
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
// test file's location until it finds the workspace file: the first go.mod
// above this file is cli/'s, the nested module, not the framework's.
func localGoToolBasePath(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine test file location via runtime.Caller")
	}

	dir := filepath.Dir(file)

	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}

		dir = parent
	}

	t.Fatal("could not locate go-tool-base go.work walking up from test file")

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

// TestGeneratedDefaultProjectCanCheckForUpdates is the contract spec 0195
// D10 adds: a project generated with every default links its backend's
// adapter, so the built binary, stamped with a release version, constructs
// its updater rather than logging that no provider is registered. The
// e2e suite cannot see this (the e2e binary embeds its own fixture and the
// generator scenarios do not build), which is how the default project shipped
// unable to update itself.
func TestGeneratedDefaultProjectCanCheckForUpdates(t *testing.T) {
	t.Parallel()

	testutil.SkipIfNotIntegration(t, "generator", "generator_build")

	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("`go` not on PATH: %v", err)
	}

	localModule := localGoToolBasePath(t)
	path := t.TempDir()

	p := &props.Props{FS: afero.NewOsFs(), Logger: logger.NewNoop(), Config: emptyTestStore(t)}
	g := New(p, &Config{})
	g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) { return nil, nil }

	features := make([]ManifestFeature, 0, len(DefaultSelectedFeatures))
	for _, name := range DefaultSelectedFeatures {
		features = append(features, ManifestFeature{Name: name, Enabled: true})
	}

	// The backend implies its forge feature (D1); the flag path does this in
	// SkeletonOptions.resolveFeatures, so the library caller states it.
	features = append(features, ManifestFeature{Name: "github", Enabled: true})

	require.NoError(t, g.GenerateSkeleton(context.Background(), SkeletonConfig{
		Name: "updtool", Repo: "test/updtool", Host: "github.com", Path: path,
		ForgeBackend: "github", ReleaseChannel: ReleaseChannelForge,
		Features: features,
	}))

	injectGoToolBaseReplace(t, path, localModule)
	runGo(t, path, "mod", "tidy")

	bin := filepath.Join(t.TempDir(), "updtool")
	runGo(t, path, "build", "-buildvcs=false",
		"-ldflags", "-X github.com/test/updtool/internal/version.version=v1.0.0",
		"-o", bin, "./cmd/updtool")

	home := t.TempDir()
	env := []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}

	initCmd := exec.Command(bin, "init")
	initCmd.Env = env
	initOut, _ := initCmd.CombinedOutput()

	verCmd := exec.Command(bin, "version")
	verCmd.Env = env
	out, _ := verCmd.CombinedOutput()

	assert.NotContains(t, string(out), "No provider is registered",
		"the default project must link its backend's adapter\ninit:\n%s\nversion:\n%s", initOut, out)
}

// TestGeneratedProjectShipsItsChatDefault proves spec 0196 D4 end to end: the
// author's default is not just a file beside chat.go, it is read by the built
// tool as its lowest config layer. The embedded-defaults layer opens one fixed
// path per bundle and silently skips a bundle that lacks it, so a bundle at
// the wrong path would pass every file-level assertion and change nothing.
func TestGeneratedProjectShipsItsChatDefault(t *testing.T) {
	t.Parallel()

	testutil.SkipIfNotIntegration(t, "generator", "generator_build")

	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("`go` not on PATH: %v", err)
	}

	localModule := localGoToolBasePath(t)
	path := t.TempDir()

	p := &props.Props{FS: afero.NewOsFs(), Logger: logger.NewNoop(), Config: emptyTestStore(t)}
	g := New(p, &Config{})
	g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) { return nil, nil }

	require.NoError(t, g.GenerateSkeleton(context.Background(), SkeletonConfig{
		Name: "chattool", Repo: "test/chattool", Host: "github.com", Path: path,
		ForgeBackend: "github", ReleaseChannel: ReleaseChannelForge,
		Features: []ManifestFeature{
			{Name: string(props.AiCmd), Enabled: true},
			{Name: string(props.ConfigCmd), Enabled: true},
			{Name: string(props.InitCmd), Enabled: true},
			{Name: string(props.DoctorCmd), Enabled: true},
			{Name: "github", Enabled: true},
		},
		Chat: ManifestChat{
			Providers: []string{"claude-local"},
			Default:   ManifestChatDefault{Provider: "claude-local", Model: "claude-opus-5"},
		},
	}))

	injectGoToolBaseReplace(t, path, localModule)
	runGo(t, path, "mod", "tidy")

	bin := filepath.Join(t.TempDir(), "chattool")
	runGo(t, path, "build", "-buildvcs=false",
		"-ldflags", "-X github.com/test/chattool/internal/version.version=v1.0.0",
		"-o", bin, "./cmd/chattool")

	home := t.TempDir()
	env := []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}

	initCmd := exec.Command(bin, "init")
	initCmd.Env = env
	initOut, err := initCmd.CombinedOutput()
	require.NoErrorf(t, err, "init:\n%s", initOut)

	// init seeds the user's file from the bundle's init template; that is one
	// half of D4. Blank the user's file so the read below can only be
	// satisfied by the embedded-defaults layer, the other half.
	userConfigs, err := filepath.Glob(filepath.Join(home, "*", "chattool", "config.yaml"))
	require.NoError(t, err)

	if len(userConfigs) == 0 {
		userConfigs, err = filepath.Glob(filepath.Join(home, ".chattool", "config.yaml"))
		require.NoError(t, err)
	}

	require.Lenf(t, userConfigs, 1, "init wrote one config file under %s\n%s", home, initOut)

	seeded, err := os.ReadFile(userConfigs[0])
	require.NoError(t, err)
	assert.Contains(t, string(seeded), "claude-local", "the init template seeds the author's default into the user's file")
	require.NoError(t, os.WriteFile(userConfigs[0], []byte("log:\n  level: info\n"), 0o600))

	for key, want := range map[string]string{"ai.provider": "claude-local", "ai.model": "claude-opus-5"} {
		cmd := exec.Command(bin, "config", "get", key)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "config get %s:\n%s", key, out)
		assert.Containsf(t, string(out), want, "the built tool reads %s from its embedded chat defaults\n%s", key, out)
	}

	// Spec 0196 D8: the built tool's doctor knows which providers it links.
	doctorCmd := exec.Command(bin, "doctor")
	doctorCmd.Env = env
	doctorOut, _ := doctorCmd.CombinedOutput()
	assert.Contains(t, string(doctorOut), "Chat providers: claude, claude-local linked",
		"the tool links chat-anthropic, which registers both claude names\n%s", doctorOut)
}

// TestGeneratedProjectWithPostureBuilds proves spec 0197 D4 end to end: the
// rendered RequireChecksum and AuxiliaryCommands are real fields on real
// types, and the built tool runs. A text assertion on cmd.go cannot tell a
// field on the wrong struct from the right one; the compiler can.
func TestGeneratedProjectWithPostureBuilds(t *testing.T) {
	t.Parallel()

	testutil.SkipIfNotIntegration(t, "generator", "generator_build")

	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("`go` not on PATH: %v", err)
	}

	localModule := localGoToolBasePath(t)
	path := t.TempDir()

	p := &props.Props{FS: afero.NewOsFs(), Logger: logger.NewNoop(), Config: emptyTestStore(t)}
	g := New(p, &Config{})
	g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) { return nil, nil }

	require.NoError(t, g.GenerateSkeleton(context.Background(), SkeletonConfig{
		Name: "posturetool", Repo: "test/posturetool", Host: "github.com", Path: path,
		ForgeBackend: "github", ReleaseChannel: ReleaseChannelForge,
		Features:  []ManifestFeature{{Name: "github", Enabled: true}},
		Signing:   ManifestSigning{RequireChecksum: true},
		Bootstrap: ManifestBootstrap{AutoInitialise: true, AuxiliaryCommands: []string{"completion"}},
	}))

	injectGoToolBaseReplace(t, path, localModule)
	runGo(t, path, "mod", "tidy")

	bin := filepath.Join(t.TempDir(), "posturetool")
	runGo(t, path, "build", "-buildvcs=false",
		"-ldflags", "-X github.com/test/posturetool/internal/version.version=v1.0.0",
		"-o", bin, "./cmd/posturetool")

	cmd := exec.Command(bin, "version")
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH")}
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "version:\n%s", out)
}
