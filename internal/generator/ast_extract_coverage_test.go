package generator

import (
	"go/token"
	"path/filepath"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// newCoverageGenerator builds a Generator backed by an in-memory filesystem
// seeded with a go.mod (so getModuleName resolves) and a noop logger. It is the
// shared fixture for the AST-extraction coverage tests: everything is in memory,
// no network/keychain/TTY/wall-clock, so the tests are CI-deterministic.
func newCoverageGenerator(t *testing.T) (*Generator, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/work/go.mod", []byte("module example.com/tool\n"), 0o644))

	g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: "/work"})

	return g, fs
}

// writeCmd writes src to /work/pkg/cmd/<dir>/cmd.go and returns that path.
func writeCmd(t *testing.T, fs afero.Fs, dir, src string) string {
	t.Helper()

	path := filepath.Join("/work/pkg/cmd", dir, "cmd.go")
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, afero.WriteFile(fs, path, []byte(src), 0o644))

	return path
}

// parseExpr parses a single Go expression string into a dst.Expr by wrapping it
// in a throwaway var declaration and pulling the value back out.
func parseExpr(t *testing.T, exprSrc string) dst.Expr {
	t.Helper()

	src := "package p\nvar _x = " + exprSrc + "\n"

	f, err := decorator.Parse(src)
	require.NoError(t, err)

	gd := f.Decls[0].(*dst.GenDecl)
	vs := gd.Specs[0].(*dst.ValueSpec)

	return vs.Values[0]
}

// parseFunc parses a source file and returns the named function declaration.
func parseFunc(t *testing.T, src, name string) *dst.FuncDecl {
	t.Helper()

	f, err := decorator.Parse(src)
	require.NoError(t, err)

	fn := findFuncDecl(f, name)
	require.NotNil(t, fn, "function %q not found", name)

	return fn
}

func findFlag(cmd *ManifestCommand, name string) *ManifestFlag {
	for i := range cmd.Flags {
		if cmd.Flags[i].Name == name {
			return &cmd.Flags[i]
		}
	}

	return nil
}

// --- evaluateTimeBinaryExpr -------------------------------------------------

func TestEvaluateTimeBinaryExpr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		exprSrc string
		wantVal string
		wantOK  bool
	}{
		{name: "lit * time.Second", exprSrc: "30 * time.Second", wantVal: "30s", wantOK: true},
		{name: "time.Minute * lit (selector first)", exprSrc: "time.Minute * 5", wantVal: "5m", wantOK: true},
		{name: "nanosecond suffix", exprSrc: "10 * time.Nanosecond", wantVal: "10ns", wantOK: true},
		{name: "hour suffix", exprSrc: "2 * time.Hour", wantVal: "2h", wantOK: true},
		{name: "non-mul op", exprSrc: "30 + time.Second", wantOK: false},
		{name: "no basic lit", exprSrc: "x * time.Second", wantOK: false},
		{name: "no selector", exprSrc: "30 * 40", wantOK: false},
		{name: "selector not time pkg", exprSrc: "30 * other.Second", wantOK: false},
		{name: "unknown time suffix", exprSrc: "30 * time.Fortnight", wantOK: false},
		{name: "non-int literal", exprSrc: "3.5 * time.Second", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			binary, ok := parseExpr(t, tt.exprSrc).(*dst.BinaryExpr)
			require.True(t, ok)

			got, ok := evaluateTimeBinaryExpr(binary)
			assert.Equal(t, tt.wantOK, ok)

			if tt.wantOK {
				assert.Equal(t, tt.wantVal, got)
			}
		})
	}
}

// selector-not-ident case: selectorExpr whose X is not an Ident (e.g. a.b.Second)
func TestEvaluateTimeBinaryExpr_SelectorXNotIdent(t *testing.T) {
	t.Parallel()

	binary, ok := parseExpr(t, "30 * pkg.sub.Second").(*dst.BinaryExpr)
	require.True(t, ok)

	_, got := evaluateTimeBinaryExpr(binary)
	assert.False(t, got)
}

// --- resolveStringValue (incl. constant / composite / bool / unresolved) ----

func TestResolveStringValue(t *testing.T) {
	t.Parallel()

	constants := map[string]string{"DefaultName": "widget"}

	tests := []struct {
		name    string
		exprSrc string
		wantVal string
		wantOK  bool
	}{
		{name: "string literal", exprSrc: `"hello"`, wantVal: "hello", wantOK: true},
		{name: "raw string literal", exprSrc: "`raw`", wantVal: "raw", wantOK: true},
		{name: "known constant", exprSrc: "DefaultName", wantVal: "widget", wantOK: true},
		{name: "true keyword", exprSrc: "true", wantVal: "true", wantOK: true},
		{name: "false keyword", exprSrc: "false", wantVal: "false", wantOK: true},
		{name: "nil keyword", exprSrc: "nil", wantVal: "nil", wantOK: true},
		{name: "unknown identifier", exprSrc: "SomeVar", wantVal: "SomeVar", wantOK: false},
		{name: "empty composite literal", exprSrc: "[]string{}", wantVal: "[]", wantOK: true},
		{name: "unsupported expr (selector)", exprSrc: "pkg.Thing", wantVal: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := resolveStringValue(parseExpr(t, tt.exprSrc), constants)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantVal, got)
		})
	}
}

// --- containsCall / extractCallTarget ---------------------------------------

func TestContainsCall(t *testing.T) {
	t.Parallel()

	g := &Generator{}

	tests := []struct {
		name    string
		bodySrc string
		prefix  string
		want    bool
	}{
		{
			name:    "direct PersistentPreRun bare-ident call",
			bodySrc: "{ PersistentPreRunHook() }",
			prefix:  "PersistentPreRun",
			want:    true,
		},
		{
			name:    "wrapped in ErrorHandler.Fatal",
			bodySrc: `{ eh.Fatal(PreRunSetup(p), "boom") }`,
			prefix:  "PreRun",
			want:    true,
		},
		{
			name:    "bare PreRun ident is excluded",
			bodySrc: "{ PreRun() }",
			prefix:  "PreRun",
			want:    false,
		},
		{
			name:    "bare PersistentPreRun ident is excluded",
			bodySrc: "{ PersistentPreRun() }",
			prefix:  "PersistentPreRun",
			want:    false,
		},
		{
			name:    "no matching call",
			bodySrc: "{ somethingElse() }",
			prefix:  "PreRun",
			want:    false,
		},
		{
			name:    "non-expr statement ignored",
			bodySrc: "{ x := 1; _ = x }",
			prefix:  "PreRun",
			want:    false,
		},
		{
			name:    "non-call expr statement ignored",
			bodySrc: "{ <-ch }",
			prefix:  "PreRun",
			want:    false,
		},
		{
			name:    "selector-target call (not ident) ignored",
			bodySrc: "{ a.b() }",
			prefix:  "PreRun",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fn := parseFunc(t, "package p\nfunc f() "+tt.bodySrc+"\n", "f")
			assert.Equal(t, tt.want, g.containsCall(fn.Body, tt.prefix))
		})
	}
}

