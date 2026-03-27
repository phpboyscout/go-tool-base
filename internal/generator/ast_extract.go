package generator

import (
	"fmt"
	"go/token"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/spf13/afero"
)

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
	g.detectConfigValidation(path, cmd)

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

// detectConfigValidation sets cmd.WithConfigValidation if a config.go file
// exists in the same directory as path, indicating the command was generated
// with the Config Validation option.
func (g *Generator) detectConfigValidation(path string, cmd *ManifestCommand) {
	configPath := filepath.Join(filepath.Dir(path), "config.go")
	if exists, _ := afero.Exists(g.props.FS, configPath); exists {
		cmd.WithConfigValidation = true
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

	targetFunc := findFuncDecl(f, "NewCmdRoot")
	if targetFunc == nil {
		return nil, nil, errors.New("NewCmdRoot not found in root cmd.go")
	}

	return findPropsLiteralInFunc(targetFunc)
}

// findFuncDecl returns the first function declaration with the given name, or nil.
func findFuncDecl(f *dst.File, name string) *dst.FuncDecl {
	for _, decl := range f.Decls {
		if fn, ok := decl.(*dst.FuncDecl); ok && fn.Name.Name == name {
			return fn
		}
	}

	return nil
}

// findPropsLiteralInFunc walks assignment statements in fn looking for a
// &props.Props{...} composite literal and extracts project properties from it.
func findPropsLiteralInFunc(fn *dst.FuncDecl) (*ManifestProperties, *ManifestReleaseSource, error) {
	for _, stmt := range fn.Body.List {
		assign, ok := stmt.(*dst.AssignStmt)
		if !ok {
			continue
		}

		for _, rhs := range assign.Rhs {
			mp, rs, err := tryExtractPropsLiteral(rhs)
			if err == nil && mp != nil {
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

		applyToolField(mp, rs, key.Name, kv.Value)
	}

	return mp, rs, nil
}

func applyToolField(mp *ManifestProperties, rs *ManifestReleaseSource, fieldName string, value dst.Expr) {
	switch fieldName {
	case "Name":
		if v, ok := stringLitValue(value); ok {
			mp.Name = v
		}
	case "Description":
		if v, ok := stringLitValue(value); ok {
			mp.Description = MultilineString(v)
		}
	case "Features":
		mp.Features = extractFeaturesFromSetFeatures(value)
	case "ReleaseSource":
		if inner, ok := value.(*dst.CompositeLit); ok {
			extractReleaseSourceLiteral(inner, rs)
		}
	}
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
			applyFeatureMutation(arg, constToFeature, enabled)
		}
	}

	features := make([]ManifestFeature, 0, len(featureOrder))
	for _, name := range featureOrder {
		features = append(features, ManifestFeature{Name: name, Enabled: enabled[name]})
	}

	return features
}

// applyFeatureMutation inspects a single argument to props.SetFeatures and,
// if it is an Enable/Disable call, updates the enabled map accordingly.
func applyFeatureMutation(arg dst.Expr, constToFeature map[string]string, enabled map[string]bool) {
	mutCall, ok := arg.(*dst.CallExpr)
	if !ok {
		return
	}

	sel, ok := mutCall.Fun.(*dst.SelectorExpr)
	if !ok {
		return
	}

	action := sel.Sel.Name
	if action != "Enable" && action != "Disable" {
		return
	}

	if len(mutCall.Args) == 0 {
		return
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
