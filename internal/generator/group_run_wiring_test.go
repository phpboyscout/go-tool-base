package generator

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Issue #21: the generator writes a Run<Name> stub returning ErrRunSubCommand
// for a command with children, and then suppressed the RunE that would call it.
// The stub was unreachable in every generated tool, and a bare group invocation
// fell through to cobra's default — help printed, exit 0.
//
// errorhandling v0.3.0 states the contract the generator was half-implementing:
//
//	// ErrRunSubCommand marks a parent command invoked without a subcommand. The
//	// generator emits `return errorhandling.ErrRunSubCommand` for a command
//	// that has children.
//	ErrRunSubCommand = WithOutcome(
//		errors.NewSentinel("errorhandling.run_subcommand", "subcommand required"),
//		Outcome{Code: ExitCodeUsage, Level: slog.LevelWarn, Usage: true},
//	)
//
// so a bare group is a usage error that prints usage and exits non-zero. The
// PreRunE the generator already emits installs the usage printer via
// props.ErrorHandler.SetUsage — the half that was wired.

func groupWiringFixture(t *testing.T) (*Generator, string) {
	t.Helper()

	fs := afero.NewMemMapFs()
	root := "/work"
	cmdDir := filepath.Join(root, "pkg", "cmd", "gamma")

	require.NoError(t, fs.MkdirAll(cmdDir, DefaultDirMode))
	require.NoError(t, fs.MkdirAll(filepath.Join(root, ".gtb"), DefaultDirMode))

	p := &props.Props{FS: fs, Logger: logger.NewNoop()}

	return New(p, &Config{Path: root, Name: "gamma"}), cmdDir
}

// renderRegistration renders a command's cmd.go the way the generator does.
func renderRegistration(t *testing.T, data templates.CommandData) string {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, templates.CommandRegistration(data).Render(&buf))

	return buf.String()
}

func groupData() templates.CommandData {
	return templates.CommandData{
		Package:        "gamma",
		Name:           "gamma",
		PascalName:     "Gamma",
		HasSubcommands: true,
		Hashes:         map[string]string{},
	}
}

func TestCommandRegistration_WiresRunEForACommandGroup(t *testing.T) {
	t.Parallel()

	rendered := renderRegistration(t, groupData())

	assert.Contains(t, rendered, "RunE:",
		"a group must call its Run stub, or the stub is unreachable and a bare "+
			"invocation exits 0 instead of reporting a usage error")
	assert.Contains(t, rendered, "RunGamma(cmd.Context()")
}

func TestCommandRegistration_StillWiresRunEForALeaf(t *testing.T) {
	t.Parallel()

	data := groupData()
	data.HasSubcommands = false

	rendered := renderRegistration(t, data)

	assert.Contains(t, rendered, "RunGamma(cmd.Context()")
}

func TestEnsureHookStubs_InjectsAMissingRunStub(t *testing.T) {
	t.Parallel()

	// main.go belongs to the developer and is preserved, so it can arrive
	// without the function cmd.go references — most obviously if they deleted
	// it. Emitting RunE against a function that does not exist is a hard
	// compile error, which is exactly the state ensureHookStubs exists to
	// prevent for the other hooks.
	g, cmdDir := groupWiringFixture(t)
	mainFile := filepath.Join(cmdDir, "main.go")

	handWritten := `package gamma

import (
	"context"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

type GammaOptions struct{}
`
	require.NoError(t, afero.WriteFile(g.props.FS, mainFile, []byte(handWritten), DefaultFileMode))

	require.NoError(t, g.ensureHookStubs(context.Background(), mainFile, groupData()))

	got, err := afero.ReadFile(g.props.FS, mainFile)
	require.NoError(t, err)

	assert.Contains(t, string(got), "func RunGamma(",
		"a preserved main.go missing the function cmd.go calls must have it injected")
	assert.Contains(t, string(got), "errorhandling.ErrRunSubCommand",
		"a group's stub reports that a subcommand is required")
	assert.Contains(t, string(got), `"gitlab.com/phpboyscout/go/errorhandling"`,
		"the injected stub needs its import")
}

