package generator

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

const (
	flagsHookPath = "/work"
	flagsHookCall = "FlagsSign(props, opts.FlagSet)"
	flagsHookFunc = "func FlagsSign(props *props.Props, flags *pflag.FlagSet) {"
)

func flagsHookProject(t *testing.T) *props.Props {
	t.Helper()

	p := setupTestProject(t, flagsHookPath)
	generateFlagsHookCmd(t, p, Config{Name: "sign", FlagsHook: true})

	return p
}

func generateFlagsHookCmd(t *testing.T, p *props.Props, cfg Config) {
	t.Helper()

	require.NoError(t, tryGenerateFlagsHookCmd(p, cfg))
}

func tryGenerateFlagsHookCmd(p *props.Props, cfg Config) error {
	cfg.Path = flagsHookPath
	cfg.Short = cfg.Name + " command"

	if cfg.Parent == "" {
		cfg.Parent = "root"
	}

	return New(p, &cfg).Generate(context.Background())
}

func readFlagsHookFile(t *testing.T, p *props.Props, rel string) string {
	t.Helper()

	src, err := afero.ReadFile(p.FS, filepath.Join(flagsHookPath, "pkg", "cmd", rel))
	require.NoError(t, err)

	return string(src)
}

func loadFlagsHookManifest(t *testing.T, p *props.Props) *Manifest {
	t.Helper()

	m, err := New(p, &Config{Path: flagsHookPath}).loadManifest()
	require.NoError(t, err)

	return m
}

func TestFlagsHook_IsGeneratedAndRecorded(t *testing.T) {
	t.Parallel()

	p := flagsHookProject(t)

	assert.Contains(t, readFlagsHookFile(t, p, "sign/cmd.go"), flagsHookCall)
	assert.Contains(t, readFlagsHookFile(t, p, "sign/main.go"), flagsHookFunc)

	recorded := walkCommandPath(loadFlagsHookManifest(t, p).Commands, []string{"sign"})
	require.NotNil(t, recorded)
	assert.True(t, recorded.FlagsHook)
}

func TestFlagsHook_IsRecordedForANestedCommand(t *testing.T) {
	t.Parallel()

	p := setupTestProject(t, flagsHookPath)
	generateFlagsHookCmd(t, p, Config{Name: "keys"})
	generateFlagsHookCmd(t, p, Config{Name: "sign", Parent: "keys"})
	generateFlagsHookCmd(t, p, Config{Name: "sign", Parent: "keys", FlagsHook: true})

	recorded := walkCommandPath(loadFlagsHookManifest(t, p).Commands, []string{"keys", "sign"})
	require.NotNil(t, recorded)
	assert.True(t, recorded.FlagsHook)
	assert.Contains(t, readFlagsHookFile(t, p, "keys/sign/cmd.go"), flagsHookCall)
}

// Leaving the option out is how it is turned off, as it is for --pre-run. The
// hand-written hook stays, because main.go is the author's.
func TestFlagsHook_LeftOutOfASecondGenerateTurnsItOff(t *testing.T) {
	t.Parallel()

	p := flagsHookProject(t)
	generateFlagsHookCmd(t, p, Config{Name: "sign"})

	assert.NotContains(t, readFlagsHookFile(t, p, "sign/cmd.go"), "FlagSet")
	assert.Contains(t, readFlagsHookFile(t, p, "sign/main.go"), flagsHookFunc)
	assert.False(t, walkCommandPath(loadFlagsHookManifest(t, p).Commands, []string{"sign"}).FlagsHook)
}

func TestFlagsHook_SurvivesRegenerateProject(t *testing.T) {
	t.Parallel()

	p := flagsHookProject(t)
	generateFlagsHookCmd(t, p, Config{Name: "keys", Parent: "sign"})

	before := readFlagsHookFile(t, p, "sign/main.go")

	g := New(p, &Config{Path: flagsHookPath})
	g.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil }
	require.NoError(t, g.RegenerateProject(context.Background()))

	assert.Contains(t, readFlagsHookFile(t, p, "sign/cmd.go"), flagsHookCall)
	assert.Equal(t, before, readFlagsHookFile(t, p, "sign/main.go"), "a hook already there is not added again")
	assert.True(t, walkCommandPath(loadFlagsHookManifest(t, p).Commands, []string{"sign"}).FlagsHook)
}

