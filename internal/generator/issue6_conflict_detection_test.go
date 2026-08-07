package generator

// Tests backing GitLab issue #6:
//
//	"generate command/add-flag silently revert hand-edited project files;
//	 regenerate handles the same files correctly"
//
// These were written as an independent diagnosis of the report against the
// CURRENT main tree (post generator-followups). They split into:
//
//   - A GREEN guard (TestIssue6_DocPostProcessing_PreservesHandEditedIndexPages)
//     proving the PRIMARY report — how-to / concepts index pages reverted by the
//     generate subcommands' documentation post-processing — no longer reproduces:
//     the doc step only rewrites the manifest-derived CLI index, leaving the
//     skeleton-owned index pages untouched.
//
//   - Two RED tests (TestIssue6_CommandsIndex_* and TestIssue6_ConflictPrompt_*)
//     capturing the behaviour that IS still present on main. They are expected to
//     FAIL until the fix lands and MUST NOT be merged to a green branch.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// diataxisManifest is a minimal manifest with a single command so the doc
// post-processing has something to render into the CLI index.
const diataxisManifest = `properties:
  name: tool
  docs_layout: diataxis
commands:
  - name: deploy
    description: Deploy it
`

// logCapture is the subset of the buffer logger the issue #6 tests assert on.
type logCapture interface {
	logger.Logger
	Contains(substr string) bool
	String() string
}

func newIssue6Generator(t *testing.T, manifestYAML string) (*Generator, logCapture, string) {
	t.Helper()

	const root = "/work"

	fs := afero.NewMemMapFs()
	if manifestYAML != "" {
		require.NoError(t, fs.MkdirAll(filepath.Join(root, ".gtb"), 0o755))
		require.NoError(t, afero.WriteFile(fs, filepath.Join(root, ".gtb", "manifest.yaml"), []byte(manifestYAML), 0o644))
	}

	buf := logger.NewBuffer()
	g := &Generator{
		props:  &props.Props{FS: fs, Logger: buf},
		config: &Config{Path: root},
	}

	return g, buf, root
}

// TestIssue6_DocPostProcessing_PreservesHandEditedIndexPages is the GREEN guard
// for the primary report. The doc post-processing the generate subcommands run
// (generateCommandsIndex) must not touch the skeleton-owned Diátaxis index pages
// (docs/how-to/index.md, docs/explanation/concepts/index.md). A hand-added entry
// in each must survive. This passes on current main and locks in the fix.
func TestIssue6_DocPostProcessing_PreservesHandEditedIndexPages(t *testing.T) {
	t.Parallel()

	g, _, root := newIssue6Generator(t, diataxisManifest)

	howto := filepath.Join(root, "docs/how-to/index.md")
	concepts := filepath.Join(root, "docs/explanation/concepts/index.md")

	howtoContent := "# How-to guides\n\n- [My Added Guide](my-guide.md)\n"
	conceptsContent := "# Concepts\n\n- [My Added Concept](my-concept.md)\n"

	require.NoError(t, g.props.FS.MkdirAll(filepath.Dir(howto), 0o755))
	require.NoError(t, g.props.FS.MkdirAll(filepath.Dir(concepts), 0o755))
	require.NoError(t, afero.WriteFile(g.props.FS, howto, []byte(howtoContent), 0o644))
	require.NoError(t, afero.WriteFile(g.props.FS, concepts, []byte(conceptsContent), 0o644))

	// This is the docs step that a `generate command` / `add-flag` run performs.
	require.NoError(t, g.generateCommandsIndex())

	gotHowto, err := afero.ReadFile(g.props.FS, howto)
	require.NoError(t, err)
	assert.Equal(t, howtoContent, string(gotHowto),
		"docs/how-to/index.md must survive the generate subcommands' doc post-processing (issue #6 primary report)")

	gotConcepts, err := afero.ReadFile(g.props.FS, concepts)
	require.NoError(t, err)
	assert.Equal(t, conceptsContent, string(gotConcepts),
		"docs/explanation/concepts/index.md must survive the generate subcommands' doc post-processing (issue #6 primary report)")
}

