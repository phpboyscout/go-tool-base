package gomod

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const settled = `module github.com/acme/mytool

go 1.27.1

tool (
	gitlab.com/phpboyscout/go-tool-base/cmd/changelog
	gitlab.com/phpboyscout/go-tool-base/cmd/docs
)

require (
	github.com/acme/shared v1.2.3
	gitlab.com/phpboyscout/go-tool-base v0.42.0
	gitlab.com/phpboyscout/go/forge-github v0.22.0
	gitlab.com/phpboyscout/go/forge-gitlab v0.21.0
)

require (
	github.com/spf13/cobra v1.10.2 // indirect
	golang.org/x/text v0.31.0 // indirect
)

exclude github.com/acme/shared v1.2.2
`

func TestSeed_AddsMissingAndLeavesEverythingElse(t *testing.T) {
	t.Parallel()

	out, report, err := Seed([]byte(settled), []Requirement{
		{Path: "gitlab.com/phpboyscout/go-tool-base", Version: "v0.42.0"},
		{Path: "gitlab.com/phpboyscout/go/forge-github", Version: "v0.22.0"},
		{Path: "gitlab.com/phpboyscout/go/forge-gitlab", Version: "v0.21.0"},
		{Path: "gitlab.com/phpboyscout/go/chat-anthropic", Version: "v0.14.1"},
	}, nil)
	require.NoError(t, err)

	s := string(out)
	assert.Contains(t, s, "gitlab.com/phpboyscout/go/chat-anthropic v0.14.1")
	assert.Contains(t, s, "github.com/acme/shared v1.2.3", "a developer's own line survives")
	assert.Contains(t, s, "github.com/spf13/cobra v1.10.2 // indirect", "tidy's indirect lines survive")
	assert.Contains(t, s, "exclude github.com/acme/shared v1.2.2")
	assert.Contains(t, s, "gitlab.com/phpboyscout/go-tool-base/cmd/changelog", "the tool block survives")
	assert.Equal(t, []string{"gitlab.com/phpboyscout/go/chat-anthropic"}, report.Added)
	assert.Empty(t, report.Dropped)
	assert.Empty(t, report.Raised)
}

func TestSeed_LeavesAPresentLineAtAnyVersion(t *testing.T) {
	t.Parallel()

	for _, seeded := range []string{"v0.20.0", "v0.21.0", "v0.99.0"} {
		out, report, err := Seed([]byte(settled), []Requirement{
			{Path: "gitlab.com/phpboyscout/go/forge-gitlab", Version: seeded},
		}, nil)
		require.NoError(t, err)
		assert.Contains(t, string(out), "gitlab.com/phpboyscout/go/forge-gitlab v0.21.0", "seed %s must not move a present line", seeded)
		assert.Empty(t, report.Raised)
	}
}

func TestSeed_RaisesBelowADeclaredFloorOnly(t *testing.T) {
	t.Parallel()

	out, report, err := Seed([]byte(settled), []Requirement{
		{Path: "gitlab.com/phpboyscout/go/forge-gitlab", Version: "v0.23.0", Floor: true},
		{Path: "gitlab.com/phpboyscout/go/forge-github", Version: "v0.20.0", Floor: true},
	}, nil)
	require.NoError(t, err)

	s := string(out)
	assert.Contains(t, s, "gitlab.com/phpboyscout/go/forge-gitlab v0.23.0", "below the floor is raised")
	assert.Contains(t, s, "gitlab.com/phpboyscout/go/forge-github v0.22.0", "above the floor is left")
	assert.Equal(t, []Raise{{Path: "gitlab.com/phpboyscout/go/forge-gitlab", From: "v0.21.0", To: "v0.23.0"}}, report.Raised)
}

// prePhase4 is a v0.42.0 scaffold's tool block: gtb, golangci-lint and mockery
// beside the framework's own commands (spec 0197 D12 removed the three).
const prePhase4 = `module github.com/acme/mytool

go 1.27.1

tool (
	github.com/golangci/golangci-lint/cmd/golangci-lint
	github.com/vektra/mockery/v3
	gitlab.com/phpboyscout/go-tool-base/cli/cmd/gtb
	gitlab.com/phpboyscout/go-tool-base/cmd/changelog
	gitlab.com/phpboyscout/go-tool-base/cmd/docs
)

require gitlab.com/phpboyscout/go-tool-base v0.42.0
`