func TestFlagsHook_IsReadBackByRegenerateManifest(t *testing.T) {
	t.Parallel()

	p := flagsHookProject(t)
	require.NoError(t, p.FS.Remove(ManifestPathFor(flagsHookPath)))
	require.NoError(t, New(p, &Config{Path: flagsHookPath}).RegenerateManifest(context.Background()))

	recorded := walkCommandPath(loadFlagsHookManifest(t, p).Commands, []string{"sign"})
	require.NotNil(t, recorded)
	assert.True(t, recorded.FlagsHook)
}

// D9: only the command's own Flags<Name> turns the switch on.
func TestExtractCommandMetadata_FlagsHook(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		call string
		want bool
	}{
		"the command's own hook":       {call: "FlagsDeploy(props, opts.FlagSet)", want: true},
		"another Flags function":       {call: "FlagsShared(props, cmd.Flags())"},
		"the hook's name as a prefix":  {call: "FlagsDeployExtra(props, cmd.Flags())"},
		"the same name in a package":   {call: "shared.FlagsDeploy(props, cmd.Flags())"},
		"the same name on a call":      {call: "shared.New().FlagsDeploy(props, cmd.Flags())"},
		"no call in the constructor":   {call: "_ = props"},
		"a helper the author wrote in": {call: "addFlags(cmd.Flags())"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g, fs := newCoverageGenerator(t)
			path := writeCmd(t, fs, "deploy", "package deploy\n\nfunc NewCmdDeploy(props *props.Props) *setup.Command {\n"+
				"\tcmd := setup.Wrap(\"deploy\", &cobra.Command{Use: \"deploy\"})\n\t"+tc.call+"\n\treturn cmd\n}\n")

			cmd, _, _, err := g.extractCommandMetadata(path)
			require.NoError(t, err)
			assert.Equal(t, tc.want, cmd.FlagsHook)
		})
	}
}

func TestFlagsHook_PreservedMainGainsTheStubOnce(t *testing.T) {
	t.Parallel()

	for name, main := range map[string]string{
		"grouped imports": "package sign\n\nimport (\n\t\"context\"\n)\n\nfunc RunSign(_ context.Context) {}\n",
		"single import":   "package sign // doc\n\nimport \"context\"\n\nfunc RunSign(_ context.Context) {}\n",
		"no imports":      "// Package sign signs.\npackage sign\n\nfunc RunSign() {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g, cmdDir := emissionFixture(t, "", main)
			data := templates.CommandData{Package: "sign", Name: "sign", PascalName: "Sign", FlagsHook: true}

			for range 2 {
				require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, &data))
			}

			src, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "main.go"))
			require.NoError(t, err)
			assert.Equal(t, 1, strings.Count(string(src), flagsHookFunc), string(src))
			assert.Equal(t, 1, strings.Count(string(src), `"github.com/spf13/pflag"`), string(src))
			assert.Equal(t, 1, strings.Count(string(src), `"gitlab.com/phpboyscout/go-tool-base/pkg/props"`), string(src))
			_, err = parser.ParseFile(token.NewFileSet(), "main.go", src, parser.ParseComments)
			require.NoError(t, err, string(src))
		})
	}
}

