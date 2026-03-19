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
	ctx, err := g.prepareSubcommandContext()
	if err != nil {
		return err
	}

	fsrc, err := afero.ReadFile(g.props.FS, ctx.parentFile)
	if err != nil {
		return err
	}

	f, err := decorator.Parse(fsrc)
	if err != nil {
		return err
	}

	g.addSubcommandImport(f, ctx.importPath)

	targetFunc, err := g.findSubcommandTargetFunction(f, ctx.parentName, ctx.parentFile)
	if err != nil {
		return err
	}

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
		return g.saveAstFile(f, ctx.parentFile)
	}

	if ctx.isRoot && ctx.rootCmdInitIdx != -1 {
		g.insertIntoRoot(fn, ctx)
	} else {
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
	fout, err := g.props.FS.Create(path)
	if err != nil {
		return err
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

func verifyPathExists(commands []ManifestCommand, path []string) bool {
	if len(path) == 0 {
		return true
	}

	for _, cmd := range commands {
		if cmd.Name == path[0] {
			return verifyPathExists(cmd.Commands, path[1:])
		}
	}

	return false
}

func countCommandsWithAssets(commands []ManifestCommand) int {
	count := 0

	for _, cmd := range commands {
		if cmd.WithAssets {
			count++
		}

		count += countCommandsWithAssets(cmd.Commands)
	}

	return count
}
func (g *Generator) deregisterSubcommand() error {
	ctx, err := g.prepareSubcommandContext()
	if err != nil {
		return err
	}

	fsrc, err := afero.ReadFile(g.props.FS, ctx.parentFile)
	if err != nil {
		return err
	}

	f, err := decorator.Parse(fsrc)
	if err != nil {
		return err
	}

	g.removeSubcommandImport(f, ctx.importPath)

	targetFunc, err := g.findSubcommandTargetFunction(f, ctx.parentName, ctx.parentFile)
	if err != nil {
		return err
	}

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
func (g *Generator) extractCommandMetadata(path string) (*ManifestCommand, string, []string, error) {
	fsrc, err := afero.ReadFile(g.props.FS, path)
	if err != nil {
		return nil, "", nil, err
	}

	f, err := decorator.Parse(fsrc)
	if err != nil {
		return nil, "", nil, err
	}

	cmd := &ManifestCommand{}
	g.detectAssets(path, f, cmd)
	g.detectInitializer(path, cmd)

	// Find NewCmd... function
	pkgName := filepath.Base(filepath.Dir(path))
	funcName := "NewCmd" + PascalCase(pkgName)

	// Phase 0: Collect constants/variables for value resolution
	constants := extractConstants(f)

	targetFunc := findTargetFunction(f, funcName)
	if targetFunc == nil {
		targetFunc = fallbackFindTargetFunction(f)
	}

	if targetFunc == nil {
		return nil, "", nil, errors.Newf("could not find command constructor in %s", path)
	}

	imports := parseImports(f)

	moduleName, _ := g.getModuleName() // Best effort
	relPath, _ := filepath.Rel(g.config.Path, filepath.Dir(path))
	currentPkgPath := filepath.Join(moduleName, relPath)

	constructorName := fmt.Sprintf("%s.%s", currentPkgPath, targetFunc.Name.Name)

	subcommandFuncs := g.processCommandBody(targetFunc, cmd, constants, path, imports, currentPkgPath)

	if cmd.Name == "" {
		cmd.Name = pkgName
	}

	return cmd, constructorName, subcommandFuncs, nil
}

// Helper to resolve string values from literals or constants.
func resolveStringValue(expr dst.Expr, constants map[string]string) (string, bool) {
	if lit, ok := expr.(*dst.BasicLit); ok {
		return strings.Trim(lit.Value, "`\""), true
	}

	if id, ok := expr.(*dst.Ident); ok {
		if val, ok := constants[id.Name]; ok {
			return val, true
		}

		if id.Name == "true" || id.Name == "false" || id.Name == "nil" {
			return id.Name, true
		}

		return id.Name, false
	}

	if _, ok := expr.(*dst.CompositeLit); ok {
		// Just return empty [] for now as raw representation for empty composite literal
		// We could be more fancy and try to print it if it has elements, but for []string{} it's []
		return "[]", true
	}

	return "", false
}

func (g *Generator) extractFromCobraLiteral(expr dst.Expr, cmd *ManifestCommand) {
	unary, ok := expr.(*dst.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return
	}

	composite, ok := unary.X.(*dst.CompositeLit)
	if !ok {
		return
	}

	sel, ok := composite.Type.(*dst.SelectorExpr)
	if !ok || sel.Sel.Name != "Command" {
		return
	}

	for _, elt := range composite.Elts {
		kve, ok := elt.(*dst.KeyValueExpr)
		if !ok {
			continue
		}

		key, ok := kve.Key.(*dst.Ident)
		if !ok {
			continue
		}

		val := ""
		if lit, ok := kve.Value.(*dst.BasicLit); ok {
			val = strings.Trim(lit.Value, "`\"")
		}

		g.processCobraKey(key.Name, val, kve.Value, cmd)
	}
}

func (g *Generator) processCobraKey(keyName, val string, valueExpr dst.Expr, cmd *ManifestCommand) {
	switch keyName {
	case "Use":
		cmd.Name = strings.Fields(val)[0]
	case "Short":
		cmd.Description = MultilineString(val)
	case "Long":
		cmd.LongDescription = MultilineString(val)
	case "PersistentPreRun", "PersistentPreRunE":
		if fn, ok := valueExpr.(*dst.FuncLit); ok {
			cmd.PersistentPreRun = g.containsCall(fn.Body, "PersistentPreRun")
		} else {
			cmd.PersistentPreRun = true
		}
	case "PreRun", "PreRunE":
		if fn, ok := valueExpr.(*dst.FuncLit); ok {
			cmd.PreRun = g.containsCall(fn.Body, "PreRun")
		} else {
			cmd.PreRun = true
		}
	case "Aliases":
		g.extractAliases(valueExpr, cmd)
	case "Args":
		g.extractArgs(valueExpr, cmd)
	}
}

func (g *Generator) extractAliases(expr dst.Expr, cmd *ManifestCommand) {
	if comp, ok := expr.(*dst.CompositeLit); ok {
		for _, elt := range comp.Elts {
			if lit, ok := elt.(*dst.BasicLit); ok {
				cmd.Aliases = append(cmd.Aliases, strings.Trim(lit.Value, "`\""))
			}
		}
	}
}

func (g *Generator) extractArgs(expr dst.Expr, cmd *ManifestCommand) {
	if sel, ok := expr.(*dst.SelectorExpr); ok {
		// Handle cobra.NoArgs, cobra.ArbitraryArgs, etc.
		cmd.Args = sel.Sel.Name
	} else if call, ok := expr.(*dst.CallExpr); ok {
		// Handle cobra.ExactArgs(1), cobra.MinimumNArgs(1), etc.
		if sel, ok := call.Fun.(*dst.SelectorExpr); ok {
			var args []string

			for _, arg := range call.Args {
				if lit, ok := arg.(*dst.BasicLit); ok {
					args = append(args, lit.Value)
				}
			}

			cmd.Args = fmt.Sprintf("%s(%s)", sel.Sel.Name, strings.Join(args, ", "))
		}
	}
}

func (g *Generator) extractFlagFromCall(call *dst.CallExpr, constants map[string]string, cmdPath string) (*ManifestFlag, bool) {
	sel, ok := call.Fun.(*dst.SelectorExpr)
	if !ok {
		return nil, false
	}

	name := sel.Sel.Name
	if strings.Contains(name, "Flags") {
		return nil, false
	}

	// Check if it's a call on a Flags object
	subCall, ok := sel.X.(*dst.CallExpr)
	if !ok {
		return nil, false
	}

	subSel, ok := subCall.Fun.(*dst.SelectorExpr)
	if !ok || !strings.HasSuffix(subSel.Sel.Name, "Flags") {
		return nil, false
	}

	if strings.HasPrefix(name, "Mark") {
		return nil, false
	}

	flag := &ManifestFlag{}
	flag.Persistent = (subSel.Sel.Name == "PersistentFlags")

	return g.parseFlagArgs(call, flag, name, constants, cmdPath)
}

func evaluateTimeBinaryExpr(binary *dst.BinaryExpr) (string, bool) {
	if binary.Op != token.MUL {
		return "", false
	}

	x := binary.X
	y := binary.Y

	// Normalize: put basic lit in x, selector in y
	if _, ok := x.(*dst.SelectorExpr); ok {
		x, y = y, x
	}

	lit, ok := x.(*dst.BasicLit)
	if !ok {
		return "", false
	}

	sel, ok := y.(*dst.SelectorExpr)
	if !ok {
		return "", false
	}

	if xid, ok := sel.X.(*dst.Ident); !ok || xid.Name != "time" {
		return "", false
	}

	multiplier := 0
	if lit.Kind == token.INT {
		if _, err := fmt.Sscanf(lit.Value, "%d", &multiplier); err != nil {
			return "", false
		}
	} else {
		return "", false
	}

	suffix, ok := timeSuffixes[sel.Sel.Name]
	if !ok {
		return "", false
	}

	return fmt.Sprintf("%d%s", multiplier, suffix), true
}

var timeSuffixes = map[string]string{
	"Nanosecond":  "ns",
	"Microsecond": "us",
	"Millisecond": "ms",
	"Second":      "s",
	"Minute":      "m",
	"Hour":        "h",
}

func (g *Generator) containsCall(body *dst.BlockStmt, prefix string) bool {
	if body == nil {
		return false
	}

	for _, stmt := range body.List {
		exprStmt, ok := stmt.(*dst.ExprStmt)
		if !ok {
			continue
		}

		call, ok := exprStmt.X.(*dst.CallExpr)
		if !ok {
			continue
		}

		target := g.extractCallTarget(call)

		if id, ok := target.(*dst.Ident); ok {
			if strings.HasPrefix(id.Name, prefix) && id.Name != "PreRun" && id.Name != "PersistentPreRun" {
				return true
			}
		}
	}

	return false
}

func (g *Generator) extractCallTarget(call *dst.CallExpr) dst.Expr {
	var target = call.Fun
	// Handle potential ErrorHandler.Fatal(PreRun...(..), "...")
	if sel, ok := call.Fun.(*dst.SelectorExpr); ok && sel.Sel.Name == "Fatal" {
		if len(call.Args) > 0 {
			if subCall, ok := call.Args[0].(*dst.CallExpr); ok {
				target = subCall.Fun
			}
		}
	}

	return target
}

func (g *Generator) detectAssets(path string, f *dst.File, cmd *ManifestCommand) {
	cmdDir := filepath.Dir(path)
	if exists, _ := afero.DirExists(g.props.FS, filepath.Join(cmdDir, "assets")); exists {
		cmd.WithAssets = true

		return
	}

	for _, decl := range f.Decls {
		if gd, ok := decl.(*dst.GenDecl); ok && gd.Tok == token.VAR {
			for _, spec := range gd.Specs {
				if v, ok := spec.(*dst.ValueSpec); ok {
					for _, s := range v.Decs.Start.All() {
						if strings.Contains(s, "//go:embed assets/*") {
							cmd.WithAssets = true

							return
						}
					}
				}
			}
		}
	}
}

// detectInitializer sets cmd.WithInitializer if an init.go file exists in the
// same directory as path, which is the canonical indicator that the command was
// generated with the Config Initialiser option.
func (g *Generator) detectInitializer(path string, cmd *ManifestCommand) {
	initPath := filepath.Join(filepath.Dir(path), "init.go")
	if exists, _ := afero.Exists(g.props.FS, initPath); exists {
		cmd.WithInitializer = true
	}
}

func extractConstants(f *dst.File) map[string]string {
	constants := make(map[string]string)

	for _, decl := range f.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
			continue
		}

		for _, spec := range gd.Specs {
			vs, ok := spec.(*dst.ValueSpec)
			if !ok {
				continue
			}

			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}

				if val, ok := resolveConstantValue(vs.Values[i]); ok {
					constants[name.Name] = val
				}
			}
		}
	}

	return constants
}