func TestEnsureHookStubs_InjectsTheLeafStubForALeaf(t *testing.T) {
	t.Parallel()

	g, cmdDir := groupWiringFixture(t)
	mainFile := filepath.Join(cmdDir, "main.go")

	require.NoError(t, afero.WriteFile(g.props.FS, mainFile,
		[]byte("package gamma\n\ntype GammaOptions struct{}\n"), DefaultFileMode))

	data := groupData()
	data.HasSubcommands = false

	require.NoError(t, g.ensureHookStubs(context.Background(), mainFile, data))

	got, err := afero.ReadFile(g.props.FS, mainFile)
	require.NoError(t, err)

	assert.Contains(t, string(got), "errorhandling.ErrNotImplemented",
		"a leaf reports that it is not implemented, not that a subcommand is required")
}

func TestEnsureHookStubs_LeavesAnExistingRunAlone(t *testing.T) {
	t.Parallel()

	g, cmdDir := groupWiringFixture(t)
	mainFile := filepath.Join(cmdDir, "main.go")

	custom := `package gamma

import (
	"context"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

type GammaOptions struct{}

func RunGamma(ctx context.Context, props *props.Props, opts *GammaOptions, args []string) error {
	props.Logger.Info("my own implementation")

	return nil
}
`
	require.NoError(t, afero.WriteFile(g.props.FS, mainFile, []byte(custom), DefaultFileMode))

	require.NoError(t, g.ensureHookStubs(context.Background(), mainFile, groupData()))

	got, err := afero.ReadFile(g.props.FS, mainFile)
	require.NoError(t, err)

	assert.Equal(t, custom, string(got), "a developer's own Run must not be touched")
}

func TestEnsureHookStubs_UpgradesAnUntouchedLeafStubWhenTheCommandGainsChildren(t *testing.T) {
	t.Parallel()

	// The transition that produced the report: gamma was generated as a leaf,
	// so its stub says "not implemented"; adding a child makes it a group, and
	// what is true is that a subcommand is required.
	g, cmdDir := groupWiringFixture(t)
	mainFile := filepath.Join(cmdDir, "main.go")

	leafStub := `package gamma

import (
	"context"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go/errorhandling"
)

type GammaOptions struct{}

func RunGamma(ctx context.Context, props *props.Props, opts *GammaOptions, args []string) error {
	return errorhandling.ErrNotImplemented
}
`
	require.NoError(t, afero.WriteFile(g.props.FS, mainFile, []byte(leafStub), DefaultFileMode))

	require.NoError(t, g.ensureHookStubs(context.Background(), mainFile, groupData()))

	got, err := afero.ReadFile(g.props.FS, mainFile)
	require.NoError(t, err)

	assert.Contains(t, string(got), "errorhandling.ErrRunSubCommand")
	assert.NotContains(t, string(got), "errorhandling.ErrNotImplemented")
}

func TestEnsureHookStubs_DoesNotUpgradeAStubTheDeveloperHasTouched(t *testing.T) {
	t.Parallel()

	// Anything beyond the single generated return is intent, and is kept — the
	// same rule the hash refresh follows for a file the developer edited.
	g, cmdDir := groupWiringFixture(t)
	mainFile := filepath.Join(cmdDir, "main.go")

	touched := `package gamma

import (
	"context"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go/errorhandling"
)

type GammaOptions struct{}

func RunGamma(ctx context.Context, props *props.Props, opts *GammaOptions, args []string) error {
	props.Logger.Info("deliberately still a stub, for now")

	return errorhandling.ErrNotImplemented
}
`
	require.NoError(t, afero.WriteFile(g.props.FS, mainFile, []byte(touched), DefaultFileMode))

	require.NoError(t, g.ensureHookStubs(context.Background(), mainFile, groupData()))

	got, err := afero.ReadFile(g.props.FS, mainFile)
	require.NoError(t, err)

	assert.Equal(t, touched, string(got))
}

func TestEnsureHookStubs_DoesNotUpgradeALeafThatStayedALeaf(t *testing.T) {
	t.Parallel()

	g, cmdDir := groupWiringFixture(t)
	mainFile := filepath.Join(cmdDir, "main.go")

	leafStub := `package gamma

import (
	"context"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go/errorhandling"
)

type GammaOptions struct{}

func RunGamma(ctx context.Context, props *props.Props, opts *GammaOptions, args []string) error {
	return errorhandling.ErrNotImplemented
}
`
	require.NoError(t, afero.WriteFile(g.props.FS, mainFile, []byte(leafStub), DefaultFileMode))

	data := groupData()
	data.HasSubcommands = false

	require.NoError(t, g.ensureHookStubs(context.Background(), mainFile, data))

	got, err := afero.ReadFile(g.props.FS, mainFile)
	require.NoError(t, err)

	assert.Equal(t, leafStub, string(got))
}