// The stubs name a package by the last element of its path, so only an import
// under that name will do.
func TestEnsureImport(t *testing.T) {
	t.Parallel()

	const body = "\n\nfunc F(_ *pflag.FlagSet) {}\n"

	for name, tc := range map[string]struct {
		src  string
		want string
	}{
		"already there":             {src: "import \"github.com/spf13/pflag\"", want: "import \"github.com/spf13/pflag\""},
		"already there, named":      {src: "import pflag \"github.com/spf13/pflag\"", want: "import pflag \"github.com/spf13/pflag\""},
		"already there, backquoted": {src: "import `github.com/spf13/pflag`", want: "import `github.com/spf13/pflag`"},
		"aliased":                   {src: "import pf \"github.com/spf13/pflag\""},
		"blank":                     {src: "import (\n\t_ \"github.com/spf13/pflag\"\n)"},
		"dot":                       {src: "import . \"github.com/spf13/pflag\""},
		"the path in a string":      {src: "import \"fmt\"\n\nvar s = \"github.com/spf13/pflag\""},
		"import ( in the doc":       {src: "/* import (\n) */"},
		"an empty block":            {src: "import ()"},
		"an empty block, two lines": {src: "import (\n)"},
		"a one-line block":          {src: "import (\"fmt\")"},
		"a paren in a comment":      {src: "import (\n\t\"fmt\" // x\n\t// )\n)"},
		"a name that only contains": {src: "import \"github.com/spf13/pflag/v2\""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			src := "package sign\n\n" + tc.src + body
			got := ensureImport(src, "github.com/spf13/pflag")

			if tc.want != "" {
				assert.Equal(t, "package sign\n\n"+tc.want+body, got)

				return
			}

			f, err := parser.ParseFile(token.NewFileSet(), "main.go", got, parser.ParseComments)
			require.NoError(t, err, got)

			plain := 0

			for _, imp := range f.Imports {
				if imp.Name == nil && imp.Path.Value == `"github.com/spf13/pflag"` {
					plain++
				}
			}

			assert.Equal(t, 1, plain, got)
			assert.Equal(t, got, ensureImport(got, "github.com/spf13/pflag"), "adds it once")
		})
	}
}

func TestEnsureImport_KeepsACommentOnThePackageClause(t *testing.T) {
	t.Parallel()

	got := ensureImport("package sign //nolint:revive // why\n\nfunc F() {}\n", "context")
	assert.Equal(t, "package sign //nolint:revive // why\n\nimport (\n\t\"context\"\n)\n\nfunc F() {}\n", got)
}

func TestEnsureImport_PutsTheBlockAfterTheClausesOwnComments(t *testing.T) {
	t.Parallel()

	const block = "\n\nimport (\n\t\"context\"\n)"

	for name, tc := range map[string]struct{ before, after string }{
		"a comment lines below":            {"package sign", "\n\n// F does it.\nfunc F() {}\n"},
		"a block comment over lines":       {"package sign /* a\nb */", "\n\nfunc F() {}\n"},
		"a block comment over lines, CRLF": {"package sign /* a\r\nb\r\nc */", "\r\n\r\nfunc F() {}\r\n"},
		"a line comment, CRLF":             {"package sign // a", "\r\n\r\n/* b */\r\nfunc F() {}\r\n"},
		"two comments":                     {"package sign /* a */ // b", "\n\nfunc F() {}\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := ensureImport(tc.before+tc.after, "context")
			assert.Equal(t, tc.before+block+tc.after, got)

			_, err := parser.ParseFile(token.NewFileSet(), "main.go", got, parser.ParseComments)
			require.NoError(t, err)
		})
	}
}

func TestEnsureImport_IgnoresACommentBeforeThePackageClause(t *testing.T) {
	t.Parallel()

	got := ensureImport("/* doc */ package sign\n", "context")
	assert.Equal(t, "/* doc */ package sign\n\nimport (\n\t\"context\"\n)\n", got)
}

func TestEnsureImport_LeavesAFileItCannotRead(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "not go", ensureImport("not go", "context"))
}

func TestEnsureHookStubs_EveryStubBringsItsImports(t *testing.T) {
	t.Parallel()

	const setupImport = "gitlab.com/phpboyscout/go-tool-base/pkg/setup"

	for name, tc := range map[string]struct {
		data templates.CommandData
		want []string
	}{
		"run":                {templates.CommandData{}, []string{contextImport, propsImport, "gitlab.com/phpboyscout/go/errorhandling"}},
		"persistent pre-run": {templates.CommandData{PureGroup: true, PersistentPreRun: true}, []string{contextImport, propsImport}},
		"pre-run":            {templates.CommandData{PureGroup: true, PreRun: true}, []string{contextImport, propsImport}},
		"flags":              {templates.CommandData{PureGroup: true, FlagsHook: true}, []string{propsImport, "github.com/spf13/pflag"}},
		"initialiser":        {templates.CommandData{PureGroup: true, WithInitializer: true}, []string{contextImport, propsImport, setupImport}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g, cmdDir := emissionFixture(t, "", "package sign\n")
			mainFile := filepath.Join(cmdDir, "main.go")
			data := tc.data
			data.Package, data.Name, data.PascalName = "sign", "sign", "Sign"

			require.NoError(t, g.ensureHookStubs(context.Background(), mainFile, data))

			src, err := afero.ReadFile(g.props.FS, mainFile)
			require.NoError(t, err)

			f, err := parser.ParseFile(token.NewFileSet(), "main.go", src, parser.ImportsOnly)
			require.NoError(t, err)

			var got []string
			for _, imp := range f.Imports {
				got = append(got, strings.Trim(imp.Path.Value, `"`))
			}

			assert.ElementsMatch(t, tc.want, got)
		})
	}
}

