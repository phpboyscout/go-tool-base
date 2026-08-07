package generator

import (
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go/errors"
)

type subcommandContext struct {
	parentFile           string
	parentName           string
	isRoot               bool
	capacity             int
	pkgName              string
	importPath           string
	assetsVarName        string
	propsVarName         string
	optsVarName          string
	cmdVarName           string
	rootCmdInitIdx       int
	allAssetsInitialized bool
	stmtIdxToRemove      []int
	firstAllAssetsIdx    int
	subCmdVar            string
	funcNameToBeCalled   string
	registered           bool
}

func (g *Generator) registerSubcommand() error {
	g.props.Logger.Debug("Preparing subcommand context for registration...")

	ctx, err := g.prepareSubcommandContext()
	if err != nil {
		return err
	}

	if g.wiringSealed(ctx.parentFile, "registering subcommand "+g.config.Name) {
		return nil
	}

	g.props.Logger.Debug("reading parent command file", "path", ctx.parentFile)

	fsrc, err := afero.ReadFile(g.props.FS, ctx.parentFile)
	if err != nil {
		return errors.Wrap(err, "failed to read parent command file")
	}

	g.props.Logger.Debug("Parsing parent command AST...")

	f, err := decorator.Parse(fsrc)
	if err != nil {
		return errors.Wrap(err, "failed to parse parent command file")
	}

	g.props.Logger.Debug("adding import", "import", ctx.importPath)
	g.addSubcommandImport(f, ctx.importPath, ctx.pkgName)

	targetFunc, err := g.findSubcommandTargetFunction(f, ctx.parentName, ctx.parentFile)
	if err != nil {
		return err
	}

	g.props.Logger.Debug(fmt.Sprintf("Analyzing target function NewCmd%s for existing registrations", PascalCase(ctx.parentName)))
	g.analyzeTargetFunction(f, targetFunc, ctx)

	return g.applySubcommandRegistration(f, targetFunc, ctx)
}

func (g *Generator) prepareSubcommandContext() (*subcommandContext, error) {
	parentParts := g.getParentPathParts()
	isRoot := len(parentParts) == 0

	ctx := &subcommandContext{
		isRoot:            isRoot,
		capacity:          g.calculateManifestCapacity(),
		assetsVarName:     "assets",
		propsVarName:      "props",
		optsVarName:       "opts",
		cmdVarName:        "cmd",
		rootCmdInitIdx:    -1,
		firstAllAssetsIdx: -1,
		pkgName:           strings.ReplaceAll(g.config.Name, "-", "_"),
	}

	var parentRelPath string

	if isRoot {
		parentRelPath = filepath.Join("pkg", "cmd", "root")
		ctx.parentName = "root"
	} else {
		ctx.parentName = parentParts[len(parentParts)-1]
		parentRelPath = filepath.Join("pkg", "cmd", filepath.Join(parentParts...))
	}

	ctx.parentFile = filepath.Join(g.config.Path, parentRelPath, "cmd.go")

	if _, err := g.props.FS.Stat(ctx.parentFile); os.IsNotExist(err) {
		ctx.parentFile = g.fallbackParentFile(parentRelPath, ctx.parentName, isRoot)

		if _, err := g.props.FS.Stat(ctx.parentFile); os.IsNotExist(err) {
			return nil, errors.Newf("%w in %s", ErrParentCommandFileNotFound, parentRelPath)
		}
	}

	moduleName, err := g.getModuleName()
	if err != nil {
		return nil, err
	}

	cmdSubPath, err := g.getCommandPath()
	if err != nil {
		return nil, err
	}

	relCmdPath, err := filepath.Rel(g.config.Path, cmdSubPath)
	if err != nil {
		return nil, err
	}

	ctx.importPath = fmt.Sprintf("%s/%s", moduleName, relCmdPath)
	ctx.subCmdVar = ctx.pkgName + "Cmd"
	ctx.funcNameToBeCalled = "NewCmd" + PascalCase(g.config.Name)

	return ctx, nil
}

func (g *Generator) calculateManifestCapacity() int {
	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")
	capacity := 0

	if data, err := afero.ReadFile(g.props.FS, manifestPath); err == nil {
		var m Manifest

		if err := yaml.Unmarshal(data, &m); err == nil {
			capacity = countCommandsWithAssets(m.Commands) + 1 // +1 for root

			if g.config.WithAssets {
				capacity++
			}
		}
	}

	const fallbackCapacity = 2

	if capacity == 0 {
		return fallbackCapacity
	}

	return capacity
}

