package generator

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
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

// A pure group's main.go holds what it asked for and no run function, and a
// group that asked for nothing has none (#235).
func TestPureGroup_MainFileHoldsWhatItAskedFor(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		data templates.CommandData
		want string
	}{
		"nothing":            {},
		"persistent pre-run": {data: templates.CommandData{PersistentPreRun: true}, want: "func PersistentPreRunLexicon("},
		"pre-run":            {data: templates.CommandData{PreRun: true}, want: "func PreRunLexicon("},
		"flags hook":         {data: templates.CommandData{FlagsHook: true}, want: "func FlagsLexicon("},
		"initialiser":        {data: templates.CommandData{WithInitializer: true}, want: "func InitLexicon("},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g, cmdDir := emissionFixture(t, "", "")
			data := tt.data
			data.Package, data.Name, data.PascalName, data.HasSubcommands = "lexicon", "lexicon", "Lexicon", true

			require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, &data))

			mainFile := filepath.Join(cmdDir, "main.go")

			if tt.want == "" {
				exists, err := afero.Exists(g.props.FS, mainFile)
				require.NoError(t, err)
				assert.False(t, exists)

				return
			}

			src, err := afero.ReadFile(g.props.FS, mainFile)
			require.NoError(t, err)
			assert.Contains(t, string(src), tt.want)
			assert.Equal(t, 1, strings.Count(string(src), "func "), "%s", src)
		})
	}
}

// A function another file of the package declares is left to it (#235).
func TestPureGroup_MainFileLeavesWhatTheAuthorWrote(t *testing.T) {
	t.Parallel()

	const initialiser = "package lexicon\n\nfunc InitLexicon() error { return nil }\n"

	for name, tt := range map[string]struct {
		file, src string
		left      bool
	}{
		"another file":             {"initialise.go", initialiser, true},
		"a kept init.go":           {"init.go", initialiser, true},
		"a file for this system":   {"main_" + runtime.GOOS + ".go", initialiser, true},
		"a file for another":       {"main_plan9.go", initialiser, false},
		"a file left out by tag":   {"other.go", "//go:build ignore\n\n" + initialiser, false},
		"an underscored file":      {"_old.go", initialiser, false},
		"a dotted file":            {".save.go", initialiser, false},
		"a directory":              {"old.go/initialise.go", initialiser, false},
		"a variable":               {"other.go", "package lexicon\n\nvar InitLexicon = func() error { return nil }\n", true},
		"a grouped variable":       {"other.go", "package lexicon\n\nvar (\n\ta int\n\tb, InitLexicon = 1, func() error { return nil }\n)\n", true},
		"a type":                   {"other.go", "package lexicon\n\ntype InitLexicon func() error\n", true},
		"a variable in a function": {"other.go", "package lexicon\n\nfunc f() {\n\tvar InitLexicon int\n\t_ = InitLexicon\n}\n", false},
		"an import alone":          {"other.go", "package lexicon\n\nimport _ \"embed\"\n", false},
		"not a Go file":            {"initialise.go.txt", initialiser, false},
		"a test file":              {"lexicon_test.go", initialiser, false},
		"another package":          {"gen.go", "//go:build ignore\n\npackage main\n\nfunc InitLexicon() {}\n", false},
		"package documentation":    {"doc.go", "package documentation\n\nfunc InitLexicon() {}\n", false},
		"assembly":                 {"init.s", "package lexicon\n", false},
		"a method":                 {"other.go", "package lexicon\n\ntype t struct{}\n\nfunc (t) InitLexicon() {}\n", false},
		"a file that won't parse":  {"other.go", "package lexicon\n\nfunc InitLexicon(\n", true},
		"a merge conflict":         {"other.go", "<<<<<<< HEAD\n" + initialiser, true},
		"a malformed build line":   {"other.go", "//go:build (\n\n" + initialiser, true},
		"assembly with no build":   {"init.s", "//go:build (\n", false},
		"a test that won't parse":  {"lexicon_test.go", "package lexicon\n\nfunc InitLexicon(\n", false},
		"a tagged-out bad file":    {"other.go", "//go:build ignore\n\npackage lexicon\n\nfunc (\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g, cmdDir := emissionFixture(t, "", "")
			g.config.Overwrite = "deny"
			require.NoError(t, afero.WriteFile(g.props.FS, filepath.Join(cmdDir, tt.file), []byte(tt.src), DefaultFileMode))
			// Files either side of it in the listing, so neither stops the scan.
			for _, beside := range []string{".aaa.go", "zzz.go"} {
				require.NoError(t, afero.WriteFile(g.props.FS, filepath.Join(cmdDir, beside), []byte("package lexicon\n"), DefaultFileMode))
			}

			data := templates.CommandData{
				Package: "lexicon", Name: "lexicon", PascalName: "Lexicon", HasSubcommands: true, WithInitializer: true,
			}
			require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, &data))

			exists, err := afero.Exists(g.props.FS, filepath.Join(cmdDir, "main.go"))
			require.NoError(t, err)
			assert.Equal(t, !tt.left, exists)
			assert.True(t, data.WithInitializer, "init.go and the manifest still have it")
		})
	}
}

