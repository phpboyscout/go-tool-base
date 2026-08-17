package generator

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/dave/dst"
	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
)

// ensureHookStubs appends any missing hook function stubs to an existing main.go.
// It is called when main.go is being preserved (no --force) so that enabling
// PersistentPreRun, PreRun, or WithInitializer on a subsequent generate doesn't
// silently leave the required function absent. Existing code is never modified or
// removed. Any extra imports required by injected stubs are also inserted.
func (g *Generator) ensureHookStubs(ctx context.Context, mainPath string, data templates.CommandData) error {
	if g.wiringSealed(mainPath, "injecting hook stubs") {
		return nil
	}

	src, err := afero.ReadFile(g.props.FS, mainPath)
	if err != nil {
		return errors.Newf("failed to read %s: %w", mainPath, err)
	}

	hooks := []hookSpec{
		{
			// A generated cmd.go that calls Run<Name> needs that function to
			// exist, so a preserved main.go which has lost it — most obviously
			// because the developer deleted it — would stop compiling. That is
			// the same failure the hook stubs below exist to prevent, so Run
			// belongs in the same table rather than being a special case.
			//
			// A PURE group is exempt: its cmd.go wires setup.GroupRunE and calls
			// nothing here, so injecting a stub would add a function nobody
			// calls — which is what issue #21 objected to.
			enabled:         !data.PureGroup,
			funcName:        "Run" + data.PascalName,
			requiredImports: []string{"gitlab.com/phpboyscout/go/errorhandling"},
			stub: func() string {
				return fmt.Sprintf(
					"\nfunc Run%s(ctx context.Context, props *props.Props, opts *%sOptions, args []string) error {\n"+
						"\treturn errorhandling.ErrNotImplemented\n"+
						"}\n",
					data.PascalName, data.PascalName,
				)
			},
		},
		{
			enabled:  data.PersistentPreRun,
			funcName: "PersistentPreRun" + data.PascalName,
			stub: func() string {
				return fmt.Sprintf(
					"\nfunc PersistentPreRun%s(ctx context.Context, props *props.Props, opts *%sOptions, args []string) error {\n"+
						"\tprops.Logger.Info(\"Running persistent pre run for %s\")\n"+
						"\treturn nil\n"+
						"}\n",
					data.PascalName, data.PascalName, data.Name,
				)
			},
		},
		{
			enabled:  data.PreRun,
			funcName: "PreRun" + data.PascalName,
			stub: func() string {
				return fmt.Sprintf(
					"\nfunc PreRun%s(ctx context.Context, props *props.Props, opts *%sOptions, args []string) error {\n"+
						"\tprops.Logger.Info(\"Running pre run for %s\")\n"+
						"\treturn nil\n"+
						"}\n",
					data.PascalName, data.PascalName, data.Name,
				)
			},
		},
		{
			enabled:  data.WithInitializer,
			funcName: "Init" + data.PascalName,
			requiredImports: []string{
				"gitlab.com/phpboyscout/go-tool-base/pkg/setup",
			},
			stub: func() string {
				return fmt.Sprintf(
					"\nfunc Init%s(ctx context.Context, p *props.Props, cfg setup.Editor) error {\n"+
						"\t// TODO: Implement custom initialization logic for %s\n"+
						"\treturn nil\n"+
						"}\n",
					data.PascalName, data.Name,
				)
			},
		},
	}

	content, appended := g.appendMissingStubs(string(src), mainPath, hooks)

	if !appended {
		return nil
	}

	if err := afero.WriteFile(g.props.FS, mainPath, []byte(content), DefaultFileMode); err != nil {
		return errors.Newf("failed to write %s: %w", mainPath, err)
	}

	if _, ok := g.props.FS.(*afero.OsFs); ok {
		cmd := exec.CommandContext(ctx, "go", "fmt", mainPath)
		_ = cmd.Run()
	}

	return nil
}

// ensureImport adds the given import path to the import block of a Go source
// file (represented as a string) if it is not already present. It locates the
// closing parenthesis of the first import(...) block and inserts the import
// just before it. If no grouped import block is found the file is returned
// unchanged (go fmt will catch any resulting compile error).
func ensureImport(src, importPath string) string {
	quoted := `"` + importPath + `"`

	if strings.Contains(src, quoted) {
		return src
	}

	// Find the closing ) of the import block.
	idx := strings.Index(src, "import (")
	if idx == -1 {
		return src
	}

	closeIdx := strings.Index(src[idx:], "\n)")
	if closeIdx == -1 {
		return src
	}

	insertAt := idx + closeIdx

	return src[:insertAt] + "\n\t" + quoted + src[insertAt:]
}

// isUntouchedStub reports whether the named function's body is exactly
// `return errorhandling.<sentinel>` for one of the given sentinels — the single
// statement the generator emits and nothing else.
//
// More than one sentinel is accepted because which one a stub carries depends on
// what the command was when it was scaffolded, not on what it is now: a command
// created as a leaf has ErrNotImplemented and keeps it after gaining children.
// Both are equally untouched, and an untouched stub expresses no intent.
func isUntouchedStub(f *dst.File, funcName string, sentinels ...string) bool {
	for _, d := range f.Decls {
		fn, ok := d.(*dst.FuncDecl)
		if ok && fn.Name.Name == funcName {
			for _, sentinel := range sentinels {
				if bodyIsSentinelReturn(fn, sentinel) {
					return true
				}
			}

			return false
		}
	}

	return false
}

// declaresFunc reports whether f declares a function with the given name.
func declaresFunc(f *dst.File, funcName string) bool {
	for _, d := range f.Decls {
		if fn, ok := d.(*dst.FuncDecl); ok && fn.Name.Name == funcName {
			return true
		}
	}

	return false
}

// bodyIsSentinelReturn reports whether fn's body is the single statement
// `return errorhandling.<sentinel>`.
func bodyIsSentinelReturn(fn *dst.FuncDecl, sentinel string) bool {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return false
	}

	ret, ok := fn.Body.List[0].(*dst.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}

	sel, ok := ret.Results[0].(*dst.SelectorExpr)
	if !ok || sel.Sel.Name != sentinel {
		return false
	}

	pkg, ok := sel.X.(*dst.Ident)

	return ok && pkg.Name == "errorhandling"
}

// hookSpec describes one function stub ensureHookStubs guarantees is present
// in a preserved main.go.
type hookSpec struct {
	enabled         bool
	funcName        string
	stub            func() string
	requiredImports []string
}

// appendMissingStubs appends the stub for every enabled hook the file does not
// already define, returning the updated content and whether anything changed.
func (g *Generator) appendMissingStubs(content, mainPath string, hooks []hookSpec) (string, bool) {
	appended := false

	for _, h := range hooks {
		if !h.enabled || strings.Contains(content, "func "+h.funcName+"(") {
			continue
		}

		g.props.Logger.Info("appending missing stub", "func", h.funcName, "path", mainPath)

		for _, imp := range h.requiredImports {
			content = ensureImport(content, imp)
		}

		content += h.stub()
		appended = true
	}

	return content, appended
}
