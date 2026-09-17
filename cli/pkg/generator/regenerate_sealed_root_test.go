package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

const handWrittenRoot = "package root\n\n// hand-written: SkipConfigCheck carries a value the manifest cannot express\nfunc NewCmdRoot(p interface{}) {}\n"

// TestRegenerateProject_SealedRootIsNotWritten (#32): `sealed` is documented
// as "never written, wiring included", and pkg/cmd/root/cmd.go is the one
// file a project cannot avoid editing. A sealed root survives a regenerate
// byte for byte, and the run says so once, truthfully.
func TestRegenerateProject_SealedRootIsNotWritten(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, fs, buf := newIssue13Project(t, "allow", "pkg/cmd/root/cmd.go sealed\n")
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/root/cmd.go", []byte(handWrittenRoot), 0o644))

	require.NoError(t, g.RegenerateProject(context.Background()))

	got, err := afero.ReadFile(fs, "/work/pkg/cmd/root/cmd.go")
	require.NoError(t, err)
	assert.Equal(t, handWrittenRoot, string(got), "the rule outranks everything, --overwrite allow included")

	assert.False(t, buf.ContainsLevel(logger.InfoLevel, "Regenerating root command"),
		"the run must not claim a write it did not make, got: %v", buf.Messages())
	assert.True(t, buf.ContainsLevel(logger.InfoLevel, "1 file sealed"), "the summary counts it once, got: %v", buf.Messages())
}

// TestRegenerateProject_IgnoredRootKeepsItsEditsAndIsWired (#32, 0188 D2): a
// plain rule stops the wholesale render but not the wiring, so a hand-written
// root keeps its edits and still registers the manifest's commands.
func TestRegenerateProject_IgnoredRootKeepsItsEditsAndIsWired(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, fs, buf := newIssue13Project(t, "allow", "pkg/cmd/root/cmd.go\n")
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/root/cmd.go", []byte(handWrittenRoot), 0o644))

	require.NoError(t, g.RegenerateProject(context.Background()))

	got, err := afero.ReadFile(fs, "/work/pkg/cmd/root/cmd.go")
	require.NoError(t, err)
	assert.Contains(t, string(got), "hand-written: SkipConfigCheck", "the render was skipped")
	assert.Contains(t, string(got), "cmd.Register(beta.NewCmdBeta(p))", "the wiring was not")
	assert.False(t, buf.ContainsLevel(logger.InfoLevel, "Regenerating root command"), "got: %v", buf.Messages())
}

// TestGenerateRegistrationFile_LogsWhatItDid (#32): "Writing registration
// file" appeared for a file an ignore rule then kept. The line now follows
// the decision.
func TestGenerateRegistrationFile_LogsWhatItDid(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, _, buf := newIssue13Project(t, "allow", "pkg/cmd/alpha/cmd.go sealed\n")

	require.NoError(t, g.RegenerateProject(context.Background()))

	assert.False(t, buf.ContainsLevel(logger.InfoLevel, "Writing registration file: /work/pkg/cmd/alpha/cmd.go"),
		"alpha was sealed and not written, got: %v", buf.Messages())
	assert.True(t, buf.ContainsLevel(logger.InfoLevel, "Writing registration file: /work/pkg/cmd/beta/cmd.go"),
		"beta was written, got: %v", buf.Messages())
}

// TestListIgnoreRules_RootCommandRuleIsNotStale (#32): the root command is
// generated but carries no hash, so a rule covering it matched no tracked
// file and was reported stale while it was the rule the project relied on.
func TestListIgnoreRules_RootCommandRuleIsNotStale(t *testing.T) {
	t.Parallel()

	g, _, _ := newIssue13Project(t, "ask", "pkg/cmd/root/cmd.go sealed\n")

	listing, err := g.ListIgnoreRules()
	require.NoError(t, err)

	assert.Empty(t, listing.StaleRules, "the root command is a generated file the rule covers")
}