// TestSeed_DropsTheToolDirectivesAsked (#86): the tool lines an older
// scaffold carried for tools that are installed now are dropped and
// reported; the framework's own stay; a line that is not there is not
// reported.
func TestSeed_DropsTheToolDirectivesAsked(t *testing.T) {
	t.Parallel()

	out, report, err := Seed([]byte(prePhase4), nil, nil, WithoutTools(
		"gitlab.com/phpboyscout/go-tool-base/cli/cmd/gtb",
		"github.com/golangci/golangci-lint/cmd/golangci-lint",
		"github.com/vektra/mockery/v3",
		"example.com/never/there",
	))
	require.NoError(t, err)

	s := string(out)
	assert.NotContains(t, s, "cli/cmd/gtb")
	assert.NotContains(t, s, "golangci-lint")
	assert.NotContains(t, s, "mockery")
	assert.Contains(t, s, "gitlab.com/phpboyscout/go-tool-base/cmd/changelog")
	assert.Contains(t, s, "gitlab.com/phpboyscout/go-tool-base/cmd/docs")
	assert.Equal(t, []string{
		"gitlab.com/phpboyscout/go-tool-base/cli/cmd/gtb",
		"github.com/golangci/golangci-lint/cmd/golangci-lint",
		"github.com/vektra/mockery/v3",
	}, report.DroppedTools)
}

// TestSeed_DropsTheToolDirectivesAtEveryMajorAndOlderPath (#92): a scaffold
// from before the CLI moved to cli/ and before golangci-lint v2 carries the
// same tools at other paths, and an exact match left them for go mod tidy to
// fail on. A drop matches the module at any major version.
func TestSeed_DropsTheToolDirectivesAtEveryMajorAndOlderPath(t *testing.T) {
	t.Parallel()

	const preCLI = `module example.com/tool

go 1.27.1

tool (
	github.com/golangci/golangci-lint/v2/cmd/golangci-lint
	github.com/vektra/mockery/v3
	gitlab.com/phpboyscout/go-tool-base/cmd/changelog
	gitlab.com/phpboyscout/go-tool-base/cmd/docs
	gitlab.com/phpboyscout/go-tool-base/cmd/gtb
)
`

	out, report, err := Seed([]byte(preCLI), nil, nil, WithoutTools(
		"gitlab.com/phpboyscout/go-tool-base/cli/cmd/gtb",
		"gitlab.com/phpboyscout/go-tool-base/cmd/gtb",
		"github.com/golangci/golangci-lint/cmd/golangci-lint",
		"github.com/vektra/mockery/v3",
	))
	require.NoError(t, err)

	s := string(out)
	assert.NotContains(t, s, "cmd/gtb")
	assert.NotContains(t, s, "golangci-lint")
	assert.NotContains(t, s, "mockery")
	assert.Contains(t, s, "gitlab.com/phpboyscout/go-tool-base/cmd/changelog")
	assert.Contains(t, s, "gitlab.com/phpboyscout/go-tool-base/cmd/docs")
	assert.Equal(t, []string{
		"gitlab.com/phpboyscout/go-tool-base/cmd/gtb",
		"github.com/golangci/golangci-lint/v2/cmd/golangci-lint",
		"github.com/vektra/mockery/v3",
	}, report.DroppedTools, "reported as the directive read, not as the pattern asked for")
}

// TestLockstepFloors (#87, spec 0200 D9): an owned adapter at the version
// this gtb knows is a floor, so a present line below it is raised; a module
// the generator does not own, and one it knows no version for, are left as
// D2 says.
func TestLockstepFloors(t *testing.T) {
	t.Parallel()

	reqs := LockstepFloors([]Requirement{
		{Path: "gitlab.com/phpboyscout/go/forge-gitlab", Version: "v0.24.0"},
		{Path: "gitlab.com/phpboyscout/go/chat-anthropic", Version: Latest},
		{Path: "github.com/acme/shared", Version: "v1.2.3"},
		{Path: "gitlab.com/phpboyscout/go/forge-github", Version: "v0.25.0", Floor: true},
	}, []string{"gitlab.com/phpboyscout/go/forge-gitlab", "gitlab.com/phpboyscout/go/chat-anthropic", "gitlab.com/phpboyscout/go/forge-github"})

	assert.Equal(t, []Requirement{
		{Path: "gitlab.com/phpboyscout/go/forge-gitlab", Version: "v0.24.0", Floor: true},
		{Path: "gitlab.com/phpboyscout/go/chat-anthropic", Version: Latest},
		{Path: "github.com/acme/shared", Version: "v1.2.3"},
		{Path: "gitlab.com/phpboyscout/go/forge-github", Version: "v0.25.0", Floor: true},
	}, reqs)

	out, report, err := Seed([]byte(settled), reqs, nil)
	require.NoError(t, err)
	assert.Contains(t, string(out), "gitlab.com/phpboyscout/go/forge-gitlab v0.24.0", "the owned line is raised")
	assert.Contains(t, string(out), "github.com/acme/shared v1.2.3", "a foreign line is left")
	assert.Equal(t, []Raise{
		{Path: "gitlab.com/phpboyscout/go/forge-gitlab", From: "v0.21.0", To: "v0.24.0"},
		{Path: "gitlab.com/phpboyscout/go/forge-github", From: "v0.22.0", To: "v0.25.0"},
	}, report.Raised, "a declared floor and a lockstep floor raise alike")
}