func TestContainsCall_NilBody(t *testing.T) {
	t.Parallel()

	g := &Generator{}
	assert.False(t, g.containsCall(nil, "PreRun"))
}

func TestExtractCallTarget(t *testing.T) {
	t.Parallel()

	g := &Generator{}

	t.Run("plain call returns Fun", func(t *testing.T) {
		t.Parallel()

		call := parseExpr(t, "doThing()").(*dst.CallExpr)
		target := g.extractCallTarget(call)
		id, ok := target.(*dst.Ident)
		require.True(t, ok)
		assert.Equal(t, "doThing", id.Name)
	})

	t.Run("Fatal wrapper unwraps first arg", func(t *testing.T) {
		t.Parallel()

		call := parseExpr(t, `eh.Fatal(InnerCall(), "msg")`).(*dst.CallExpr)
		target := g.extractCallTarget(call)
		id, ok := target.(*dst.Ident)
		require.True(t, ok)
		assert.Equal(t, "InnerCall", id.Name)
	})

	t.Run("Fatal with no args returns Fatal selector", func(t *testing.T) {
		t.Parallel()

		call := parseExpr(t, "eh.Fatal()").(*dst.CallExpr)
		target := g.extractCallTarget(call)
		_, ok := target.(*dst.SelectorExpr)
		assert.True(t, ok)
	})

	t.Run("Fatal with non-call first arg returns Fatal selector", func(t *testing.T) {
		t.Parallel()

		call := parseExpr(t, "eh.Fatal(x)").(*dst.CallExpr)
		target := g.extractCallTarget(call)
		_, ok := target.(*dst.SelectorExpr)
		assert.True(t, ok)
	})
}

// --- isCobraCommandConstructor / fallbackFindTargetFunction -----------------

func TestIsCobraCommandConstructor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "returns *cobra.Command",
			src:  "package p\nfunc Build() *cobra.Command { return nil }\n",
			want: true,
		},
		{
			name: "no results",
			src:  "package p\nfunc Build() { }\n",
			want: false,
		},
		{
			name: "non-star result",
			src:  "package p\nfunc Build() cobra.Command { return cobra.Command{} }\n",
			want: false,
		},
		{
			name: "star but not selector",
			src:  "package p\nfunc Build() *Command { return nil }\n",
			want: false,
		},
		{
			name: "selector but wrong pkg",
			src:  "package p\nfunc Build() *other.Command { return nil }\n",
			want: false,
		},
		{
			name: "selector but wrong type name",
			src:  "package p\nfunc Build() *cobra.Group { return nil }\n",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fn := parseFunc(t, tt.src, "Build")
			assert.Equal(t, tt.want, isCobraCommandConstructor(fn))
		})
	}
}

// selector X not an Ident (e.g. *a.b.Command) isn't valid parseable Go, so the
// FuncDecl is built directly.
func TestIsCobraCommandConstructor_SelectorXNotIdent(t *testing.T) {
	t.Parallel()

	fn := &dst.FuncDecl{
		Type: &dst.FuncType{
			Results: &dst.FieldList{
				List: []*dst.Field{
					{
						Type: &dst.StarExpr{
							X: &dst.SelectorExpr{
								X:   &dst.SelectorExpr{X: &dst.Ident{Name: "a"}, Sel: &dst.Ident{Name: "b"}},
								Sel: &dst.Ident{Name: "Command"},
							},
						},
					},
				},
			},
		},
	}

	assert.False(t, isCobraCommandConstructor(fn))
}

func TestFallbackFindTargetFunction(t *testing.T) {
	t.Parallel()

	t.Run("finds cobra constructor among decls", func(t *testing.T) {
		t.Parallel()

		src := "package p\n" +
			"var x = 1\n" +
			"func helper() int { return 0 }\n" +
			"func Make() *cobra.Command { return nil }\n"

		f, err := decorator.Parse(src)
		require.NoError(t, err)

		fn := fallbackFindTargetFunction(f)
		require.NotNil(t, fn)
		assert.Equal(t, "Make", fn.Name.Name)
	})

	t.Run("returns nil when no constructor", func(t *testing.T) {
		t.Parallel()

		src := "package p\nfunc helper() int { return 0 }\n"

		f, err := decorator.Parse(src)
		require.NoError(t, err)

		assert.Nil(t, fallbackFindTargetFunction(f))
	})
}

// --- processDeclStmt (via extractCommandMetadata with a var decl cobra lit) --

func TestProcessDeclStmt(t *testing.T) {
	t.Parallel()

	g := &Generator{}

	t.Run("var decl with cobra literal extracts fields", func(t *testing.T) {
		t.Parallel()

		fn := parseFunc(t, "package p\nfunc f() {\n"+
			"\tvar cmd = &cobra.Command{Use: \"foo\", Short: \"a short\"}\n"+
			"\t_ = cmd\n}\n", "f")

		declStmt := fn.Body.List[0].(*dst.DeclStmt)
		cmd := &ManifestCommand{}
		exprs := g.processDeclStmt(declStmt, cmd)

		assert.Equal(t, "foo", cmd.Name)
		assert.Len(t, exprs, 1)
	})

	t.Run("non-var decl returns no exprs", func(t *testing.T) {
		t.Parallel()

		fn := parseFunc(t, "package p\nfunc f() {\n\tconst x = 1\n\t_ = x\n}\n", "f")
		declStmt := fn.Body.List[0].(*dst.DeclStmt)
		cmd := &ManifestCommand{}
		assert.Empty(t, g.processDeclStmt(declStmt, cmd))
	})
}

// --- markFlagHidden ----------------------------------------------------------