func (g *Generator) fallbackParentFile(relPath, name string, isRoot bool) string {
	if isRoot {
		return filepath.Join(g.config.Path, relPath, "root.go")
	}

	return filepath.Join(g.config.Path, relPath, name+".go")
}

func (g *Generator) addSubcommandImport(f *dst.File, path, alias string) {
	g.ensureImport(f, alias, fmt.Sprintf("\"%s\"", path))
}

// ensureImport adds quotedPath (a double-quoted import path) to f's imports if it
// is not already present, appending to the last import block or creating one. The
// import is given an explicit alias so the dst-incremental form matches the
// alias-everything style the jennifer renderer emits on a full regenerate —
// otherwise a freshly-generated subcommand import gains an alias on the first
// `regenerate project` (cosmetic round-trip churn).
func (g *Generator) ensureImport(f *dst.File, alias, quotedPath string) {
	if g.hasImport(f, quotedPath) {
		return
	}

	spec := &dst.ImportSpec{Path: &dst.BasicLit{Kind: token.STRING, Value: quotedPath}}
	if alias != "" {
		spec.Name = dst.NewIdent(alias)
	}

	if decl := g.findLastImportDecl(f); decl != nil {
		decl.Specs = append(decl.Specs, spec)

		return
	}

	f.Decls = append([]dst.Decl{&dst.GenDecl{
		Tok:   token.IMPORT,
		Specs: []dst.Spec{spec},
	}}, f.Decls...)
}

func (g *Generator) hasImport(f *dst.File, path string) bool {
	for _, imp := range f.Imports {
		if imp.Path.Value == path {
			return true
		}
	}

	return false
}

func (g *Generator) findLastImportDecl(f *dst.File) *dst.GenDecl {
	var lastImportDecl *dst.GenDecl

	for _, decl := range f.Decls {
		if gd, ok := decl.(*dst.GenDecl); ok && gd.Tok == token.IMPORT {
			lastImportDecl = gd
		}
	}

	return lastImportDecl
}

func (g *Generator) findSubcommandTargetFunction(f *dst.File, parentName, parentFile string) (*dst.FuncDecl, error) {
	funcName := "NewCmd" + PascalCase(parentName)

	for _, decl := range f.Decls {
		if fn, ok := decl.(*dst.FuncDecl); ok && fn.Name.Name == funcName {
			return fn, nil
		}
	}

	return nil, errors.Newf("%w %s in %s", ErrFuncNotFound, funcName, parentFile)
}

func (g *Generator) analyzeTargetFunction(f *dst.File, fn *dst.FuncDecl, ctx *subcommandContext) {
	g.findAssetsVariableName(f, ctx)

	g.findPropsFromParams(fn, ctx)

	for i, stmt := range fn.Body.List {
		if as, ok := stmt.(*dst.AssignStmt); ok {
			g.analyzeAssignStmt(as, i, ctx)
		}

		if ds, ok := stmt.(*dst.DeclStmt); ok {
			g.checkAllAssetsInitialized(ds, ctx)
		}

		if es, ok := stmt.(*dst.ExprStmt); ok {
			g.analyzeExprStmt(es, ctx)
		}
	}

	g.removeMarkedStatements(fn, ctx)
}

func (g *Generator) checkAllAssetsInitialized(ds *dst.DeclStmt, ctx *subcommandContext) {
	gd, ok := ds.Decl.(*dst.GenDecl)
	if !ok || gd.Tok != token.VAR {
		return
	}

	for _, spec := range gd.Specs {
		v, ok := spec.(*dst.ValueSpec)
		if !ok {
			continue
		}

		for _, name := range v.Names {
			if name.Name == "allAssets" {
				ctx.allAssetsInitialized = true
			}
		}
	}
}

func (g *Generator) findAssetsVariableName(f *dst.File, ctx *subcommandContext) {
	for _, decl := range f.Decls {
		g.processAssetsVarDecl(decl, ctx)
	}
}