// A hook-only main.go imports no context, so a later stub that needs one has
// to bring it.
func TestEnsureHookStubs_AStubBringsItsOwnImports(t *testing.T) {
	t.Parallel()

	g, cmdDir := emissionFixture(t, "", "package sign\n\nimport (\n\t\"github.com/spf13/pflag\"\n)\n\nvar _ *pflag.FlagSet\n")
	mainFile := filepath.Join(cmdDir, "main.go")

	require.NoError(t, g.ensureHookStubs(context.Background(), mainFile,
		templates.CommandData{Package: "sign", Name: "sign", PascalName: "Sign"}))

	src, err := afero.ReadFile(g.props.FS, mainFile)
	require.NoError(t, err)
	assert.Contains(t, string(src), `"context"`)
	assert.Contains(t, string(src), `"gitlab.com/phpboyscout/go-tool-base/pkg/props"`)
}

func TestFlagsHook_SealedMainIsLeftAlone(t *testing.T) {
	t.Parallel()

	const main = "package sign\n\nfunc RunSign() {}\n"

	g, cmdDir := emissionFixture(t, "pkg/cmd/lexicon/main.go sealed\n", main)

	require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir,
		&templates.CommandData{Package: "sign", Name: "sign", PascalName: "Sign", FlagsHook: true}))

	src, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "main.go"))
	require.NoError(t, err)
	assert.Equal(t, main, string(src))
	assert.Contains(t, g.conflicts.sealed, "pkg/cmd/lexicon/main.go")
}

// AI-written content defines Run<Name> alone, and cmd.go calls the hook too.
func TestFlagsHook_AIWrittenMainGainsTheStub(t *testing.T) {
	t.Parallel()

	g, cmdDir := emissionFixture(t, "", "")

	require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, &templates.CommandData{
		Package: "sign", Name: "sign", PascalName: "Sign", FlagsHook: true,
		FullFileContent: "package sign\n\nimport (\n\t\"context\"\n)\n\nfunc RunSign(_ context.Context) {}\n",
	}))

	src, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "main.go"))
	require.NoError(t, err)
	assert.Contains(t, string(src), "func RunSign(_ context.Context) {}")
	assert.Contains(t, string(src), flagsHookFunc)
}

// The repair agent sees a misnamed Run only while it fails to compile.
func TestFlagsHook_AIWrittenMainIsNotGivenARun(t *testing.T) {
	t.Parallel()

	g, cmdDir := emissionFixture(t, "", "")

	require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, &templates.CommandData{
		Package: "sign", Name: "sign", PascalName: "Sign",
		FullFileContent: "package sign\n\nfunc RunSignn() {}\n",
	}))

	src, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "main.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(src), "ErrNotImplemented")
}

// Without force the AI content is dropped and main.go is preserved, so Run is
// still owed to cmd.go.
func TestFlagsHook_PreservedMainGainsARunWhenAIContentIsDropped(t *testing.T) {
	t.Parallel()

	g, cmdDir := emissionFixture(t, "", "package sign\n")

	require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, &templates.CommandData{
		Package: "sign", Name: "sign", PascalName: "Sign",
		FullFileContent: "package sign\n\nfunc RunSign() {}\n",
	}))

	src, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "main.go"))
	require.NoError(t, err)
	assert.Contains(t, string(src), "errorhandling.ErrNotImplemented")
}