func TestMarkFlagHidden(t *testing.T) {
	t.Parallel()

	g := &Generator{}

	t.Run("marks matching flag hidden", func(t *testing.T) {
		t.Parallel()

		cmd := &ManifestCommand{Flags: []ManifestFlag{{Name: "secret"}, {Name: "other"}}}
		call := parseExpr(t, `cmd.Flags().MarkHidden("secret")`).(*dst.CallExpr)
		g.markFlagHidden(call, cmd)

		assert.True(t, findFlag(cmd, "secret").Hidden)
		assert.False(t, findFlag(cmd, "other").Hidden)
	})

	t.Run("no args is a no-op", func(t *testing.T) {
		t.Parallel()

		cmd := &ManifestCommand{Flags: []ManifestFlag{{Name: "x"}}}
		call := parseExpr(t, "cmd.MarkHidden()").(*dst.CallExpr)
		g.markFlagHidden(call, cmd)
		assert.False(t, cmd.Flags[0].Hidden)
	})

	t.Run("non-literal arg is a no-op", func(t *testing.T) {
		t.Parallel()

		cmd := &ManifestCommand{Flags: []ManifestFlag{{Name: "x"}}}
		call := parseExpr(t, "cmd.MarkHidden(varName)").(*dst.CallExpr)
		g.markFlagHidden(call, cmd)
		assert.False(t, cmd.Flags[0].Hidden)
	})
}

// --- processCobraKey (PreRun/PersistentPreRun/Aliases/Args branches) --------

func TestProcessCobraKey(t *testing.T) {
	t.Parallel()

	g := &Generator{}

	t.Run("PersistentPreRunE func lit with matching call", func(t *testing.T) {
		t.Parallel()

		valExpr := parseExpr(t, "func(c *cobra.Command, a []string) error { PersistentPreRunSetup(); return nil }")
		cmd := &ManifestCommand{}
		g.processCobraKey("PersistentPreRunE", "", valExpr, cmd)
		assert.True(t, cmd.PersistentPreRun)
	})

	t.Run("PersistentPreRun non-func-lit defaults to true", func(t *testing.T) {
		t.Parallel()

		valExpr := parseExpr(t, "someHandler")
		cmd := &ManifestCommand{}
		g.processCobraKey("PersistentPreRun", "", valExpr, cmd)
		assert.True(t, cmd.PersistentPreRun)
	})

	t.Run("PreRunE func lit with matching call", func(t *testing.T) {
		t.Parallel()

		valExpr := parseExpr(t, "func(c *cobra.Command, a []string) error { PreRunSetup(); return nil }")
		cmd := &ManifestCommand{}
		g.processCobraKey("PreRunE", "", valExpr, cmd)
		assert.True(t, cmd.PreRun)
	})

	t.Run("PreRun non-func-lit defaults to true", func(t *testing.T) {
		t.Parallel()

		valExpr := parseExpr(t, "handler")
		cmd := &ManifestCommand{}
		g.processCobraKey("PreRun", "", valExpr, cmd)
		assert.True(t, cmd.PreRun)
	})

	t.Run("Aliases", func(t *testing.T) {
		t.Parallel()

		valExpr := parseExpr(t, `[]string{"a", "b"}`)
		cmd := &ManifestCommand{}
		g.processCobraKey("Aliases", "", valExpr, cmd)
		assert.Equal(t, []string{"a", "b"}, cmd.Aliases)
	})

	t.Run("Args", func(t *testing.T) {
		t.Parallel()

		valExpr := parseExpr(t, "cobra.NoArgs")
		cmd := &ManifestCommand{}
		g.processCobraKey("Args", "", valExpr, cmd)
		assert.Equal(t, "NoArgs", cmd.Args)
	})

	t.Run("Use takes first field", func(t *testing.T) {
		t.Parallel()

		cmd := &ManifestCommand{}
		g.processCobraKey("Use", "deploy [target]", nil, cmd)
		assert.Equal(t, "deploy", cmd.Name)
	})

	t.Run("unknown key is a no-op", func(t *testing.T) {
		t.Parallel()

		cmd := &ManifestCommand{}
		g.processCobraKey("Example", "whatever", nil, cmd)
		assert.Empty(t, cmd.Name)
	})
}

// --- parseFlagExtras (P / non-P, default resolution & warning) --------------

func TestParseFlagExtras(t *testing.T) {
	t.Parallel()

	t.Run("P-variant resolves shorthand and default", func(t *testing.T) {
		t.Parallel()

		g, _ := newCoverageGenerator(t)
		// StringP(name, shorthand, default, usage)
		call := parseExpr(t, `cmd.Flags().StringP("region", "r", "us-east", "the region")`).(*dst.CallExpr)
		flag := &ManifestFlag{}
		g.parseFlagExtras(call, flag, 0, true, map[string]string{}, "/work/cmd.go")
		assert.Equal(t, "r", flag.Shorthand)
		assert.Equal(t, "us-east", flag.Default)
	})

	t.Run("non-P resolves default at baseIdx+1", func(t *testing.T) {
		t.Parallel()

		g, _ := newCoverageGenerator(t)
		// String(name, default, usage)
		call := parseExpr(t, `cmd.Flags().String("region", "eu-west", "the region")`).(*dst.CallExpr)
		flag := &ManifestFlag{}
		g.parseFlagExtras(call, flag, 0, false, map[string]string{}, "/work/cmd.go")
		assert.Equal(t, "eu-west", flag.Default)
	})

	t.Run("P-variant missing default args is a no-op past shorthand", func(t *testing.T) {
		t.Parallel()

		g, _ := newCoverageGenerator(t)
		call := parseExpr(t, `cmd.Flags().StringP("region", "r")`).(*dst.CallExpr)
		flag := &ManifestFlag{}
		g.parseFlagExtras(call, flag, 0, true, map[string]string{}, "/work/cmd.go")
		assert.Equal(t, "r", flag.Shorthand)
		assert.Empty(t, flag.Default)
	})

	t.Run("unresolved default sets warning", func(t *testing.T) {
		t.Parallel()

		g, _ := newCoverageGenerator(t)
		call := parseExpr(t, `cmd.Flags().String("region", someConst, "usage")`).(*dst.CallExpr)
		flag := &ManifestFlag{Name: "region"}
		g.parseFlagExtras(call, flag, 0, false, map[string]string{}, "/work/cmd.go")
		assert.Contains(t, flag.Warning, "could not resolve default value")
	})
}