func (g *Generator) processAssetsVarDecl(decl dst.Decl, ctx *subcommandContext) {
	gd, ok := decl.(*dst.GenDecl)
	if !ok || gd.Tok != token.VAR {
		return
	}

	for _, spec := range gd.Specs {
		v, ok := spec.(*dst.ValueSpec)
		if !ok {
			continue
		}

		for _, name := range v.Names {
			if strings.Contains(name.Name, "assets") {
				ctx.assetsVarName = name.Name
			}

			if name.Name == "allAssets" {
				ctx.allAssetsInitialized = true
			}
		}
	}
}

func (g *Generator) findPropsFromParams(fn *dst.FuncDecl, ctx *subcommandContext) {
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			lower := strings.ToLower(name.Name)
			if lower == "props" || lower == "p" {
				ctx.propsVarName = name.Name

				return
			}

			if strings.Contains(lower, "props") {
				ctx.propsVarName = name.Name

				return
			}
		}
	}
}

func (g *Generator) removeMarkedStatements(fn *dst.FuncDecl, ctx *subcommandContext) {
	for i := len(ctx.stmtIdxToRemove) - 1; i >= 0; i-- {
		idx := ctx.stmtIdxToRemove[i]
		fn.Body.List = append(fn.Body.List[:idx], fn.Body.List[idx+1:]...)

		if idx < ctx.rootCmdInitIdx {
			ctx.rootCmdInitIdx--
		}
	}
}

func (g *Generator) analyzeAssignStmt(as *dst.AssignStmt, idx int, ctx *subcommandContext) {
	for i, expr := range as.Rhs {
		g.checkRegistrationAndRootInit(as, expr, idx, i, ctx)

		g.checkPropsAssignment(as, expr, i, ctx)
		g.checkOptsAssignment(as, expr, i, ctx)

		if i < len(as.Lhs) {
			if id, ok := as.Lhs[i].(*dst.Ident); ok && id.Name == "allAssets" {
				g.handleAllAssetsAssignment(as, expr, idx, ctx)
			}
		}
	}
}

func (g *Generator) analyzeExprStmt(es *dst.ExprStmt, ctx *subcommandContext) {
	call, ok := es.X.(*dst.CallExpr)
	if !ok {
		return
	}

	sel, ok := call.Fun.(*dst.SelectorExpr)
	if !ok {
		return
	}

	switch sel.Sel.Name {
	case "AddCommand", "Register":
		// parent.AddCommand(...) and parent.Register(...) both register
		// a child against the parent — either matters for idempotency.
		for _, arg := range call.Args {
			if g.isRegistrationArg(arg, ctx) {
				ctx.registered = true
			}
		}
	}
}

func (g *Generator) isRegistrationArg(arg dst.Expr, ctx *subcommandContext) bool {
	// Inline constructor: cmd.Register(pkg.NewCmdSub(props)) — the live form —
	// or the legacy cmd.AddCommand(pkg.NewCmdSub(props)).
	if argCall, ok := arg.(*dst.CallExpr); ok {
		if argSel, ok := argCall.Fun.(*dst.SelectorExpr); ok {
			if xid, ok := argSel.X.(*dst.Ident); ok && xid.Name == ctx.pkgName && argSel.Sel.Name == ctx.funcNameToBeCalled {
				return true
			}
		}
	}

	// Variable form: cmd.Register(subCmd) / cmd.AddCommand(subCmd).
	if id, ok := arg.(*dst.Ident); ok && id.Name == ctx.subCmdVar {
		return true
	}

	return false
}

func (g *Generator) checkRegistrationAndRootInit(as *dst.AssignStmt, expr dst.Expr, idx, i int, ctx *subcommandContext) {
	call, ok := expr.(*dst.CallExpr)
	if !ok {
		return
	}

	sel, ok := call.Fun.(*dst.SelectorExpr)
	if !ok {
		return
	}

	if xid, ok := sel.X.(*dst.Ident); ok && xid.Name == ctx.pkgName && sel.Sel.Name == ctx.funcNameToBeCalled {
		ctx.registered = true
	}

	if sel.Sel.Name == "NewCmdRoot" {
		g.handleNewCmdRootInit(as, call, idx, i, ctx)
	}
}

