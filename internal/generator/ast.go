package generator

import (
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
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

	g.props.Logger.Debugf("Reading parent command file: %s", ctx.parentFile)

	fsrc, err := afero.ReadFile(g.props.FS, ctx.parentFile)
	if err != nil {
		return errors.Wrap(err, "failed to read parent command file")
	}

	g.props.Logger.Debug("Parsing parent command AST...")

	f, err := decorator.Parse(fsrc)
	if err != nil {
		return errors.Wrap(err, "failed to parse parent command file")
	}

	g.props.Logger.Debugf("Adding import for %s", ctx.importPath)
	g.addSubcommandImport(f, ctx.importPath)

	targetFunc, err := g.findSubcommandTargetFunction(f, ctx.parentName, ctx.parentFile)
	if err != nil {
		return err
	}

	g.props.Logger.Debugf("Analyzing target function NewCmd%s for existing registrations", PascalCase(ctx.parentName))
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

func (g *Generator) addSubcommandImport(f *dst.File, path string) {
	importPath := fmt.Sprintf("\"%s\"", path)

	for _, imp := range f.Imports {
		if imp.Path.Value == importPath {
			return
		}
	}

	// Find the last import declaration to append to
	var lastImportDecl *dst.GenDecl

	for _, decl := range f.Decls {
		if gd, ok := decl.(*dst.GenDecl); ok && gd.Tok == token.IMPORT {
			lastImportDecl = gd
		}
	}

	if lastImportDecl != nil {
		// Append to existing import block
		lastImportDecl.Specs = append(lastImportDecl.Specs, &dst.ImportSpec{
			Path: &dst.BasicLit{Kind: token.STRING, Value: importPath},
		})

		return
	}

	// Create new import block if none exists
	f.Decls = append([]dst.Decl{&dst.GenDecl{
		Tok: token.IMPORT,
		Specs: []dst.Spec{
			&dst.ImportSpec{
				Path: &dst.BasicLit{Kind: token.STRING, Value: importPath},
			},
		},
	}}, f.Decls...)
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
	if !ok || sel.Sel.Name != "AddCommand" {
		return
	}

	for _, arg := range call.Args {
		if g.isAddCommandArg(arg, ctx) {
			ctx.registered = true
		}
	}
}

func (g *Generator) isAddCommandArg(arg dst.Expr, ctx *subcommandContext) bool {
	// Check for inline call: cmd.AddCommand(pkg.NewCmdSub(props))
	if argCall, ok := arg.(*dst.CallExpr); ok {
		if argSel, ok := argCall.Fun.(*dst.SelectorExpr); ok {
			if xid, ok := argSel.X.(*dst.Ident); ok && xid.Name == ctx.pkgName && argSel.Sel.Name == ctx.funcNameToBeCalled {
				return true
			}
		}
	}

	// Check for variable: cmd.AddCommand(subCmd)
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
		g.props.Logger.Debugf("Subcommand %q already registered in parent, saving AST", g.config.Name)

		return g.saveAstFile(f, ctx.parentFile)
	}

	if ctx.isRoot && ctx.rootCmdInitIdx != -1 {
		g.props.Logger.Debugf("Inserting subcommand %q into root NewCmdRoot call", g.config.Name)
		g.insertIntoRoot(fn, ctx)
	} else {
		g.props.Logger.Debugf("Inserting AddCommand call for %q before return statement", g.config.Name)
		stmt := g.createRegistrationStmts(ctx)
		g.insertGeneric(fn, stmt)
	}

	return g.saveAstFile(f, ctx.parentFile)
}

func (g *Generator) createRegistrationStmts(ctx *subcommandContext) dst.Stmt {
	// Create: cmd.AddCommand(pkg.NewCmdName(props))

	// pkg.NewCmdName(props)
	newCmdCall := &dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X:   dst.NewIdent(ctx.pkgName),
			Sel: dst.NewIdent("NewCmd" + PascalCase(g.config.Name)),
		},
		Args: []dst.Expr{dst.NewIdent(ctx.propsVarName)},
	}

	// cmd.AddCommand(...)
	addCmdStmt := &dst.ExprStmt{
		X: &dst.CallExpr{
			Fun: &dst.SelectorExpr{
				X:   dst.NewIdent(ctx.cmdVarName),
				Sel: dst.NewIdent("AddCommand"),
			},
			Args: []dst.Expr{newCmdCall},
		},
	}

	// Ensure newline before AddCommand
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
	g.props.Logger.Debugf("Writing modified AST to %s", path)

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
		return filepath.Join(g.config.Path, "pkg", "cmd", g.config.Name), nil
	}

	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")

	data, err := afero.ReadFile(g.props.FS, manifestPath)
	if err != nil {
		return "", err
	}

	var m Manifest

	if err := yaml.Unmarshal(data, &m); err != nil {
		return "", err
	}

	// Verify parent path exists in manifest
	if !verifyPathExists(m.Commands, pathParts) {
		return "", errors.Newf("%w: %s", ErrParentPathNotFound, g.config.Parent)
	}

	return filepath.Join(g.config.Path, "pkg", "cmd", filepath.Join(append(pathParts, g.config.Name)...)), nil
}

func (g *Generator) deregisterSubcommand() error {
	g.props.Logger.Debugf("Deregistering subcommand %q...", g.config.Name)

	ctx, err := g.prepareSubcommandContext()
	if err != nil {
		return err
	}

	g.props.Logger.Debugf("Reading parent command file for deregistration: %s", ctx.parentFile)

	fsrc, err := afero.ReadFile(g.props.FS, ctx.parentFile)
	if err != nil {
		return errors.Wrap(err, "failed to read parent command file")
	}

	f, err := decorator.Parse(fsrc)
	if err != nil {
		return errors.Wrap(err, "failed to parse parent command file")
	}

	g.props.Logger.Debugf("Removing import %s", ctx.importPath)
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
		if g.isSubcommandInit(stmt, ctx) || g.isSubcommandAddCommand(stmt, ctx) || g.isSubcommandAssetAppend(stmt, ctx) {
			continue
		}

		newList = append(newList, stmt)
	}

	fn.Body.List = newList
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

func (g *Generator) isSubcommandAddCommand(stmt dst.Stmt, ctx *subcommandContext) bool {
	exprStmt, ok := stmt.(*dst.ExprStmt)
	if !ok {
		return false
	}

	call, ok := exprStmt.X.(*dst.CallExpr)
	if !ok {
		return false
	}

	sel, ok := call.Fun.(*dst.SelectorExpr)
	if !ok || sel.Sel.Name != "AddCommand" {
		return false
	}

	for _, arg := range call.Args {
		if id, ok := arg.(*dst.Ident); ok && (id.Name == ctx.subCmdVar || id.Name == ctx.pkgName+"Cmd") {
			return true
		}
	}

	return false
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