// --- applyFeatureMutation ----------------------------------------------------

func TestApplyFeatureMutation(t *testing.T) {
	t.Parallel()

	constToFeature := map[string]string{
		"InitCmd": "init", "UpdateCmd": "update", "McpCmd": "mcp", "DocsCmd": "docs",
	}

	t.Run("Disable selector arg flips feature off", func(t *testing.T) {
		t.Parallel()

		enabled := map[string]bool{"init": true}
		applyFeatureMutation(parseExpr(t, "props.Disable(props.InitCmd)"), constToFeature, enabled)
		assert.False(t, enabled["init"])
	})

	t.Run("Enable with bare-ident const arg flips feature on", func(t *testing.T) {
		t.Parallel()

		enabled := map[string]bool{"docs": false}
		applyFeatureMutation(parseExpr(t, "props.Enable(DocsCmd)"), constToFeature, enabled)
		assert.True(t, enabled["docs"])
	})

	t.Run("non-call arg ignored", func(t *testing.T) {
		t.Parallel()

		enabled := map[string]bool{"init": true}
		applyFeatureMutation(parseExpr(t, "someIdent"), constToFeature, enabled)
		assert.True(t, enabled["init"])
	})

	t.Run("non-selector fun ignored", func(t *testing.T) {
		t.Parallel()

		enabled := map[string]bool{"init": true}
		applyFeatureMutation(parseExpr(t, "Enable()"), constToFeature, enabled)
		assert.True(t, enabled["init"])
	})

	t.Run("non Enable/Disable action ignored", func(t *testing.T) {
		t.Parallel()

		enabled := map[string]bool{"init": true}
		applyFeatureMutation(parseExpr(t, "props.Toggle(props.InitCmd)"), constToFeature, enabled)
		assert.True(t, enabled["init"])
	})

	t.Run("Enable/Disable with no args ignored", func(t *testing.T) {
		t.Parallel()

		enabled := map[string]bool{"init": true}
		applyFeatureMutation(parseExpr(t, "props.Disable()"), constToFeature, enabled)
		assert.True(t, enabled["init"])
	})

	t.Run("unknown const name ignored", func(t *testing.T) {
		t.Parallel()

		enabled := map[string]bool{"init": true}
		applyFeatureMutation(parseExpr(t, "props.Disable(props.SomethingCmd)"), constToFeature, enabled)
		assert.True(t, enabled["init"])
	})

	t.Run("non-selector/ident first arg ignored", func(t *testing.T) {
		t.Parallel()

		enabled := map[string]bool{"init": true}
		applyFeatureMutation(parseExpr(t, `props.Disable("InitCmd")`), constToFeature, enabled)
		assert.True(t, enabled["init"])
	})
}

// --- getFullyQualifiedName ---------------------------------------------------

func TestGetFullyQualifiedName(t *testing.T) {
	t.Parallel()

	g := &Generator{}
	imports := map[string]string{"child": "example.com/tool/pkg/cmd/child"}

	tests := []struct {
		name     string
		funcName string
		pkgAlias string
		want     string
	}{
		{
			name:     "known import alias",
			funcName: "NewCmdChild",
			pkgAlias: "child",
			want:     "example.com/tool/pkg/cmd/child.NewCmdChild",
		},
		{
			name:     "unknown alias falls back to alias",
			funcName: "NewCmdX",
			pkgAlias: "mystery",
			want:     "mystery.NewCmdX",
		},
		{
			name:     "no alias uses current package path",
			funcName: "NewCmdLocal",
			pkgAlias: "",
			want:     "example.com/tool/pkg/cmd/root.NewCmdLocal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := g.getFullyQualifiedName(tt.funcName, tt.pkgAlias, imports, "example.com/tool/pkg/cmd/root")
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- isTypeName --------------------------------------------------------------

func TestIsTypeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		exprSrc string
		typ     string
		want    bool
	}{
		{name: "bare ident match", exprSrc: "Props", typ: "Props", want: true},
		{name: "bare ident mismatch", exprSrc: "Other", typ: "Props", want: false},
		{name: "selector match", exprSrc: "props.Props", typ: "Props", want: true},
		{name: "selector mismatch", exprSrc: "props.Other", typ: "Props", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// parseExpr can't yield a bare type ident directly; build one explicitly.
			var expr dst.Expr
			switch tt.exprSrc {
			case "Props":
				expr = &dst.Ident{Name: "Props"}
			case "Other":
				expr = &dst.Ident{Name: "Other"}
			default:
				expr = parseExpr(t, tt.exprSrc)
			}

			assert.Equal(t, tt.want, isTypeName(expr, tt.typ))
		})
	}
}

func TestIsTypeName_OtherExpr(t *testing.T) {
	t.Parallel()
	// A non-ident, non-selector expression returns false.
	assert.False(t, isTypeName(&dst.BasicLit{Kind: token.STRING, Value: `"x"`}, "Props"))
}

// --- getCallInfo (selector-with-non-ident-X branch) -------------------------

func TestGetCallInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		exprSrc  string
		wantFunc string
		wantPkg  string
	}{
		{name: "bare ident call", exprSrc: "DoThing()", wantFunc: "DoThing", wantPkg: ""},
		{name: "selector with ident X", exprSrc: "child.NewCmdChild()", wantFunc: "NewCmdChild", wantPkg: "child"},
		{name: "selector with non-ident X", exprSrc: "a.b.Method()", wantFunc: "Method", wantPkg: ""},
		{name: "non-call-target (index expr)", exprSrc: "fns[0]()", wantFunc: "", wantPkg: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			call := parseExpr(t, tt.exprSrc).(*dst.CallExpr)
			fn, pkg := getCallInfo(call)
			assert.Equal(t, tt.wantFunc, fn)
			assert.Equal(t, tt.wantPkg, pkg)
		})
	}
}

// --- resolveConstantValue / extractConstants --------------------------------