// D6: the hook survives its command becoming a group with no run logic.
func TestFlagsHook_SurvivesBecomingAPureGroup(t *testing.T) {
	t.Parallel()

	p := flagsHookProject(t)
	generateFlagsHookCmd(t, p, Config{Name: "keys", Parent: "sign"})

	src := readFlagsHookFile(t, p, "sign/cmd.go")
	assert.Contains(t, src, "setup.GroupRunE")
	assert.Contains(t, src, "opts := &SignOptions{}")
	assert.Contains(t, src, flagsHookCall)
	assert.Contains(t, src, "keys.NewCmdKeys(props)")
}

// A pure group writes no main.go unless a hook needs one.
func TestFlagsHook_PureGroupWritesMainForTheHook(t *testing.T) {
	t.Parallel()

	g, cmdDir := emissionFixture(t, "", "")

	require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, &templates.CommandData{
		Package: "sign", Name: "sign", PascalName: "Sign", HasSubcommands: true, FlagsHook: true,
	}))

	src, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "main.go"))
	require.NoError(t, err)
	assert.Contains(t, string(src), flagsHookFunc)
	assert.NotContains(t, string(src), `"context"`)
}

// D4: a flag stored in the field the hook adds is refused before anything is
// written, wherever it is declared.
func TestFlagsHook_RefusesAFlagThatTakesItsField(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		setup []Config
		cfg   Config
		want  []string
	}{
		"its own":              {cfg: Config{Name: "sign", FlagsHook: true, Flags: []string{"flag-set:string:x"}}, want: []string{"sign", "flag-set"}},
		"its own, another way": {cfg: Config{Name: "sign", FlagsHook: true, Flags: []string{"flag--set:string:x"}}, want: []string{"sign", "flag--set"}},
		"its own, persistent":  {cfg: Config{Name: "sign", FlagsHook: true, Flags: []string{"flag-set:string:x:true"}}, want: []string{"sign", "flag-set"}},
		"its own, recorded earlier": {
			setup: []Config{{Name: "sign", Flags: []string{"flag-set:string:x"}}},
			cfg:   Config{Name: "sign", FlagsHook: true},
			want:  []string{"sign", "flag-set"},
		},
		"inherited": {
			setup: []Config{{Name: "keys", Flags: []string{"flag-set:string:x:true"}}, {Name: "mint", Parent: "keys"}},
			cfg:   Config{Name: "sign", Parent: "keys/mint", FlagsHook: true},
			want:  []string{"keys/mint/sign", "command keys ", "flag-set"},
		},
		"inherited from its parent": {
			setup: []Config{{Name: "keys", Flags: []string{"flag-set:string:x:true"}}},
			cfg:   Config{Name: "sign", Parent: "keys", FlagsHook: true},
			want:  []string{"keys/sign", "command keys ", "flag-set"},
		},
		"inherited from a middle ancestor": {
			setup: []Config{
				{Name: "keys"}, {Name: "mint", Parent: "keys", Flags: []string{"flag-set:string:x:true"}}, {Name: "box", Parent: "keys/mint"},
			},
			cfg:  Config{Name: "sign", Parent: "keys/mint/box", FlagsHook: true},
			want: []string{"keys/mint/box/sign", "command keys/mint ", "flag-set"},
		},
		"added to an ancestor later": {
			setup: []Config{{Name: "keys"}, {Name: "mint", Parent: "keys"}, {Name: "sign", Parent: "keys/mint", FlagsHook: true}},
			cfg:   Config{Name: "keys", Flags: []string{"flag-set:string:x:true"}},
			want:  []string{"keys/mint/sign", "command keys ", "flag-set"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := setupTestProject(t, flagsHookPath)
			for _, c := range tc.setup {
				generateFlagsHookCmd(t, p, c)
			}

			before := snapshotFS(t, p.FS)

			for _, dryRun := range []bool{true, false} {
				cfg := tc.cfg
				cfg.DryRun = dryRun
				err := tryGenerateFlagsHookCmd(p, cfg)
				require.ErrorIs(t, err, ErrInvalidInput)

				for _, want := range tc.want {
					assert.Contains(t, strings.Join(errors.GetAllHints(err), "\n"), want)
				}

				assert.Equal(t, before, snapshotFS(t, p.FS), "refused before anything is written")
			}
		})
	}
}