func resolveConstantValue(expr dst.Expr) (string, bool) {
	if lit, ok := expr.(*dst.BasicLit); ok {
		return strings.Trim(lit.Value, "`\""), true
	}

	if call, ok := expr.(*dst.CallExpr); ok {
		if len(call.Args) > 0 {
			if lit, ok := call.Args[0].(*dst.BasicLit); ok {
				return strings.Trim(lit.Value, "`\""), true
			}
		}
	}

	if binary, ok := expr.(*dst.BinaryExpr); ok {
		return evaluateTimeBinaryExpr(binary)
	}

	return "", false
}

func findTargetFunction(f *dst.File, funcName string) *dst.FuncDecl {
	for _, decl := range f.Decls {
		if fn, ok := decl.(*dst.FuncDecl); ok && fn.Name.Name == funcName {
			return fn
		}
	}

	return nil
}

func fallbackFindTargetFunction(f *dst.File) *dst.FuncDecl {
	for _, decl := range f.Decls {
		fn, ok := decl.(*dst.FuncDecl)
		if !ok {
			continue
		}

		if isCobraCommandConstructor(fn) {
			return fn
		}
	}

	return nil
}

func isCobraCommandConstructor(fn *dst.FuncDecl) bool {
	if fn.Type.Results == nil || len(fn.Type.Results.List) == 0 {
		return false
	}

	star, ok := fn.Type.Results.List[0].Type.(*dst.StarExpr)
	if !ok {
		return false
	}

	sel, ok := star.X.(*dst.SelectorExpr)
	if !ok {
		return false
	}

	xid, ok := sel.X.(*dst.Ident)

	return ok && xid.Name == "cobra" && sel.Sel.Name == "Command"
}