func TestResolveConstantValueAndExtractConstants(t *testing.T) {
	t.Parallel()

	t.Run("literal, call-wrapped, time binary, unresolved", func(t *testing.T) {
		t.Parallel()

		assertResolve := func(src, want string, ok bool) {
			v, got := resolveConstantValue(parseExpr(t, src))
			assert.Equal(t, ok, got)
			if ok {
				assert.Equal(t, want, v)
			}
		}

		assertResolve(`"plain"`, "plain", true)
		assertResolve(`time.Duration("5s")`, "5s", true)
		assertResolve("30 * time.Second", "30s", true)
		assertResolve("someFunc()", "", false) // call with no basic-lit arg
		assertResolve("a + b", "", false)      // binary, not time
	})

	t.Run("extractConstants gathers const and var literals", func(t *testing.T) {
		t.Parallel()

		src := "package p\n" +
			"const Name = \"widget\"\n" +
			"var Region = \"us-east\"\n" +
			"const Timeout = 30 * time.Second\n" +
			"const Skip = unresolvable\n"

		f, err := decorator.Parse(src)
		require.NoError(t, err)

		constants := extractConstants(f)
		assert.Equal(t, "widget", constants["Name"])
		assert.Equal(t, "us-east", constants["Region"])
		assert.Equal(t, "30s", constants["Timeout"])
		_, ok := constants["Skip"]
		assert.False(t, ok)
	})
}

// --- detectInitializer / detectConfigValidation -----------------------------

func TestDetectInitializerAndConfigValidation(t *testing.T) {
	t.Parallel()

	g, fs := newCoverageGenerator(t)

	dir := "/work/pkg/cmd/sample"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "init.go"), []byte("package sample\n"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "config.go"), []byte("package sample\n"), 0o644))

	cmd := &ManifestCommand{}
	g.detectInitializer(filepath.Join(dir, "cmd.go"), cmd)
	g.detectConfigValidation(filepath.Join(dir, "cmd.go"), cmd)

	assert.True(t, cmd.WithInitializer)
	assert.True(t, cmd.WithConfigValidation)

	// Absent files leave the flags false.
	emptyDir := "/work/pkg/cmd/empty"
	require.NoError(t, fs.MkdirAll(emptyDir, 0o755))
	cmd2 := &ManifestCommand{}
	g.detectInitializer(filepath.Join(emptyDir, "cmd.go"), cmd2)
	g.detectConfigValidation(filepath.Join(emptyDir, "cmd.go"), cmd2)
	assert.False(t, cmd2.WithInitializer)
	assert.False(t, cmd2.WithConfigValidation)
}

// --- extractCommandMetadata: end-to-end through many helpers ----------------

const richCmdSrc = `package deploy

import (
	"github.com/spf13/cobra"
	"example.com/tool/pkg/cmd/deploy/child"
)

const DefaultRegion = "us-east-1"

// NewCmdDeploy builds the deploy command.
func NewCmdDeploy(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "deploy [target]",
		Short:   "Deploy things",
		Long:    "Deploy all the things",
		Aliases: []string{"dep"},
		Args:    cobra.NoArgs,
		PreRunE: func(c *cobra.Command, a []string) error { PreRunValidate(); return nil },
	}

	cmd.Flags().StringP("region", "r", DefaultRegion, "target region")
	cmd.Flags().Bool("force", false, "force deploy")
	cmd.PersistentFlags().String("profile", "default", "aws profile")

	cmd.MarkFlagRequired("region")
	cmd.Flags().MarkHidden("force")
	cmd.MarkFlagsMutuallyExclusive("region", "force")
	cmd.MarkFlagsRequiredTogether("region", "profile")
	cmd.Flags().MarkRequired("profile")

	cmd.AddCommand(child.NewCmdChild(p))

	return cmd
}
`

func TestExtractCommandMetadata_Rich(t *testing.T) {
	t.Parallel()

	g, fs := newCoverageGenerator(t)
	path := writeCmd(t, fs, "deploy", richCmdSrc)

	cmd, constructorName, subFuncs, err := g.extractCommandMetadata(path)
	require.NoError(t, err)

	assert.Equal(t, "deploy", cmd.Name)
	assert.Equal(t, MultilineString("Deploy things"), cmd.Description)
	assert.Equal(t, MultilineString("Deploy all the things"), cmd.LongDescription)
	assert.Equal(t, []string{"dep"}, cmd.Aliases)
	assert.Equal(t, "NoArgs", cmd.Args)
	assert.True(t, cmd.PreRun)

	region := findFlag(cmd, "region")
	require.NotNil(t, region)
	assert.Equal(t, "r", region.Shorthand)
	assert.Equal(t, "us-east-1", region.Default) // resolved through DefaultRegion const
	assert.True(t, region.Required)
	assert.False(t, region.Persistent)

	force := findFlag(cmd, "force")
	require.NotNil(t, force)
	assert.True(t, force.Hidden)

	profile := findFlag(cmd, "profile")
	require.NotNil(t, profile)
	assert.True(t, profile.Persistent)
	assert.True(t, profile.Required) // via MarkRequired complex path

	assert.Equal(t, [][]string{{"region", "force"}}, cmd.MutuallyExclusive)
	assert.Equal(t, [][]string{{"region", "profile"}}, cmd.RequiredTogether)

	assert.Contains(t, constructorName, "NewCmdDeploy")
	require.Len(t, subFuncs, 1)
	assert.Equal(t, "example.com/tool/pkg/cmd/deploy/child.NewCmdChild", subFuncs[0])
}

// Fallback path: no NewCmd<Pascal> match, but a cobra constructor exists.
func TestExtractCommandMetadata_FallbackConstructor(t *testing.T) {
	t.Parallel()

	g, fs := newCoverageGenerator(t)

	src := `package widget

import "github.com/spf13/cobra"

func Build(p *props.Props) *cobra.Command {
	return &cobra.Command{Use: "widget", Short: "a widget"}
}
`
	path := writeCmd(t, fs, "widget", src)

	cmd, _, _, err := g.extractCommandMetadata(path)
	require.NoError(t, err)
	assert.Equal(t, "widget", cmd.Name)
	assert.Equal(t, MultilineString("a widget"), cmd.Description)
}