// TestIssue6_CommandsIndex_ClobbersHandEditedIndex is a RED test for the ONE
// docs file the generate subcommands still rewrite destructively: the
// manifest-derived CLI index (docs/reference/cli/index.md). generateCommandsIndex
// performs an unconditional afero.WriteFile with no hash-conflict check and no
// .gtb/ignore consultation, so any hand-added prose is silently discarded — the
// residual, narrower instance of the issue #6 complaint about index-page
// rewriting. Expected to FAIL on main until the index writer preserves or merges
// manual content (see spec §4).
func TestIssue6_CommandsIndex_ClobbersHandEditedIndex(t *testing.T) {
	t.Parallel()

	g, _, root := newIssue6Generator(t, diataxisManifest)

	cliIndex := filepath.Join(root, "docs/reference/cli/index.md")
	handEdited := "# Commands\n\n> Hand-maintained intro paragraph a downstream added.\n\n| Command | Description |\n| :--- | :--- |\n"

	require.NoError(t, g.props.FS.MkdirAll(filepath.Dir(cliIndex), 0o755))
	require.NoError(t, afero.WriteFile(g.props.FS, cliIndex, []byte(handEdited), 0o644))

	require.NoError(t, g.generateCommandsIndex())

	got, err := afero.ReadFile(g.props.FS, cliIndex)
	require.NoError(t, err)

	assert.Contains(t, string(got), "Hand-maintained intro paragraph",
		"the CLI index writer must not silently discard hand-added content (issue #6 index-page sub-case)")
}

// TestIssue6_ConflictPrompt_NonInteractiveEmitsTTYNoise is the RED test for the
// SECOND bug. When a conflict is detected in a non-interactive environment
// (no controlling terminal, GTB_NON_INTERACTIVE unset), the conflict resolver
// invokes huh/bubbletea unconditionally. In a headless/CI environment the
// terminal open fails and it logs the stack-flavoured
// "Prompt failed (non-interactive?): ... open /dev/tty ..." warning per file
// before defaulting to skip; on a machine WITH a /dev/tty it would instead block
// on an interactive prompt. The desired behaviour is to detect non-interactive
// up front and skip cleanly with no TTY attempt.
//
// The prompt is driven in a goroutine with a timeout so the test is RED in both
// environments: headless -> "Prompt failed" is logged; TTY -> the call blocks and
// the timeout fires. It goes GREEN only once the resolver short-circuits before
// touching the terminal. Expected to FAIL on main.
func TestIssue6_ConflictPrompt_NonInteractiveEmitsTTYNoise(t *testing.T) {
	// Not parallel: mutates GTB_NON_INTERACTIVE via t.Setenv.
	t.Setenv("GTB_NON_INTERACTIVE", "") // explicitly NOT "true": force the ask path

	g, buf, root := newIssue6Generator(t, "")
	g.config.Overwrite = "" // default "ask"
	g.config.Force = false

	rel := ".pre-commit-config.yaml"
	full := filepath.Join(root, rel)
	require.NoError(t, afero.WriteFile(g.props.FS, full, []byte("on-disk hand-edited content\n"), 0o644))

	// Stored hash differs from on-disk content -> conflict.
	storedHashes := map[string]string{rel: calculateHash([]byte("original template content\n"))}

	done := make(chan struct{}, 1)

	go func() {
		g.resolveConflict(full, rel, storedHashes[rel], []byte("incoming template content\n"))
		done <- struct{}{}
	}()

	select {
	case <-done:
		// Resolver returned. On main it returned only because the huh prompt
		// failed opening the TTY, which is logged as noise. The desired fix
		// returns here WITHOUT any TTY attempt / noise.
		if buf.Contains("Prompt failed") {
			t.Fatalf("bug #6.2: conflict resolver attempted a TTY prompt in a non-interactive context and emitted noise:\n%s", buf.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("bug #6.2: conflict resolver blocked on an interactive prompt in a non-interactive context (no up-front non-interactive detection)")
	}
}