func parseImports(f *dst.File) map[string]string {
	imports := make(map[string]string)

	for _, imp := range f.Imports {
		var alias string

		path := strings.Trim(imp.Path.Value, "\"")
		if imp.Name != nil {
			alias = imp.Name.Name
		} else {
			parts := strings.Split(path, "/")
			alias = parts[len(parts)-1]
			alias = strings.ReplaceAll(alias, "-", "_")
			alias = strings.ReplaceAll(alias, ".", "_")
		}

		imports[alias] = path
	}

	return imports
}

func (g *Generator) processCommandBody(targetFunc *dst.FuncDecl, cmd *ManifestCommand, constants map[string]string, path string, imports map[string]string, currentPkgPath string) []string {
	var subcommandFuncs []string

	varToConstructor := make(map[string]string)

	for _, stmt := range targetFunc.Body.List {
		var exprs []dst.Expr

		switch s := stmt.(type) {
		case *dst.AssignStmt:
			exprs = g.processAssignStmt(s, cmd, imports, currentPkgPath, varToConstructor)
		case *dst.ExprStmt:
			exprs = append(exprs, s.X)
		case *dst.DeclStmt:
			exprs = g.processDeclStmt(s, cmd)
		case *dst.ReturnStmt:
			for _, res := range s.Results {
				g.extractFromCobraLiteral(res, cmd)
				exprs = append(exprs, res)
			}
		}

		for _, expr := range exprs {
			if call, ok := expr.(*dst.CallExpr); ok {
				if flag, ok := g.extractFlagFromCall(call, constants, path); ok {
					cmd.Flags = append(cmd.Flags, *flag)
				} else {
					g.extractSubcommandOrMeta(call, cmd, imports, currentPkgPath, varToConstructor, &subcommandFuncs)
				}
			}
		}
	}

	return subcommandFuncs
}

