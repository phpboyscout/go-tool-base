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

// Issue #21: the generator wrote a Run<Name> stub returning ErrRunSubCommand for
// a command with children, and then suppressed the RunE that would call it. The
// stub was unreachable in every generated tool, and a bare group invocation fell
// through to cobra's default — help printed, exit 0.
//
// It was first answered by wiring the RunE, making the stub reachable and a bare
// group a usage error. Spec 0190 answers it the other way instead: a command with
// no run logic gets no run function at all, and its RunE is setup.GroupRunE. The
// tests here cover what remains of that machinery — a WORKING group, which still
// calls its own Run<Name>, and the stub injection that keeps a preserved main.go
// compiling.
//
// The stub no longer varies by whether the command has children. Only a command
// that calls Run<Name> gets one, and for such a command "not implemented yet" is
// the accurate thing to say; a group that would have said "subcommand required"
// now says it by wiring GroupRunE, which needs no stub to say anything.

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

func TestCommandRegistration_WiresRunEForAWorkingGroup(t *testing.T) {
	t.Parallel()

	rendered := renderRegistration(t, groupData())

	assert.Contains(t, rendered, "RunE:",
		"a group must wire RunE either way: a command that is not Runnable never "+
			"evaluates Args, so cobra answers a mistyped subcommand with help and success")
	assert.Contains(t, rendered, "RunGamma(cmd.Context()",
		"a group with run logic of its own still calls it")
	assert.NotContains(t, rendered, "GroupRunE",
		"and the framework default does not displace it")
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
	assert.Contains(t, string(got), "errorhandling.ErrNotImplemented",
		"the injected stub says what is true of any command that has one: nothing is "+
			"implemented there yet")
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

func TestEnsureHookStubs_LeavesATouchedStubAlone(t *testing.T) {
	t.Parallel()

	// Anything beyond the single generated return is intent, and is kept — the
	// same rule the hash refresh follows for a file the developer edited. Note
	// this also keeps the command classified as WORKING, which is the seam a
	// developer uses to opt out of the group default.
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

func TestEnsureHookStubs_LeavesALeafStubAlone(t *testing.T) {
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

// A pure group's cmd.go wires setup.GroupRunE and calls nothing in main.go, so
// injecting a Run stub would put back exactly what issue #21 objected to: an
// exported function nobody calls.
func TestEnsureHookStubs_InjectsNoRunStubForAPureGroup(t *testing.T) {
	t.Parallel()

	g, cmdDir := groupWiringFixture(t)
	mainFile := filepath.Join(cmdDir, "main.go")

	// main.go exists for a hook, and has no Run of its own.
	existing := `package gamma

import (
	"context"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

type GammaOptions struct{}

func PreRunGamma(ctx context.Context, props *props.Props, opts *GammaOptions, args []string) error {
	return nil
}
`
	require.NoError(t, afero.WriteFile(g.props.FS, mainFile, []byte(existing), DefaultFileMode))

	data := groupData()
	data.PureGroup = true
	data.PreRun = true

	require.NoError(t, g.ensureHookStubs(context.Background(), mainFile, data))

	got, err := afero.ReadFile(g.props.FS, mainFile)
	require.NoError(t, err)

	assert.Equal(t, existing, string(got), "nothing is injected and nothing is rewritten")
	assert.NotContains(t, string(got), "func RunGamma(")
}
