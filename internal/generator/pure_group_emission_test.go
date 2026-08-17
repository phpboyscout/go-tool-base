package generator

import (
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

func emissionFixture(t *testing.T, ignoreFile, mainContent string) (*Generator, string) {
	t.Helper()

	fs := afero.NewMemMapFs()
	root := "/work"
	cmdDir := filepath.Join(root, "pkg", "cmd", "lexicon")

	require.NoError(t, fs.MkdirAll(cmdDir, DefaultDirMode))
	require.NoError(t, fs.MkdirAll(filepath.Join(root, ".gtb"), DefaultDirMode))

	if ignoreFile != "" {
		require.NoError(t, afero.WriteFile(fs,
			filepath.Join(root, ".gtb", "ignore"), []byte(ignoreFile), DefaultFileMode))
	}

	if mainContent != "" {
		require.NoError(t, afero.WriteFile(fs,
			filepath.Join(cmdDir, "main.go"), []byte(mainContent), DefaultFileMode))
	}

	p := &props.Props{FS: fs, Logger: logger.NewNoop()}

	return New(p, &Config{Path: root, Name: "lexicon"}), cmdDir
}

func generateGroup(t *testing.T, g *Generator, cmdDir string) string {
	t.Helper()

	data := &templates.CommandData{
		Package:        "lexicon",
		Name:           "lexicon",
		PascalName:     "Lexicon",
		HasSubcommands: true,
	}

	require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, data))

	got, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "cmd.go"))
	require.NoError(t, err)

	return string(got)
}

// The regression that motivated spec 0190. A seal governs what the generator may
// WRITE; it has no business deciding what the built tool DOES.
//
// How it went wrong, in order. Issue #21 made every cmd.go wire
// `RunE: … Run<Name>(…)`. Issue #17 made a sealed path never created. Together
// they broke a real project: keryx had sealed pkg/cmd/voice/lexicon/main.go and
// deleted it — its ignore file says why, "its cmd.go wires no RunE" — so after
// regeneration cmd.go referenced a RunLexicon that could not be created:
//
//	pkg/cmd/voice/lexicon/cmd.go:28:11: undefined: RunLexicon (typecheck)
//
// The fix at the time was to suppress the RunE when main.go was sealed AND
// absent, which kept it compiling and made something worse true: the same pure
// group exited 2 on a bare invocation, or 0, according to whether .gtb/ignore
// happened to have sealed a file that was not there (issue #22). Nothing stated
// that, and nothing tested it.
//
// Every combination must now emit byte-identical cmd.go. That is what makes the
// seal unable to reach runtime behaviour, and it is why the suppression could
// only be deleted once emission stopped naming Run<Name>.
func TestPureGroupEmission_IsIndependentOfTheIgnoreFile(t *testing.T) {
	t.Parallel()

	const sealed = "pkg/cmd/lexicon/main.go sealed\n"

	matrix := map[string]struct {
		ignoreFile  string
		mainContent string
	}{
		"unsealed, main.go absent": {},
		"sealed, main.go absent":   {ignoreFile: sealed},
		"unsealed, untouched stub": {mainContent: untouchedGroupStub},
		"sealed, untouched stub":   {ignoreFile: sealed, mainContent: untouchedGroupStub},
		"unsealed, leaf-era stub":  {mainContent: untouchedLeafStub},
	}

	emitted := make(map[string]string, len(matrix))

	for name, tc := range matrix {
		g, cmdDir := emissionFixture(t, tc.ignoreFile, tc.mainContent)
		emitted[name] = generateGroup(t, g, cmdDir)
	}

	var first, firstName string
	for name, got := range emitted {
		if firstName == "" {
			first, firstName = got, name

			continue
		}

		assert.Equal(t, first, got,
			"%q and %q are the same command: the ignore file must not change what is emitted",
			firstName, name)
	}

	assert.Contains(t, first, "GroupRunE",
		"and what is emitted is the framework helper, not a call into main.go")
	assert.NotContains(t, first, "RunLexicon(",
		"nothing in the developer's own package is referenced, so nothing can dangle")
}

// A group with hand-written run logic is a WORKING group: the developer owns the
// outcome, and the wiring it has today stands.
func TestWorkingGroupEmission_KeepsItsWiring(t *testing.T) {
	t.Parallel()

	g, cmdDir := emissionFixture(t, "", developerWrittenRun)

	got := generateGroup(t, g, cmdDir)

	assert.Contains(t, got, "RunLexicon(", "a body expresses intent and is still called")
	assert.NotContains(t, got, "GroupRunE", "and the framework default does not override it")
}

// A leaf is unaffected: ErrNotImplemented remains the right answer for a command
// that exists but does nothing yet.
func TestLeafEmission_IsUnchanged(t *testing.T) {
	t.Parallel()

	g, cmdDir := emissionFixture(t, "", "")

	data := &templates.CommandData{
		Package:    "lexicon",
		Name:       "lexicon",
		PascalName: "Lexicon",
	}

	require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, data))

	cmd, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "cmd.go"))
	require.NoError(t, err)
	assert.Contains(t, string(cmd), "RunLexicon(")

	main, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "main.go"))
	require.NoError(t, err)
	assert.Contains(t, string(main), "ErrNotImplemented",
		"a leaf still gets a stub saying it is not implemented yet")
}

// D3: the leaf-to-group transition never edits or deletes main.go. The stub it
// leaves behind is a live seam — put a body in it and the group is classified
// working again on the next run — not litter to be tidied by a generator that
// does not own the file.
func TestLeafBecomingAGroup_LeavesMainGoByteIdentical(t *testing.T) {
	t.Parallel()

	g, cmdDir := emissionFixture(t, "", untouchedLeafStub)

	before, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "main.go"))
	require.NoError(t, err)

	generateGroup(t, g, cmdDir)

	after, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "main.go"))
	require.NoError(t, err)

	assert.Equal(t, string(before), string(after),
		"main.go belongs to the developer; the sentinel it names is no longer called")
}

// A pure group needs no main.go, so none is created — but a pure group that also
// asks for a pre-run hook does, because that hook has to live somewhere.
func TestPureGroup_MainGoIsCreatedOnlyForWhatItStillDefines(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		preRun     bool
		wantExists bool
	}{
		"no hooks: nothing to define":     {wantExists: false},
		"pre-run hook: something to hold": {preRun: true, wantExists: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g, cmdDir := emissionFixture(t, "", "")

			data := &templates.CommandData{
				Package:        "lexicon",
				Name:           "lexicon",
				PascalName:     "Lexicon",
				HasSubcommands: true,
				PreRun:         tc.preRun,
			}

			require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, data))

			exists, err := afero.Exists(g.props.FS, filepath.Join(cmdDir, "main.go"))
			require.NoError(t, err)
			assert.Equal(t, tc.wantExists, exists)

			if tc.wantExists {
				got, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "main.go"))
				require.NoError(t, err)
				assert.Contains(t, string(got), "PreRunLexicon")
				assert.NotContains(t, string(got), "func RunLexicon",
					"a pure group has no run function, even when it has hooks")
			}
		})
	}
}