func (g *Generator) handleNewCmdRootInit(as *dst.AssignStmt, call *dst.CallExpr, idx, i int, ctx *subcommandContext) {
	ctx.rootCmdInitIdx = idx

	if i < len(as.Lhs) {
		if id, ok := as.Lhs[i].(*dst.Ident); ok {
			ctx.cmdVarName = id.Name
		}
	}

	// Check arguments for inline subcommand initialization
	for _, arg := range call.Args {
		if argCall, ok := arg.(*dst.CallExpr); ok {
			if argSel, ok := argCall.Fun.(*dst.SelectorExpr); ok {
				if xid, ok := argSel.X.(*dst.Ident); ok && xid.Name == ctx.pkgName && argSel.Sel.Name == ctx.funcNameToBeCalled {
					ctx.registered = true
				}
			}
		}
	}
}

func (g *Generator) checkPropsAssignment(as *dst.AssignStmt, expr dst.Expr, i int, ctx *subcommandContext) {
	unary, ok := expr.(*dst.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return
	}

	composite, ok := unary.X.(*dst.CompositeLit)
	if !ok {
		return
	}

	if sel, ok := composite.Type.(*dst.SelectorExpr); ok && sel.Sel.Name == "Props" {
		if i < len(as.Lhs) {
			if id, ok := as.Lhs[i].(*dst.Ident); ok {
				ctx.propsVarName = id.Name
			}
		}
	}
}

func (g *Generator) checkOptsAssignment(as *dst.AssignStmt, expr dst.Expr, i int, ctx *subcommandContext) {
	unary, ok := expr.(*dst.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return
	}

	composite, ok := unary.X.(*dst.CompositeLit)
	if !ok {
		return
	}

	if sel, ok := composite.Type.(*dst.SelectorExpr); ok && strings.HasSuffix(sel.Sel.Name, "Options") {
		if i < len(as.Lhs) {
			if id, ok := as.Lhs[i].(*dst.Ident); ok {
				ctx.optsVarName = id.Name
			}
		}
	}
}

func (g *Generator) handleAllAssetsAssignment(as *dst.AssignStmt, expr dst.Expr, idx int, ctx *subcommandContext) {
	if call, ok := expr.(*dst.CallExpr); ok {
		if fid, ok := call.Fun.(*dst.Ident); ok && fid.Name == "make" {
			ctx.allAssetsInitialized = true

			const maxArgs = 3

			if len(call.Args) >= maxArgs {
				call.Args[2] = &dst.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", ctx.capacity)}
			}

			return
		}
	}

	if ctx.isRoot && as.Tok == token.DEFINE {
		ctx.stmtIdxToRemove = append(ctx.stmtIdxToRemove, idx)

		if ctx.firstAllAssetsIdx == -1 {
			ctx.firstAllAssetsIdx = idx
		}
	}
}

func (g *Generator) applySubcommandRegistration(f *dst.File, fn *dst.FuncDecl, ctx *subcommandContext) error {
	if ctx.registered {
		g.props.Logger.Debug("subcommand already registered in parent, saving AST", "command", g.config.Name)

		return g.saveAstFile(f, ctx.parentFile)
	}

	if ctx.isRoot && ctx.rootCmdInitIdx != -1 {
		g.props.Logger.Debug("inserting subcommand into root NewCmdRoot call", "command", g.config.Name)
		g.insertIntoRoot(fn, ctx)
	} else {
		g.props.Logger.Debug("inserting Register call before return statement", "command", g.config.Name)

		stmt := g.createRegistrationStmts(ctx)
		g.insertGeneric(fn, stmt)
	}

	return g.saveAstFile(f, ctx.parentFile)
}

func (g *Generator) createRegistrationStmts(ctx *subcommandContext) dst.Stmt {
	// Emit: cmd.Register(pkg.NewCmdName(props))
	//
	// Each NewCmd<Name> returns *setup.Command carrying its own feature, so
	// the parent only needs to attach the child via Register — middleware is
	// wired once at attach time. This replaces the now-removed
	// setup.AddCommandWithMiddleware(parent, child, props.<Name>Cmd) emission,
	// which referenced a feature constant the generator never created.
	newCmdCall := &dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X:   dst.NewIdent(ctx.pkgName),
			Sel: dst.NewIdent("NewCmd" + PascalCase(g.config.Name)),
		},
		Args: []dst.Expr{dst.NewIdent(ctx.propsVarName)},
	}

	callExpr := &dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X:   dst.NewIdent(ctx.cmdVarName),
			Sel: dst.NewIdent("Register"),
		},
		Args: []dst.Expr{newCmdCall},
	}

	addCmdStmt := &dst.ExprStmt{
		X: callExpr,
	}

	// Ensure newline before Register
	addCmdStmt.Decs.Before = dst.NewLine

	return addCmdStmt
}