// A hand-edited manifest can hold an ancestor that is refused itself. A command
// under it must not be written before that is found.
func TestFlagsHook_RefusesACommandUnderARefusedAncestor(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		flag, parent, refused string
	}{
		"the parent's own flag":      {flag: "flag-set:string:x", parent: "keys", refused: "keys"},
		"a flag the parent inherits": {flag: "flag-set:string:x:true", parent: "keys/mint", refused: "keys/mint"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := setupTestProject(t, flagsHookPath)
			generateFlagsHookCmd(t, p, Config{Name: "keys", Flags: []string{tc.flag}})
			generateFlagsHookCmd(t, p, Config{Name: "mint", Parent: "keys"})
			turnOnFlagsHookByHand(t, p, tc.refused)

			before := snapshotFS(t, p.FS)

			for _, dryRun := range []bool{true, false} {
				err := tryGenerateFlagsHookCmd(p, Config{Name: "sign", Parent: tc.parent, DryRun: dryRun})
				require.ErrorIs(t, err, ErrInvalidInput)
				assert.Contains(t, strings.Join(errors.GetAllHints(err), "\n"), "command "+tc.refused+" has a flags hook")
				assert.Equal(t, before, snapshotFS(t, p.FS))
			}
		})
	}
}

// A command that is refused itself does not stop its siblings being written.
func TestFlagsHook_ARefusedSiblingIsLeftAlone(t *testing.T) {
	t.Parallel()

	p := setupTestProject(t, flagsHookPath)
	generateFlagsHookCmd(t, p, Config{Name: "keys"})
	generateFlagsHookCmd(t, p, Config{Name: "mint", Parent: "keys", Flags: []string{"flag-set:string:x"}})
	turnOnFlagsHookByHand(t, p, "keys/mint")

	generateFlagsHookCmd(t, p, Config{Name: "sign", Parent: "keys"})
}

func turnOnFlagsHookByHand(t *testing.T, p *props.Props, command string) {
	t.Helper()

	manifestPath := filepath.Join(flagsHookPath, ".gtb", "manifest.yaml")
	g := New(p, &Config{Path: flagsHookPath})
	m, err := g.decodeManifestFile(manifestPath)
	require.NoError(t, err)

	walkCommandPath(m.Commands, strings.Split(command, "/")).FlagsHook = true
	require.NoError(t, g.marshalManifestFile(manifestPath, m))
}

func TestFlagsHook_AllowsAFlagNothingInherits(t *testing.T) {
	t.Parallel()

	p := setupTestProject(t, flagsHookPath)
	generateFlagsHookCmd(t, p, Config{Name: "keys", Flags: []string{"flag-set:string:x"}})
	generateFlagsHookCmd(t, p, Config{Name: "sign", Parent: "keys", FlagsHook: true})
	generateFlagsHookCmd(t, p, Config{Name: "other", Parent: "keys", Flags: []string{"flag-set:string:x:true"}})
}

func TestFlagsHook_RefusesOnRegenerate(t *testing.T) {
	t.Parallel()

	p := flagsHookProject(t)

	m := loadFlagsHookManifest(t, p)
	recorded := walkCommandPath(m.Commands, []string{"sign"})
	recorded.Flags = []ManifestFlag{{Name: "flag-set", Type: "string"}}
	require.NoError(t, EncodeManifestFile(p.FS, ManifestPathFor(flagsHookPath), m))

	before := readFlagsHookFile(t, p, "sign/cmd.go")

	require.ErrorIs(t, New(p, &Config{Path: flagsHookPath}).RegenerateProject(context.Background()), ErrInvalidInput)
	require.ErrorIs(t, New(p, &Config{Path: flagsHookPath}).RegenerateCommand(context.Background(), *recorded, nil), ErrInvalidInput)
	assert.Equal(t, before, readFlagsHookFile(t, p, "sign/cmd.go"))
}

func snapshotFS(t *testing.T, fs afero.Fs) map[string]string {
	t.Helper()

	files := map[string]string{}

	require.NoError(t, afero.Walk(fs, flagsHookPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		content, err := afero.ReadFile(fs, path)
		files[path] = string(content)

		return err
	}))

	return files
}