func (g *Generator) processAssignStmt(s *dst.AssignStmt, cmd *ManifestCommand, imports map[string]string, currentPkgPath string, varToConstructor map[string]string) []dst.Expr {
	var exprs = make([]dst.Expr, 0, len(s.Rhs))

	for _, rhs := range s.Rhs {
		g.extractFromCobraLiteral(rhs, cmd)
		exprs = append(exprs, rhs)

		if call, ok := rhs.(*dst.CallExpr); ok {
			funcName, pkgAlias := getCallInfo(call)

			if funcName != "" {
				for _, lhs := range s.Lhs {
					if id, ok := lhs.(*dst.Ident); ok {
						fqName := g.getFullyQualifiedName(funcName, pkgAlias, imports, currentPkgPath)
						varToConstructor[id.Name] = fqName
					}
				}
			}
		}
	}

	return exprs
}

func (g *Generator) processDeclStmt(s *dst.DeclStmt, cmd *ManifestCommand) []dst.Expr {
	var exprs []dst.Expr

	if gd, ok := s.Decl.(*dst.GenDecl); ok && gd.Tok == token.VAR {
		for _, spec := range gd.Specs {
			if vs, ok := spec.(*dst.ValueSpec); ok {
				for _, val := range vs.Values {
					g.extractFromCobraLiteral(val, cmd)
					exprs = append(exprs, val)
				}
			}
		}
	}

	return exprs
}

