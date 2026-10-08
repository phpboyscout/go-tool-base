package generator

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteBasicCommandDocs_FlagsComeFromTheSource (#33): the source is what
// the binary registers, so the table has a row per declared flag, carries
// the shorthand, and does not stop at what the manifest happens to list.
func TestWriteBasicCommandDocs_FlagsComeFromTheSource(t *testing.T) {
	t.Parallel()

	manifest := "properties:\n  name: mytool\ncommands:\n  - name: plan\n    description: Plan a release\n    flags:\n      - name: repo\n        type: string\n        default: \".\"\n        description: Path to the git repository to read\n"
	g := newPromptGenerator(t, manifest, false)

	src := `package plan

import (
	"github.com/spf13/cobra"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

type PlanOptions struct {
	Repo string
	Base string
}

func NewCmdPlan(props *props.Props) *setup.Command {
	opts := &PlanOptions{}
	cmd := setup.Wrap("plan", &cobra.Command{Use: "plan", Short: "Plan a release"})
	cmd.Flags().StringVarP(&opts.Repo, "repo", "C", ".", "Path to the git repository to read")
	cmd.Flags().StringVar(&opts.Base, "base", "main", "Branch the release lands on")
	_ = cmd.MarkFlagRequired("base")
	return cmd
}
`
	require.NoError(t, g.props.FS.MkdirAll("/work/pkg/cmd/plan", 0o755))
	require.NoError(t, afero.WriteFile(g.props.FS, "/work/pkg/cmd/plan/cmd.go", []byte(src), 0o644))

	out := filepath.Join("/work", "docs", "reference", "cli", "plan.md")
	require.NoError(t, g.writeBasicCommandDocs("plan", "mytool plan", "", out))

	data, err := afero.ReadFile(g.props.FS, out)
	require.NoError(t, err)
	s := string(data)

	assert.Contains(t, s, "| `-C, --repo` | Path to the git repository to read | `.` |  |", s)
	assert.Contains(t, s, "| `--base` | Branch the release lands on | `main` | Yes |", s)
}

// TestWriteBasicCommandDocs_ManifestFlagsWhenNoSource: a command the tree
// does not hold yet (generate command writes docs before the file lands on
// some paths) still gets its manifest flags, shorthand included.
func TestWriteBasicCommandDocs_ManifestFlagsWhenNoSource(t *testing.T) {
	t.Parallel()

	manifest := "properties:\n  name: mytool\ncommands:\n  - name: plan\n    flags:\n      - name: repo\n        shorthand: C\n        type: string\n        default: \".\"\n        description: Path to the git repository to read\n"
	g := newPromptGenerator(t, manifest, false)

	out := "/work/docs/reference/cli/plan.md"
	require.NoError(t, g.writeBasicCommandDocs("plan", "mytool plan", "", out))

	data, err := afero.ReadFile(g.props.FS, out)
	require.NoError(t, err)
	assert.Contains(t, string(data), "| `-C, --repo` | Path to the git repository to read | `.` |  |")
}

// Two commands share the leaf name status. The page for review status was
// written with index status's description, because the manifest was searched
// by name and index came first (#102).
func TestWriteBasicCommandDocs_ResolvesTheCommandByItsPath(t *testing.T) {
	t.Parallel()

	manifest := "properties:\n  name: mytool\ncommands:\n" +
		"  - name: index\n    description: Manage the index\n    commands:\n" +
		"      - name: status\n        description: Report what is indexed and how stale it is\n" +
		"  - name: review\n    description: Review answers\n    commands:\n" +
		"      - name: status\n        description: Report how many shadow answers have been reviewed\n"
	g := newPromptGenerator(t, manifest, false)

	out := "/work/docs/reference/cli/review/status.md"
	require.NoError(t, g.handleNoAIDocs("status", "mytool review status", "pkg/cmd/review/status", "example.com/mytool", out, false))

	page, err := afero.ReadFile(g.props.FS, out)
	require.NoError(t, err)
	assert.Contains(t, string(page), "Report how many shadow answers have been reviewed")
	assert.NotContains(t, string(page), "Report what is indexed")
}