// No constructor at all -> error.
func TestExtractCommandMetadata_NoConstructor(t *testing.T) {
	t.Parallel()

	g, fs := newCoverageGenerator(t)
	path := writeCmd(t, fs, "none", "package none\nfunc helper() int { return 0 }\n")

	_, _, _, err := g.extractCommandMetadata(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not find command constructor")
}

// Register verb (setup.Command wrapper) registers subcommands.
func TestExtractCommandMetadata_RegisterVerb(t *testing.T) {
	t.Parallel()

	g, fs := newCoverageGenerator(t)

	src := `package parent

import (
	"github.com/spf13/cobra"
	"example.com/tool/pkg/cmd/parent/sub"
)

func NewCmdParent(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "parent"}
	cmd.Register(sub.NewCmdSub(p))
	return cmd
}
`
	path := writeCmd(t, fs, "parent", src)

	_, _, subFuncs, err := g.extractCommandMetadata(path)
	require.NoError(t, err)
	require.Len(t, subFuncs, 1)
	assert.Equal(t, "example.com/tool/pkg/cmd/parent/sub.NewCmdSub", subFuncs[0])
}

// Read error path: missing file.
func TestExtractCommandMetadata_ReadError(t *testing.T) {
	t.Parallel()

	g, _ := newCoverageGenerator(t)
	_, _, _, err := g.extractCommandMetadata("/work/pkg/cmd/ghost/cmd.go")
	require.Error(t, err)
}

// Parse error path: invalid Go source.
func TestExtractCommandMetadata_ParseError(t *testing.T) {
	t.Parallel()

	g, fs := newCoverageGenerator(t)
	path := writeCmd(t, fs, "broken", "this is not go")
	_, _, _, err := g.extractCommandMetadata(path)
	require.Error(t, err)
}

// --- extractProjectProperties: end-to-end ------------------------------------

const rootCmdSrc = `package root

import "example.com/tool/pkg/props"

func NewCmdRoot(p *props.Props) *cobra.Command {
	props := &props.Props{
		Tool: props.Tool{
			Name:        "mytool",
			Description: "My great tool",
			Features: props.SetFeatures(
				props.Disable(props.InitCmd),
				props.Enable(props.AiCmd),
			),
			ReleaseSource: props.ReleaseSource{
				Type:  "gitlab",
				Host:  "gitlab.com",
				Owner: "acme",
				Repo:  "mytool",
			},
		},
	}
	cmd := props.NewCmdRoot(props)
	return cmd
}
`

func TestExtractProjectProperties(t *testing.T) {
	t.Parallel()

	g, fs := newCoverageGenerator(t)
	path := "/work/pkg/cmd/root/cmd.go"
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, afero.WriteFile(fs, path, []byte(rootCmdSrc), 0o644))

	mp, rs, err := g.extractProjectProperties(path)
	require.NoError(t, err)

	assert.Equal(t, "mytool", mp.Name)
	assert.Equal(t, MultilineString("My great tool"), mp.Description)

	assert.Equal(t, "gitlab", rs.Type)
	assert.Equal(t, "gitlab.com", rs.Host)
	assert.Equal(t, "acme", rs.Owner)
	assert.Equal(t, "mytool", rs.Repo)

	// The recovered feature list is the delta from framework defaults: a
	// default-on feature that was Disabled, and a default-off feature that was
	// Enabled, each carry an entry. Features at their default are inferred at
	// read time and carry no entry.
	init := findFeature(mp.Features, "init")
	require.NotNil(t, init, "Disabled default-on feature must be present")
	assert.False(t, init.Enabled)

	ai := findFeature(mp.Features, "ai")
	require.NotNil(t, ai, "Enabled opt-in feature must be present")
	assert.True(t, ai.Enabled)

	// docs and update remain at their default, so they carry no entry.
	assert.Nil(t, findFeature(mp.Features, "docs"), "default-state feature must be omitted")
	assert.Nil(t, findFeature(mp.Features, "update"), "default-state feature must be omitted")
}

func TestExtractProjectProperties_NoNewCmdRoot(t *testing.T) {
	t.Parallel()

	g, fs := newCoverageGenerator(t)
	path := "/work/pkg/cmd/root/cmd.go"
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, afero.WriteFile(fs, path, []byte("package root\nfunc Other() {}\n"), 0o644))

	_, _, err := g.extractProjectProperties(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NewCmdRoot not found")
}

func TestExtractProjectProperties_ReadError(t *testing.T) {
	t.Parallel()

	g, _ := newCoverageGenerator(t)
	_, _, err := g.extractProjectProperties("/work/pkg/cmd/root/missing.go")
	require.Error(t, err)
}

func TestExtractProjectProperties_ParseError(t *testing.T) {
	t.Parallel()

	g, fs := newCoverageGenerator(t)
	path := "/work/pkg/cmd/root/cmd.go"
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, afero.WriteFile(fs, path, []byte("not valid go!!!"), 0o644))

	_, _, err := g.extractProjectProperties(path)
	require.Error(t, err)
}

func findFeature(features []ManifestFeature, name string) *ManifestFeature {
	for i := range features {
		if features[i].Name == name {
			return &features[i]
		}
	}

	return nil
}

// --- findPropsLiteralInFunc / tryExtractPropsLiteral negative branches -------

func TestFindPropsLiteralInFunc_NotFound(t *testing.T) {
	t.Parallel()

	fn := parseFunc(t, "package p\nfunc f() {\n\tx := 1\n\t_ = x\n}\n", "f")
	_, _, err := findPropsLiteralInFunc(fn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "props.Props literal not found")
}