func (g *Generator) insertIntoRoot(fn *dst.FuncDecl, ctx *subcommandContext) {
	// We no longer need initStmt (subCmd := NewSubCmd(p)) or appendAssetsStmt (allAssets = append(...))
	// Instead, we inject the NewSubCmd call directly into NewCmdRoot
	g.appendSubcommandCallToRootInit(fn, ctx)
}

func (g *Generator) appendSubcommandCallToRootInit(fn *dst.FuncDecl, ctx *subcommandContext) {
	for _, stmt := range fn.Body.List {
		if as, ok := stmt.(*dst.AssignStmt); ok {
			for _, expr := range as.Rhs {
				if call, ok := expr.(*dst.CallExpr); ok {
					if sel, ok := call.Fun.(*dst.SelectorExpr); ok && sel.Sel.Name == "NewCmdRoot" {
						// Create the new CallExpr: pkg.NewCmdName(p)
						newCmdCall := &dst.CallExpr{
							Fun: &dst.SelectorExpr{
								X:   dst.NewIdent(ctx.pkgName),
								Sel: dst.NewIdent(ctx.funcNameToBeCalled),
							},
							Args: []dst.Expr{dst.NewIdent("p")},
						}

						// Ensure the argument is on a new line
						newCmdCall.Decs.Before = dst.NewLine

						// Append this call to NewCmdRoot args
						call.Args = append(call.Args, newCmdCall)

						return
					}
				}
			}
		}
	}
}

func (g *Generator) insertGeneric(fn *dst.FuncDecl, stmt dst.Stmt) {
	for i, s := range fn.Body.List {
		if _, ok := s.(*dst.ReturnStmt); ok {
			// Insert before the return statement
			fn.Body.List = append(fn.Body.List[:i], append([]dst.Stmt{stmt}, fn.Body.List[i:]...)...)

			return
		}
	}

	// Fallback: append to end if no return found (unlikely for NewCmd...)
	fn.Body.List = append(fn.Body.List, stmt)
}

func (g *Generator) saveAstFile(f *dst.File, path string) error {
	g.props.Logger.Debug("writing modified AST", "path", path)

	fout, err := g.props.FS.Create(path)
	if err != nil {
		return errors.Wrap(err, "failed to create output file")
	}

	defer func() {
		_ = fout.Close()
	}()

	res := decorator.NewRestorer()

	return res.Fprint(fout, f)
}

func (g *Generator) getModuleName() (string, error) {
	content, err := afero.ReadFile(g.props.FS, filepath.Join(g.config.Path, "go.mod"))
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(content), "\n")

	if len(lines) > 0 && strings.HasPrefix(lines[0], "module ") {
		return strings.TrimSpace(strings.TrimPrefix(lines[0], "module ")), nil
	}

	return "", ErrModuleNotFound
}

func (g *Generator) getParentPathParts() []string {
	p := strings.TrimSpace(g.config.Parent)

	if p == "root" || p == "" {
		return []string{}
	}

	p = strings.Trim(p, "/")

	return strings.Split(p, "/")
}

func (g *Generator) getCommandPath() (string, error) {
	pathParts := g.getParentPathParts()

	if len(pathParts) == 0 {
		return g.containedCommandPath(g.config.Name)
	}

	m, err := g.decodeManifestFile(ManifestPathFor(g.config.Path))
	if err != nil {
		return "", err
	}

	// Verify parent path exists in manifest
	if !verifyPathExists(m.Commands, pathParts) {
		return "", errors.Newf("%w: %s", ErrParentPathNotFound, g.config.Parent)
	}

	return g.containedCommandPath(filepath.Join(append(pathParts, g.config.Name)...))
}