// Every function the generator would write to main.go is left to the file that
// has it, and the others are still written.
func TestMainFile_LeavesEachFunctionAnotherFileHas(t *testing.T) {
	t.Parallel()

	for _, left := range []string{"PersistentPreRunLexicon", "PreRunLexicon", "FlagsLexicon", "InitLexicon"} {
		const runOnly = "package lexicon\n\nfunc RunLexicon() {}\n"

		for name, tt := range map[string]struct{ existing, written string }{
			"new": {}, "kept": {existing: runOnly}, "written whole": {written: runOnly},
		} {
			t.Run(left+" "+name, func(t *testing.T) {
				t.Parallel()

				g, cmdDir := emissionFixture(t, "", "")
				mainFile := filepath.Join(cmdDir, "main.go")

				require.NoError(t, afero.WriteFile(g.props.FS, filepath.Join(cmdDir, "hooks.go"), []byte("package lexicon\n\nfunc "+left+"() {}\n"), DefaultFileMode))

				if tt.existing != "" {
					require.NoError(t, afero.WriteFile(g.props.FS, mainFile, []byte(tt.existing), DefaultFileMode))
				}

				data := templates.CommandData{
					Package: "lexicon", Name: "lexicon", PascalName: "Lexicon",
					PersistentPreRun: true, PreRun: true, FlagsHook: true, WithInitializer: true,
					FullFileContent: tt.written,
				}
				require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, &data))

				src, err := afero.ReadFile(g.props.FS, mainFile)
				require.NoError(t, err)

				for _, fn := range []string{"RunLexicon", "PersistentPreRunLexicon", "PreRunLexicon", "FlagsLexicon", "InitLexicon"} {
					want := 1
					if fn == left {
						want = 0
					}

					assert.Equal(t, want, strings.Count(string(src), "func "+fn+"("), fn)
				}
			})
		}
	}
}

// main.go is not another file: --force writes it whole, and its own functions
// are not left out.
func TestMainFile_ForcedKeepsItsOwnFunctions(t *testing.T) {
	t.Parallel()

	g, cmdDir := emissionFixture(t, "", "package lexicon\n\nfunc InitLexicon() {}\n")
	g.config.Force = true

	data := templates.CommandData{Package: "lexicon", Name: "lexicon", PascalName: "Lexicon", WithInitializer: true}
	require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, &data))

	src, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "main.go"))
	require.NoError(t, err)
	assert.Contains(t, string(src), "func InitLexicon(ctx")
}

// openRefusedFs refuses to open one path, as a dangling link or a file without
// read permission does.
type openRefusedFs struct {
	afero.Fs
	path string
}

func (f openRefusedFs) Open(name string) (afero.File, error) {
	if name == f.path {
		return nil, os.ErrPermission
	}

	return f.Fs.Open(name)
}

func TestMainFile_AFileItCannotReadDeclaresNothing(t *testing.T) {
	t.Parallel()

	for _, name := range []string{".#main.go", "locked.go", "."} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g, cmdDir := emissionFixture(t, "", "")
			locked := filepath.Join(cmdDir, name)
			if locked != cmdDir {
				require.NoError(t, afero.WriteFile(g.props.FS, locked, []byte("package lexicon\n"), DefaultFileMode))
			}

			g.props.FS = openRefusedFs{Fs: g.props.FS, path: locked}

			data := templates.CommandData{Package: "lexicon", Name: "lexicon", PascalName: "Lexicon", PreRun: true}
			require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, &data))

			src, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "main.go"))
			require.NoError(t, err)
			assert.Contains(t, string(src), "func PreRunLexicon(")
		})
	}
}