func TestTryExtractPropsLiteral_NegativeBranches(t *testing.T) {
	t.Parallel()

	t.Run("not a unary expr", func(t *testing.T) {
		t.Parallel()
		_, _, err := tryExtractPropsLiteral(parseExpr(t, "foo()"))
		require.Error(t, err)
	})

	t.Run("not a composite lit", func(t *testing.T) {
		t.Parallel()
		_, _, err := tryExtractPropsLiteral(parseExpr(t, "&x"))
		require.Error(t, err)
	})

	t.Run("not Props type", func(t *testing.T) {
		t.Parallel()
		_, _, err := tryExtractPropsLiteral(parseExpr(t, "&props.Other{}"))
		require.Error(t, err)
	})

	t.Run("Tool field not found", func(t *testing.T) {
		t.Parallel()
		_, _, err := tryExtractPropsLiteral(parseExpr(t, "&props.Props{Logger: l}"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Tool field not found")
	})

	t.Run("Tool value not composite lit", func(t *testing.T) {
		t.Parallel()
		_, _, err := tryExtractPropsLiteral(parseExpr(t, "&props.Props{Tool: someTool}"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Tool value is not a composite lit")
	})

	t.Run("happy path bare Props ident", func(t *testing.T) {
		t.Parallel()
		mp, _, err := tryExtractPropsLiteral(parseExpr(t, `&Props{Tool: Tool{Name: "x"}}`))
		require.NoError(t, err)
		assert.Equal(t, "x", mp.Name)
	})
}

// --- stringLitValue ----------------------------------------------------------

func TestStringLitValue(t *testing.T) {
	t.Parallel()

	v, ok := stringLitValue(parseExpr(t, `"quoted"`))
	assert.True(t, ok)
	assert.Equal(t, "quoted", v)

	_, ok = stringLitValue(parseExpr(t, "ident"))
	assert.False(t, ok)
}

// --- extractReleaseSourceLiteral negative branches --------------------------

func TestExtractReleaseSourceLiteral_SkipsNonMatching(t *testing.T) {
	t.Parallel()

	// Covers: bare (non-KV) elt, a non-literal value, an unknown key, plus the
	// full set of recognised keys.
	comp := parseExpr(t, `props.ReleaseSource{
		bareElt,
		Type:  "github",
		Host:  "github.com",
		Owner: "acme",
		Repo:  "thing",
		Unknown: "ignored",
		Bad:   nonLiteral,
	}`).(*dst.CompositeLit)

	rs := &ManifestReleaseSource{}
	extractReleaseSourceLiteral(comp, rs)
	assert.Equal(t, "github", rs.Type)
	assert.Equal(t, "github.com", rs.Host)
	assert.Equal(t, "acme", rs.Owner)
	assert.Equal(t, "thing", rs.Repo)
}

// --- extractFlagFromCall negative branches ----------------------------------

func TestExtractFlagFromCall_NegativeBranches(t *testing.T) {
	t.Parallel()

	g, _ := newCoverageGenerator(t)
	constants := map[string]string{}

	cases := []string{
		"plainFunc()",                 // Fun not a selector
		`cmd.Flags()`,                 // name contains "Flags"
		`obj.String("x", "y", "z")`,   // sel.X not a CallExpr
		`notFlags().String("x")`,      // subSel suffix not "Flags"
		`cmd.Flags().MarkHidden("x")`, // name has Mark prefix
	}

	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			call := parseExpr(t, src).(*dst.CallExpr)
			_, ok := g.extractFlagFromCall(call, constants, "/work/cmd.go")
			assert.False(t, ok)
		})
	}
}

func TestExtractFlagFromCall_PersistentFlag(t *testing.T) {
	t.Parallel()

	g, _ := newCoverageGenerator(t)
	call := parseExpr(t, `cmd.PersistentFlags().String("name", "def", "usage")`).(*dst.CallExpr)
	flag, ok := g.extractFlagFromCall(call, map[string]string{}, "/work/cmd.go")
	require.True(t, ok)
	assert.True(t, flag.Persistent)
	assert.Equal(t, "name", flag.Name)
	assert.Equal(t, "string", flag.Type)
}

// --- verifyPathExists --------------------------------------------------------

func TestVerifyPathExists(t *testing.T) {
	t.Parallel()

	tree := []ManifestCommand{
		{
			Name: "root",
			Commands: []ManifestCommand{
				{Name: "child", Commands: []ManifestCommand{{Name: "grandchild"}}},
			},
		},
	}

	tests := []struct {
		name string
		path []string
		want bool
	}{
		{name: "empty path", path: nil, want: true},
		{name: "single level", path: []string{"root"}, want: true},
		{name: "nested match", path: []string{"root", "child", "grandchild"}, want: true},
		{name: "missing top", path: []string{"nope"}, want: false},
		{name: "missing nested", path: []string{"root", "ghost"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, verifyPathExists(tree, tt.path))
		})
	}
}

// --- extractAddCommand: variable-registered subcommand ----------------------

func TestExtractAddCommand_VariableArg(t *testing.T) {
	t.Parallel()

	g, _ := newCoverageGenerator(t)
	imports := map[string]string{}
	varToConstructor := map[string]string{"childCmd": "example.com/tool/pkg/cmd/child.NewCmdChild"}

	var subFuncs []string

	// parent.AddCommand(childCmd) where childCmd is a previously-defined var.
	call := parseExpr(t, "cmd.AddCommand(childCmd)").(*dst.CallExpr)
	g.extractAddCommand(call, imports, "example.com/tool/pkg/cmd/parent", varToConstructor, &subFuncs)

	require.Len(t, subFuncs, 1)
	assert.Equal(t, "example.com/tool/pkg/cmd/child.NewCmdChild", subFuncs[0])
}

func TestExtractAddCommand_UnknownVariableIgnored(t *testing.T) {
	t.Parallel()

	g, _ := newCoverageGenerator(t)

	var subFuncs []string

	call := parseExpr(t, "cmd.AddCommand(unknownVar)").(*dst.CallExpr)
	g.extractAddCommand(call, map[string]string{}, "pkg", map[string]string{}, &subFuncs)
	assert.Empty(t, subFuncs)
}

// --- extractSubcommandOrMeta: variadic NewCmd root pattern ------------------

func TestExtractSubcommandOrMeta_VariadicNewCmdRoot(t *testing.T) {
	t.Parallel()

	g, _ := newCoverageGenerator(t)
	imports := map[string]string{"child": "example.com/tool/pkg/cmd/child"}

	var subFuncs []string

	cmd := &ManifestCommand{}
	// gtbRoot.NewCmdRoot(p, child.NewCmdChild(p), notACmd, plainArg())
	call := parseExpr(t, "gtbRoot.NewCmdRoot(p, child.NewCmdChild(x), other.Thing(x))").(*dst.CallExpr)
	g.extractSubcommandOrMeta(call, cmd, imports, "example.com/tool/pkg/cmd/root", map[string]string{}, &subFuncs)

	require.Len(t, subFuncs, 1)
	assert.Equal(t, "example.com/tool/pkg/cmd/child.NewCmdChild", subFuncs[0])
}

func TestExtractSubcommandOrMeta_NonSelectorFun(t *testing.T) {
	t.Parallel()

	g, _ := newCoverageGenerator(t)

	var subFuncs []string

	cmd := &ManifestCommand{}
	call := parseExpr(t, "plainFunc()").(*dst.CallExpr)
	g.extractSubcommandOrMeta(call, cmd, map[string]string{}, "pkg", map[string]string{}, &subFuncs)
	assert.Empty(t, subFuncs)
}

// --- markFlagRequired / complex / extractMarkRequiredOrHidden ---------------