// containedCommandPath joins rel under <project>/pkg/cmd and rejects any
// result that escapes that directory (or resolves to the directory itself).
// This is the single chokepoint in front of the command-directory
// write/RemoveAll sinks: even if a hostile name slips past entry-point
// validation, the traversal is stopped here.
func (g *Generator) containedCommandPath(rel string) (string, error) {
	cmdRoot := filepath.Join(g.config.Path, "pkg", "cmd")

	target, contained, err := joinContained(cmdRoot, rel)
	if err != nil || contained == "." {
		return "", errors.Newf("command path %q escapes the project command directory", rel)
	}

	return target, nil
}

// joinContained joins rel beneath root and returns the result together with
// its root-relative form, rejecting any result that escapes root. Shared by
// the command-directory chokepoint above and the AI doc-tool containment in
// docs.go so the two guards cannot drift apart.
func joinContained(root, rel string) (target, contained string, err error) {
	target = filepath.Join(root, rel)

	contained, rerr := filepath.Rel(root, target)
	if rerr != nil || contained == ".." ||
		strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
		return "", "", errors.Newf("path %q escapes %q", rel, root)
	}

	return target, contained, nil
}

func (g *Generator) deregisterSubcommand() error {
	g.props.Logger.Debug("deregistering subcommand", "command", g.config.Name)

	ctx, err := g.prepareSubcommandContext()
	if err != nil {
		return err
	}

	if g.wiringSealed(ctx.parentFile, "deregistering subcommand "+g.config.Name) {
		return nil
	}

	g.props.Logger.Debug("reading parent command file for deregistration", "path", ctx.parentFile)

	fsrc, err := afero.ReadFile(g.props.FS, ctx.parentFile)
	if err != nil {
		return errors.Wrap(err, "failed to read parent command file")
	}

	f, err := decorator.Parse(fsrc)
	if err != nil {
		return errors.Wrap(err, "failed to parse parent command file")
	}

	g.props.Logger.Debug("removing import", "import", ctx.importPath)
	g.removeSubcommandImport(f, ctx.importPath)

	targetFunc, err := g.findSubcommandTargetFunction(f, ctx.parentName, ctx.parentFile)
	if err != nil {
		return err
	}

	g.props.Logger.Debug("Removing subcommand registration statements")
	g.removeSubcommandRegistration(targetFunc, ctx)

	return g.saveAstFile(f, ctx.parentFile)
}

func (g *Generator) removeSubcommandImport(f *dst.File, path string) {
	importPath := fmt.Sprintf("\"%s\"", path)

	for i, imp := range f.Imports {
		if imp.Path.Value == importPath {
			g.removeSpecFromGenDecl(f, importPath)
			f.Imports = append(f.Imports[:i], f.Imports[i+1:]...)

			break
		}
	}
}

func (g *Generator) removeSpecFromGenDecl(f *dst.File, importPath string) {
	for j, decl := range f.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}

		for k, spec := range gd.Specs {
			if ispec, ok := spec.(*dst.ImportSpec); ok && ispec.Path.Value == importPath {
				gd.Specs = append(gd.Specs[:k], gd.Specs[k+1:]...)

				if len(gd.Specs) == 0 {
					f.Decls = append(f.Decls[:j], f.Decls[j+1:]...)
				}

				return
			}
		}
	}
}

func (g *Generator) removeSubcommandRegistration(fn *dst.FuncDecl, ctx *subcommandContext) {
	var newList []dst.Stmt

	for _, stmt := range fn.Body.List {
		if g.isSubcommandInit(stmt, ctx) || g.isSubcommandRegistration(stmt, ctx) || g.isSubcommandAssetAppend(stmt, ctx) {
			continue
		}

		// A root-level command is registered as a variadic argument inside the
		// gtbRoot.NewCmdRoot(p, foo.NewCmdFoo(p), ...) call, not as its own
		// statement — strip that argument in place rather than dropping the whole
		// statement (keryx v0.19.0 Bug 1: the import was removed but the call left
		// behind, breaking the build).
		g.stripSubcommandRegistrationArg(stmt, ctx)

		newList = append(newList, stmt)
	}

	fn.Body.List = newList
}