// An unreadable file may declare Init<Name>, so a new main.go leaves it out and
// says so. One that exists is given it, as before (#240).
func TestMainFile_LeavesInitToAFileItCannotRead(t *testing.T) {
	t.Parallel()

	const (
		runOnly = "package lexicon\n\nfunc RunLexicon() {}\n"
		// What the parser recovers before the conflict does not count.
		conflicted = "package lexicon\n\nfunc InitLexicon() {}\n\nfunc PreRunLexicon() {}\n\n<<<<<<< HEAD\n"
	)

	for name, tt := range map[string]struct {
		existing, beside string
		readable         string
		locked, force    bool
		wantInit         bool
		wantWarned       string
	}{
		"does not parse":           {beside: "func (", wantWarned: "pkg/cmd/lexicon/other.go"},
		"declares it, then breaks": {beside: conflicted, wantWarned: "pkg/cmd/lexicon/other.go"},
		"the same, main.go kept":   {existing: runOnly, beside: conflicted, wantInit: true},
		"the same, main.go forced": {existing: runOnly, beside: conflicted, force: true, wantInit: true},
		"cannot be opened":         {beside: "package lexicon\n", locked: true, wantWarned: "pkg/cmd/lexicon/other.go"},
		"parses":                   {beside: "package lexicon\n", wantInit: true},
		"main.go kept":             {existing: runOnly, beside: "func (", wantInit: true},
		"main.go forced":           {existing: runOnly, beside: "func (", force: true, wantInit: true},
		"another file declares it": {beside: "func (", readable: "package lexicon\n\nfunc InitLexicon() {}\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g, cmdDir := emissionFixture(t, "", tt.existing)
			log := logger.NewBuffer()
			g.props.Logger = log
			g.config.Force = tt.force

			other := filepath.Join(cmdDir, "other.go")
			require.NoError(t, afero.WriteFile(g.props.FS, other, []byte(tt.beside), DefaultFileMode))

			if tt.readable != "" {
				require.NoError(t, afero.WriteFile(g.props.FS, filepath.Join(cmdDir, "readable.go"), []byte(tt.readable), DefaultFileMode))
			}

			if tt.locked {
				g.props.FS = openRefusedFs{Fs: g.props.FS, path: other}
			}

			data := templates.CommandData{Package: "lexicon", Name: "lexicon", PascalName: "Lexicon", PreRun: true, WithInitializer: true}
			require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, &data))
			assert.True(t, data.WithInitializer, "init.go and the manifest still have it")

			src, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "main.go"))
			require.NoError(t, err)
			assert.Contains(t, string(src), "func PreRunLexicon(")
			assert.Equal(t, tt.wantInit, strings.Contains(string(src), "func InitLexicon("))

			warned := warnings(t, log)
			if tt.wantWarned == "" {
				assert.Empty(t, warned)

				return
			}

			require.Len(t, warned, 1)
			assert.Contains(t, warned[0], "InitLexicon")
			assert.Contains(t, warned[0], tt.wantWarned)
		})
	}
}

// Fixing the file and regenerating settles it: the function is written once.
func TestMainFile_HoldsInitOnceTheFileIsReadable(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		fixed string
		want  bool
	}{
		"declares it": {"package lexicon\n\nfunc InitLexicon() {}\n", false},
		"does not":    {"package lexicon\n", true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g, cmdDir := emissionFixture(t, "", "")
			g.config.Overwrite = "allow"
			other, mainFile := filepath.Join(cmdDir, "other.go"), filepath.Join(cmdDir, "main.go")

			for _, src := range []string{"func (", tt.fixed} {
				require.NoError(t, afero.WriteFile(g.props.FS, other, []byte(src), DefaultFileMode))

				data := templates.CommandData{
					Package: "lexicon", Name: "lexicon", PascalName: "Lexicon", HasSubcommands: true, WithInitializer: true,
				}
				require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, &data))
			}

			exists, err := afero.Exists(g.props.FS, mainFile)
			require.NoError(t, err)
			assert.Equal(t, tt.want, exists)
		})
	}
}

// An init.go the run replaces no longer declares what it did.
func TestMainFile_HoldsWhatAReplacedInitFileDeclared(t *testing.T) {
	t.Parallel()

	g, cmdDir := emissionFixture(t, "", "")
	g.config.Overwrite = "allow"
	require.NoError(t, afero.WriteFile(g.props.FS, filepath.Join(cmdDir, "init.go"), []byte("package lexicon\n\nfunc InitLexicon() {}\n"), DefaultFileMode))

	data := templates.CommandData{Package: "lexicon", Name: "lexicon", PascalName: "Lexicon", WithInitializer: true}
	require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, &data))

	src, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "main.go"))
	require.NoError(t, err)
	assert.Contains(t, string(src), "func InitLexicon(ctx")
}