func getCallInfo(call *dst.CallExpr) (string, string) {
	if id, ok := call.Fun.(*dst.Ident); ok {
		return id.Name, ""
	} else if sel, ok := call.Fun.(*dst.SelectorExpr); ok {
		if x, ok := sel.X.(*dst.Ident); ok {
			return sel.Sel.Name, x.Name
		}

		return sel.Sel.Name, ""
	}

	return "", ""
}

func (g *Generator) getFullyQualifiedName(funcName, pkgAlias string, imports map[string]string, currentPkgPath string) string {
	if pkgAlias != "" {
		if pkgPath, ok := imports[pkgAlias]; ok {
			return fmt.Sprintf("%s.%s", pkgPath, funcName)
		}

		return fmt.Sprintf("%s.%s", pkgAlias, funcName)
	}

	return fmt.Sprintf("%s.%s", currentPkgPath, funcName)
}

func (g *Generator) extractSubcommandOrMeta(call *dst.CallExpr, cmd *ManifestCommand, imports map[string]string, currentPkgPath string, varToConstructor map[string]string, subcommandFuncs *[]string) {
	sel, ok := call.Fun.(*dst.SelectorExpr)
	if !ok {
		return
	}

	target := sel.Sel.Name

	if target == "AddCommand" {
		g.extractAddCommand(call, imports, currentPkgPath, varToConstructor, subcommandFuncs)

		return
	}

	if target == "MarkFlagsMutuallyExclusive" {
		g.extractMarkFlagsMutuallyExclusive(call, cmd)

		return
	}

	if target == "MarkFlagsRequiredTogether" {
		g.extractMarkFlagsRequiredTogether(call, cmd)

		return
	}

	// Handle the generated root pattern: gtbRoot.NewCmdRoot(p, child1.NewCmdX(p), ...)
	// Any NewCmd* call whose arguments (after the first) are themselves NewCmd* calls
	// is registering subcommands, not via AddCommand but as variadic args.
	if strings.HasPrefix(target, "NewCmd") && len(call.Args) > 1 {
		for _, arg := range call.Args[1:] {
			if subCall, ok := arg.(*dst.CallExpr); ok {
				subName, subPkgAlias := getCallInfo(subCall)
				if strings.HasPrefix(subName, "NewCmd") {
					fqName := g.getFullyQualifiedName(subName, subPkgAlias, imports, currentPkgPath)
					*subcommandFuncs = append(*subcommandFuncs, fqName)
				}
			}
		}

		return
	}

	// Handle MarkFlagRequired etc.
	g.extractMarkRequiredOrHidden(call, cmd, target)
}

func (g *Generator) extractAddCommand(call *dst.CallExpr, imports map[string]string, currentPkgPath string, varToConstructor map[string]string, subcommandFuncs *[]string) {
	for _, arg := range call.Args {
		if subCall, ok := arg.(*dst.CallExpr); ok {
			subName, subPkgAlias := getCallInfo(subCall)
			if subName != "" {
				fqName := g.getFullyQualifiedName(subName, subPkgAlias, imports, currentPkgPath)
				*subcommandFuncs = append(*subcommandFuncs, fqName)
			}
		} else if subIdent, ok := arg.(*dst.Ident); ok {
			if funcName, ok := varToConstructor[subIdent.Name]; ok {
				*subcommandFuncs = append(*subcommandFuncs, funcName)
			}
		}
	}
}