func TestMarkFlagRequired_Branches(t *testing.T) {
	t.Parallel()

	g := &Generator{}

	t.Run("marks matching flag required", func(t *testing.T) {
		t.Parallel()
		cmd := &ManifestCommand{Flags: []ManifestFlag{{Name: "a"}}}
		g.markFlagRequired(parseExpr(t, `cmd.MarkFlagRequired("a")`).(*dst.CallExpr), cmd)
		assert.True(t, cmd.Flags[0].Required)
	})

	t.Run("no args no-op", func(t *testing.T) {
		t.Parallel()
		cmd := &ManifestCommand{Flags: []ManifestFlag{{Name: "a"}}}
		g.markFlagRequired(parseExpr(t, "cmd.MarkFlagRequired()").(*dst.CallExpr), cmd)
		assert.False(t, cmd.Flags[0].Required)
	})

	t.Run("non-literal arg no-op", func(t *testing.T) {
		t.Parallel()
		cmd := &ManifestCommand{Flags: []ManifestFlag{{Name: "a"}}}
		g.markFlagRequired(parseExpr(t, "cmd.MarkFlagRequired(v)").(*dst.CallExpr), cmd)
		assert.False(t, cmd.Flags[0].Required)
	})
}

func TestExtractMarkRequiredOrHidden_Dispatch(t *testing.T) {
	t.Parallel()

	g := &Generator{}

	t.Run("MarkPersistentFlagRequired routes to markFlagRequired", func(t *testing.T) {
		t.Parallel()
		cmd := &ManifestCommand{Flags: []ManifestFlag{{Name: "p"}}}
		call := parseExpr(t, `cmd.MarkPersistentFlagRequired("p")`).(*dst.CallExpr)
		g.extractMarkRequiredOrHidden(call, cmd, "MarkPersistentFlagRequired")
		assert.True(t, cmd.Flags[0].Required)
	})

	t.Run("MarkFlagHidden routes to markFlagHidden", func(t *testing.T) {
		t.Parallel()
		cmd := &ManifestCommand{Flags: []ManifestFlag{{Name: "h"}}}
		call := parseExpr(t, `cmd.MarkFlagHidden("h")`).(*dst.CallExpr)
		g.extractMarkRequiredOrHidden(call, cmd, "MarkFlagHidden")
		assert.True(t, cmd.Flags[0].Hidden)
	})

	t.Run("MarkHidden routes to complex path and hides flag", func(t *testing.T) {
		t.Parallel()
		cmd := &ManifestCommand{Flags: []ManifestFlag{{Name: "h"}}}
		call := parseExpr(t, `cmd.Flags().MarkHidden("h")`).(*dst.CallExpr)
		g.extractMarkRequiredOrHidden(call, cmd, "MarkHidden")
		assert.True(t, cmd.Flags[0].Hidden)
	})

	t.Run("MarkRequired routes to complex path on PersistentFlags", func(t *testing.T) {
		t.Parallel()
		cmd := &ManifestCommand{Flags: []ManifestFlag{{Name: "r"}}}
		call := parseExpr(t, `cmd.PersistentFlags().MarkRequired("r")`).(*dst.CallExpr)
		g.extractMarkRequiredOrHidden(call, cmd, "MarkRequired")
		assert.True(t, cmd.Flags[0].Required)
	})
}

func TestMarkFlagRequiredOrHiddenComplex_NegativeBranches(t *testing.T) {
	t.Parallel()

	g := &Generator{}
	cmd := &ManifestCommand{Flags: []ManifestFlag{{Name: "x"}}}

	cases := []string{
		"plainCall()",                  // Fun not a selector
		"obj.MarkRequired()",           // sel.X not a CallExpr
		`notFlags().MarkRequired("x")`, // subSel not Flags/PersistentFlags
	}

	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			call := parseExpr(t, src).(*dst.CallExpr)
			g.markFlagRequiredOrHiddenComplex(call, cmd, "MarkRequired")
			assert.False(t, cmd.Flags[0].Required)
			assert.False(t, cmd.Flags[0].Hidden)
		})
	}

	t.Run("no args no-op", func(t *testing.T) {
		t.Parallel()
		c := &ManifestCommand{Flags: []ManifestFlag{{Name: "x"}}}
		call := parseExpr(t, "cmd.Flags().MarkRequired()").(*dst.CallExpr)
		g.markFlagRequiredOrHiddenComplex(call, c, "MarkRequired")
		assert.False(t, c.Flags[0].Required)
	})

	t.Run("non-literal arg no-op", func(t *testing.T) {
		t.Parallel()
		c := &ManifestCommand{Flags: []ManifestFlag{{Name: "x"}}}
		call := parseExpr(t, "cmd.Flags().MarkRequired(v)").(*dst.CallExpr)
		g.markFlagRequiredOrHiddenComplex(call, c, "MarkRequired")
		assert.False(t, c.Flags[0].Required)
	})
}

// --- applyCobraLiteralFields: skip non-KV elt and non-Ident key -------------

func TestApplyCobraLiteralFields_SkipsMalformed(t *testing.T) {
	t.Parallel()

	g := &Generator{}
	// A composite with: a bare elt (not KeyValueExpr) and a valid Short key.
	comp := parseExpr(t, `cobra.Command{
		bareElement,
		Short: "the short",
	}`).(*dst.CompositeLit)

	cmd := &ManifestCommand{}
	g.applyCobraLiteralFields(comp, cmd)
	assert.Equal(t, MultilineString("the short"), cmd.Description)
}

// --- extractFromToolLiteral: skips non-KV elt and non-Ident key -------------

func TestExtractFromToolLiteral_SkipsMalformed(t *testing.T) {
	t.Parallel()

	comp := parseExpr(t, `props.Tool{
		bareElt,
		Name: "tool",
	}`).(*dst.CompositeLit)

	mp, _, err := extractFromToolLiteral(comp)
	require.NoError(t, err)
	assert.Equal(t, "tool", mp.Name)
}

// --- extractConstants: more names than values -------------------------------

func TestExtractConstants_MoreNamesThanValues(t *testing.T) {
	t.Parallel()

	// `var a, b = single()` => 2 names, 1 value: the i>=len(values) branch.
	src := "package p\nvar a, b = single()\nconst Good = \"yes\"\n"

	f, err := decorator.Parse(src)
	require.NoError(t, err)

	constants := extractConstants(f)
	assert.Equal(t, "yes", constants["Good"])
	_, ok := constants["b"]
	assert.False(t, ok)
}