func TestSeed_DropsAnOrphanedLineOfItsOwn(t *testing.T) {
	t.Parallel()

	// gitlab was disabled: it is no longer wanted and is one of ours (Owned).
	out, report, err := Seed([]byte(settled), []Requirement{
		{Path: "gitlab.com/phpboyscout/go-tool-base", Version: "v0.42.0"},
		{Path: "gitlab.com/phpboyscout/go/forge-github", Version: "v0.22.0"},
	}, nil, Owned("gitlab.com/phpboyscout/go/forge-gitlab", "gitlab.com/phpboyscout/go/forge-github"))
	require.NoError(t, err)

	s := string(out)
	assert.NotContains(t, s, "forge-gitlab")
	assert.Contains(t, s, "github.com/acme/shared v1.2.3", "a foreign line is never dropped")
	assert.Equal(t, []string{"gitlab.com/phpboyscout/go/forge-gitlab"}, report.Dropped)
}

func TestSeed_CreatesAFileFromNothing(t *testing.T) {
	t.Parallel()

	out, _, err := Seed(nil, []Requirement{
		{Path: "gitlab.com/phpboyscout/go-tool-base", Version: "v0.42.0"},
	}, nil, WithModule("github.com/acme/mytool", "1.27.1"), WithTools("gitlab.com/phpboyscout/go-tool-base/cmd/changelog"))
	require.NoError(t, err)

	s := string(out)
	assert.Contains(t, s, "module github.com/acme/mytool\n")
	assert.Contains(t, s, "go 1.27.1\n")
	assert.Contains(t, s, "tool gitlab.com/phpboyscout/go-tool-base/cmd/changelog")
	assert.Contains(t, s, "require gitlab.com/phpboyscout/go-tool-base v0.42.0")
}

func TestSeed_ReplaceIsAddedAndDropped(t *testing.T) {
	t.Parallel()

	out, _, err := Seed([]byte(settled), nil, &Replace{Path: "gitlab.com/phpboyscout/go-tool-base", Dir: "/home/dev/go-tool-base"})
	require.NoError(t, err)
	assert.Contains(t, string(out), "replace gitlab.com/phpboyscout/go-tool-base => /home/dev/go-tool-base")
	assert.Contains(t, string(out), "GTB_FRAMEWORK_REPLACE", "the comment explains the line")

	out, _, err = Seed(out, nil, nil)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "replace ", "no replace wanted, the development one goes")
	assert.NotContains(t, string(out), "GTB_FRAMEWORK_REPLACE")
}

func TestSeed_RejectsAMalformedFile(t *testing.T) {
	t.Parallel()

	_, _, err := Seed([]byte("module\n\nrequire (\n"), nil, nil)
	require.Error(t, err)
}

func TestSeed_WritesNoLineForLatest(t *testing.T) {
	t.Parallel()

	out, report, err := Seed([]byte(settled), []Requirement{
		{Path: "gitlab.com/phpboyscout/go/chat-bedrock", Version: Latest},
	}, nil)
	require.NoError(t, err)

	assert.NotContains(t, string(out), "chat-bedrock", "a version nothing knows is tidy's to resolve")
	assert.Equal(t, []string{"gitlab.com/phpboyscout/go/chat-bedrock"}, report.Unpinned)
}

func TestSeed_LeavesADevelopersOwnReplaceAlone(t *testing.T) {
	t.Parallel()

	theirs := settled + "\nreplace github.com/acme/shared => ../shared\n"

	out, _, err := Seed([]byte(theirs), nil, nil)
	require.NoError(t, err)
	assert.Contains(t, string(out), "replace github.com/acme/shared => ../shared", "not ours: no comment, never dropped")
}

func TestSeed_RaisesAFloorOnTheDevelopersOwnLineToo(t *testing.T) {
	t.Parallel()

	// A floor is a compatibility claim about the module, whoever added the line.
	out, report, err := Seed([]byte(settled), []Requirement{
		{Path: "github.com/acme/shared", Version: "v1.3.0", Floor: true},
	}, nil)
	require.NoError(t, err)
	assert.Contains(t, string(out), "github.com/acme/shared v1.3.0")
	assert.Len(t, report.Raised, 1)
}

func TestSeed_SecondRunIsAFixedPoint(t *testing.T) {
	t.Parallel()

	want := []Requirement{
		{Path: "gitlab.com/phpboyscout/go-tool-base", Version: "v0.42.0"},
		{Path: "gitlab.com/phpboyscout/go/chat-anthropic", Version: "v0.14.1"},
	}
	rep := &Replace{Path: "gitlab.com/phpboyscout/go-tool-base", Dir: "/home/dev/gtb"}

	once, _, err := Seed([]byte(settled), want, rep, WithTools("gitlab.com/phpboyscout/go-tool-base/cmd/docs"))
	require.NoError(t, err)

	twice, report, err := Seed(once, want, rep, WithTools("gitlab.com/phpboyscout/go-tool-base/cmd/docs"))
	require.NoError(t, err)

	assert.Equal(t, string(once), string(twice))
	assert.Empty(t, report.Added)
	assert.Equal(t, 1, strings.Count(string(twice), "GTB_FRAMEWORK_REPLACE"), "the comment is written once")
}