func (g *Generator) extractMarkFlagsMutuallyExclusive(call *dst.CallExpr, cmd *ManifestCommand) {
	var group []string

	for _, arg := range call.Args {
		if lit, ok := arg.(*dst.BasicLit); ok {
			group = append(group, strings.Trim(lit.Value, "`\""))
		}
	}

	if len(group) > 0 {
		cmd.MutuallyExclusive = append(cmd.MutuallyExclusive, group)
	}
}

func (g *Generator) extractMarkFlagsRequiredTogether(call *dst.CallExpr, cmd *ManifestCommand) {
	var group []string

	for _, arg := range call.Args {
		if lit, ok := arg.(*dst.BasicLit); ok {
			group = append(group, strings.Trim(lit.Value, "`\""))
		}
	}

	if len(group) > 0 {
		cmd.RequiredTogether = append(cmd.RequiredTogether, group)
	}
}

func (g *Generator) extractMarkRequiredOrHidden(call *dst.CallExpr, cmd *ManifestCommand, target string) {
	if target == "MarkFlagRequired" || target == "MarkPersistentFlagRequired" {
		g.markFlagRequired(call, cmd)

		return
	}

	if target == "MarkFlagHidden" {
		g.markFlagHidden(call, cmd)

		return
	}

	g.markFlagRequiredOrHiddenComplex(call, cmd, target)
}

func (g *Generator) markFlagHidden(call *dst.CallExpr, cmd *ManifestCommand) {
	if len(call.Args) == 0 {
		return
	}

	lit, ok := call.Args[0].(*dst.BasicLit)
	if !ok {
		return
	}

	flagName := strings.Trim(lit.Value, "`\"")
	for i := range cmd.Flags {
		if cmd.Flags[i].Name == flagName {
			cmd.Flags[i].Hidden = true
		}
	}
}

func (g *Generator) markFlagRequired(call *dst.CallExpr, cmd *ManifestCommand) {
	if len(call.Args) == 0 {
		return
	}

	lit, ok := call.Args[0].(*dst.BasicLit)
	if !ok {
		return
	}

	flagName := strings.Trim(lit.Value, "`\"")
	for i := range cmd.Flags {
		if cmd.Flags[i].Name == flagName {
			cmd.Flags[i].Required = true
		}
	}
}

func (g *Generator) markFlagRequiredOrHiddenComplex(call *dst.CallExpr, cmd *ManifestCommand, target string) {
	sel, ok := call.Fun.(*dst.SelectorExpr)
	if !ok {
		return
	}

	subCall, ok := sel.X.(*dst.CallExpr)
	if !ok {
		return
	}

	subSel, ok := subCall.Fun.(*dst.SelectorExpr)
	if !ok || (subSel.Sel.Name != "Flags" && subSel.Sel.Name != "PersistentFlags") {
		return
	}

	if len(call.Args) == 0 {
		return
	}

	lit, ok := call.Args[0].(*dst.BasicLit)
	if !ok {
		return
	}

	flagName := strings.Trim(lit.Value, "`\"")
	g.updateFlagStatus(cmd, flagName, target)
}

func (g *Generator) updateFlagStatus(cmd *ManifestCommand, flagName, target string) {
	for i := range cmd.Flags {
		if cmd.Flags[i].Name == flagName {
			if target == "MarkHidden" {
				cmd.Flags[i].Hidden = true
			} else {
				cmd.Flags[i].Required = true
			}
		}
	}
}

func (g *Generator) parseFlagArgs(call *dst.CallExpr, flag *ManifestFlag, name string, constants map[string]string, cmdPath string) (*ManifestFlag, bool) {
	isVar := strings.Contains(name, "Var")
	isP := strings.HasSuffix(name, "P")

	baseIdx := 0
	if isVar {
		baseIdx = 1
	}

	if len(call.Args) <= baseIdx {
		return nil, false
	}

	flag.Name, _ = resolveStringValue(call.Args[baseIdx], constants)

	g.parseFlagExtras(call, flag, baseIdx, isP, constants, cmdPath)

	// Description is usually at the end
	if len(call.Args) > 0 {
		desc, _ := resolveStringValue(call.Args[len(call.Args)-1], constants)
		flag.Description = MultilineString(desc)
	}

	typeName := strings.TrimSuffix(name, "VarP")
	typeName = strings.TrimSuffix(typeName, "Var")
	typeName = strings.TrimSuffix(typeName, "P")
	flag.Type = strings.ToLower(typeName)

	return flag, true
}