// stripSubcommandRegistrationArg removes the `<pkg>.NewCmd<Name>(...)` argument
// from any call on a kept statement's right-hand side (the variadic
// NewCmdRoot(p, child1, child2) registration), preserving the statement and its
// other arguments.
func (g *Generator) stripSubcommandRegistrationArg(stmt dst.Stmt, ctx *subcommandContext) {
	as, ok := stmt.(*dst.AssignStmt)
	if !ok {
		return
	}

	for _, rhs := range as.Rhs {
		call, ok := rhs.(*dst.CallExpr)
		if !ok {
			continue
		}

		filtered := make([]dst.Expr, 0, len(call.Args))

		for _, arg := range call.Args {
			if g.isSubcommandConstructorCall(arg, ctx) {
				continue
			}

			filtered = append(filtered, arg)
		}

		call.Args = filtered
	}
}

// isSubcommandConstructorCall reports whether expr is the constructor call for
// the subcommand being removed, e.g. `foo.NewCmdFoo(p)` for pkg "foo".
func (g *Generator) isSubcommandConstructorCall(expr dst.Expr, ctx *subcommandContext) bool {
	call, ok := expr.(*dst.CallExpr)
	if !ok {
		return false
	}

	sel, ok := call.Fun.(*dst.SelectorExpr)
	if !ok {
		return false
	}

	xid, ok := sel.X.(*dst.Ident)

	return ok && xid.Name == ctx.pkgName && sel.Sel.Name == ctx.funcNameToBeCalled
}

func (g *Generator) isSubcommandInit(stmt dst.Stmt, ctx *subcommandContext) bool {
	as, ok := stmt.(*dst.AssignStmt)
	if !ok || as.Tok != token.DEFINE {
		return false
	}

	for _, expr := range as.Rhs {
		if call, ok := expr.(*dst.CallExpr); ok {
			if sel, ok := call.Fun.(*dst.SelectorExpr); ok {
				if xid, ok := sel.X.(*dst.Ident); ok && xid.Name == ctx.pkgName && sel.Sel.Name == ctx.funcNameToBeCalled {
					return true
				}
			}
		}
	}

	return false
}

func (g *Generator) isSubcommandRegistration(stmt dst.Stmt, ctx *subcommandContext) bool {
	exprStmt, ok := stmt.(*dst.ExprStmt)
	if !ok {
		return false
	}

	call, ok := exprStmt.X.(*dst.CallExpr)
	if !ok {
		return false
	}

	sel, ok := call.Fun.(*dst.SelectorExpr)
	// cobra's parent.AddCommand(child) and the GTB setup.Command wrapper's
	// parent.Register(child) both register a subcommand; a parent built on the
	// wrapper uses Register, so removal must recognise it too.
	if !ok || (sel.Sel.Name != "AddCommand" && sel.Sel.Name != "Register") {
		return false
	}

	for _, arg := range call.Args {
		if g.argRegistersSubcommand(arg, ctx) {
			return true
		}
	}

	return false
}

// argRegistersSubcommand reports whether a registration-call argument refers to
// the subcommand in ctx — either the `<pkg>Cmd` variable (the AddCommand(var)
// form) or the `<pkg>.NewCmd<Name>(props)` constructor call (the Register form).
func (g *Generator) argRegistersSubcommand(arg dst.Expr, ctx *subcommandContext) bool {
	if id, ok := arg.(*dst.Ident); ok && (id.Name == ctx.subCmdVar || id.Name == ctx.pkgName+"Cmd") {
		return true
	}

	return g.isSubcommandConstructorCall(arg, ctx)
}

func (g *Generator) isSubcommandAssetAppend(stmt dst.Stmt, ctx *subcommandContext) bool {
	as, ok := stmt.(*dst.AssignStmt)
	if !ok || as.Tok != token.ASSIGN {
		return false
	}

	for _, lhs := range as.Lhs {
		if id, ok := lhs.(*dst.Ident); ok && id.Name == "allAssets" {
			return g.checkAssetAppendRHS(as.Rhs, ctx)
		}
	}

	return false
}

func (g *Generator) checkAssetAppendRHS(rhs []dst.Expr, ctx *subcommandContext) bool {
	for _, expr := range rhs {
		call, ok := expr.(*dst.CallExpr)
		if !ok {
			continue
		}

		id, ok := call.Fun.(*dst.Ident)
		if !ok || id.Name != "append" {
			continue
		}

		if len(call.Args) > 1 {
			if aid, ok := call.Args[1].(*dst.Ident); ok && strings.HasSuffix(aid.Name, "Assets") && strings.HasPrefix(aid.Name, ctx.pkgName) {
				return true
			}
		}
	}

	return false
}