func (g *Generator) parseFlagExtras(call *dst.CallExpr, flag *ManifestFlag, baseIdx int, isP bool, constants map[string]string, cmdPath string) {
	var resolved bool

	if isP {
		if len(call.Args) > baseIdx+1 {
			flag.Shorthand, _ = resolveStringValue(call.Args[baseIdx+1], constants)
		}

		if len(call.Args) > baseIdx+2 {
			flag.Default, resolved = resolveStringValue(call.Args[baseIdx+2], constants)
			if !resolved {
				g.props.Logger.Warn(fmt.Sprintf("Could not resolve default value for flag '%s' in '%s' (value: %s)", flag.Name, cmdPath, flag.Default))
				flag.Warning = "WARNING: could not resolve default value: " + flag.Default
			}
		}

		return
	}

	if len(call.Args) > baseIdx+1 {
		flag.Default, resolved = resolveStringValue(call.Args[baseIdx+1], constants)
		if !resolved {
			g.props.Logger.Warn(fmt.Sprintf("Could not resolve default value for flag '%s' in '%s' (value: %s)", flag.Name, cmdPath, flag.Default))
			flag.Warning = "WARNING: could not resolve default value: " + flag.Default
		}
	}
}

// extractProjectProperties parses pkg/cmd/root/cmd.go and extracts project-level
// metadata (name, description, release source, features) from the &props.Props{Tool: ...}
// composite literal inside the NewCmdRoot constructor.
func (g *Generator) extractProjectProperties(rootCmdPath string) (*ManifestProperties, *ManifestReleaseSource, error) {
	src, err := afero.ReadFile(g.props.FS, rootCmdPath)
	if err != nil {
		return nil, nil, err
	}

	f, err := decorator.Parse(src)
	if err != nil {
		return nil, nil, err
	}

	var targetFunc *dst.FuncDecl

	for _, decl := range f.Decls {
		if fn, ok := decl.(*dst.FuncDecl); ok && fn.Name.Name == "NewCmdRoot" {
			targetFunc = fn
			break
		}
	}

	if targetFunc == nil {
		return nil, nil, errors.New("NewCmdRoot not found in root cmd.go")
	}

	for _, stmt := range targetFunc.Body.List {
		assign, ok := stmt.(*dst.AssignStmt)
		if !ok {
			continue
		}

		for _, rhs := range assign.Rhs {
			mp, rs, propErr := tryExtractPropsLiteral(rhs)
			if propErr == nil && mp != nil {
				return mp, rs, nil
			}
		}
	}

	return nil, nil, errors.New("props.Props literal not found in NewCmdRoot")
}

// tryExtractPropsLiteral attempts to pull ManifestProperties and ManifestReleaseSource
// from a &props.Props{...} composite literal expression.
func tryExtractPropsLiteral(expr dst.Expr) (*ManifestProperties, *ManifestReleaseSource, error) {
	unary, ok := expr.(*dst.UnaryExpr)
	if !ok {
		return nil, nil, errors.New("not a unary expr")
	}

	comp, ok := unary.X.(*dst.CompositeLit)
	if !ok {
		return nil, nil, errors.New("not a composite lit")
	}

	if !isTypeName(comp.Type, "Props") {
		return nil, nil, errors.New("not Props")
	}

	for _, elt := range comp.Elts {
		kv, ok := elt.(*dst.KeyValueExpr)
		if !ok {
			continue
		}

		key, ok := kv.Key.(*dst.Ident)
		if !ok || key.Name != "Tool" {
			continue
		}

		toolComp, ok := kv.Value.(*dst.CompositeLit)
		if !ok {
			return nil, nil, errors.New("Tool value is not a composite lit")
		}

		return extractFromToolLiteral(toolComp)
	}

	return nil, nil, errors.New("Tool field not found in Props literal")
}

func extractFromToolLiteral(comp *dst.CompositeLit) (*ManifestProperties, *ManifestReleaseSource, error) {
	mp := &ManifestProperties{}
	rs := &ManifestReleaseSource{}

	for _, elt := range comp.Elts {
		kv, ok := elt.(*dst.KeyValueExpr)
		if !ok {
			continue
		}

		key, ok := kv.Key.(*dst.Ident)
		if !ok {
			continue
		}

		switch key.Name {
		case "Name":
			if v, ok := stringLitValue(kv.Value); ok {
				mp.Name = v
			}
		case "Description":
			if v, ok := stringLitValue(kv.Value); ok {
				mp.Description = MultilineString(v)
			}
		case "Features":
			mp.Features = extractFeaturesFromSetFeatures(kv.Value)
		case "ReleaseSource":
			if inner, ok := kv.Value.(*dst.CompositeLit); ok {
				extractReleaseSourceLiteral(inner, rs)
			}
		}
	}

	return mp, rs, nil
}

func extractReleaseSourceLiteral(comp *dst.CompositeLit, rs *ManifestReleaseSource) {
	for _, elt := range comp.Elts {
		kv, ok := elt.(*dst.KeyValueExpr)
		if !ok {
			continue
		}

		key, ok := kv.Key.(*dst.Ident)
		if !ok {
			continue
		}

		v, ok := stringLitValue(kv.Value)
		if !ok {
			continue
		}

		switch key.Name {
		case "Type":
			rs.Type = v
		case "Host":
			rs.Host = v
		case "Owner":
			rs.Owner = v
		case "Repo":
			rs.Repo = v
		}
	}
}

// extractFeaturesFromSetFeatures parses props.SetFeatures(props.Enable/Disable(...), ...)
// and returns a ManifestFeature slice. Starts from all-enabled defaults and applies
// each Enable/Disable mutation found in the call arguments.
func extractFeaturesFromSetFeatures(expr dst.Expr) []ManifestFeature {
	featureOrder := []string{"init", "update", "mcp", "docs"}
	enabled := map[string]bool{
		"init":   true,
		"update": true,
		"mcp":    true,
		"docs":   true,
	}

	constToFeature := map[string]string{
		"InitCmd":   "init",
		"UpdateCmd": "update",
		"McpCmd":    "mcp",
		"DocsCmd":   "docs",
	}

	if call, ok := expr.(*dst.CallExpr); ok {
		for _, arg := range call.Args {
			mutCall, ok := arg.(*dst.CallExpr)
			if !ok {
				continue
			}

			sel, ok := mutCall.Fun.(*dst.SelectorExpr)
			if !ok {
				continue
			}

			action := sel.Sel.Name
			if action != "Enable" && action != "Disable" {
				continue
			}

			if len(mutCall.Args) == 0 {
				continue
			}

			var constName string

			switch a := mutCall.Args[0].(type) {
			case *dst.SelectorExpr:
				constName = a.Sel.Name
			case *dst.Ident:
				constName = a.Name
			}

			if feat, ok := constToFeature[constName]; ok {
				enabled[feat] = action == "Enable"
			}
		}
	}

	features := make([]ManifestFeature, 0, len(featureOrder))
	for _, name := range featureOrder {
		features = append(features, ManifestFeature{Name: name, Enabled: enabled[name]})
	}

	return features
}

// isTypeName reports whether expr refers to the given simple type name,
// matching both bare identifiers (Props) and selector expressions (props.Props).
func isTypeName(expr dst.Expr, name string) bool {
	if id, ok := expr.(*dst.Ident); ok {
		return id.Name == name
	}

	if sel, ok := expr.(*dst.SelectorExpr); ok {
		return sel.Sel.Name == name
	}

	return false
}

// stringLitValue extracts the unquoted value from a basic string literal node.
func stringLitValue(expr dst.Expr) (string, bool) {
	lit, ok := expr.(*dst.BasicLit)
	if !ok {
		return "", false
	}

	return strings.Trim(lit.Value, "`\""), true
}
