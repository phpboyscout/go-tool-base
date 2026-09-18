package generator

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/gomod"
	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// frameworkModule is the module every scaffold requires.
const frameworkModule = "gitlab.com/phpboyscout/go-tool-base"

// toolkitPrefix is the estate's module group: every module under it is one
// path element deep, so an import's module is its first three elements.
const toolkitPrefix = "gitlab.com/phpboyscout/go/"

// scaffoldTools are the tool directives every scaffold carries (spec 0194
// D2): the framework's own commands, run through `go tool`.
var scaffoldTools = []string{frameworkModule + "/cmd/changelog", frameworkModule + "/cmd/docs"}

// legacyTools are the tool directives a scaffold carried before spec 0197
// phase 4 for tools that are installed now (D12). A regenerate drops them
// (#86): the gtb line pinned the CLI module out of step with the framework,
// and the other two dragged their dependency graphs into every project.
var legacyTools = []string{
	frameworkModule + "/cli/cmd/gtb",
	"github.com/golangci/golangci-lint/cmd/golangci-lint",
	"github.com/vektra/mockery/v3",
}

// seedGoMod brings the project's go.mod into line with what the generated
// tree imports (spec 0200): a require line for each module the tree's Go
// files import that the running gtb knows a version for, the framework at
// version.gtb, an adapter line dropped when its import has gone, and the
// development replace when GTB_FRAMEWORK_REPLACE asks. Everything else in
// the file is left alone; go mod tidy owns the result where it can run.
func (g *Generator) seedGoMod(projectPath, modulePath, goVersion, frameworkVersion string) error {
	path := filepath.Join(projectPath, "go.mod")

	src, err := afero.ReadFile(g.props.FS, path)
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "read %s", path)
	}

	if g.ignoreRules().IsIgnored("go.mod") {
		g.props.Logger.Debug("ignored by .gtb/ignore, leaving untouched", "path", "go.mod")

		return nil
	}

	imports, err := g.projectImports(projectPath, modulePath)
	if err != nil {
		return err
	}

	// An owned adapter's known version is a floor (spec 0200 D9, #87): the
	// adapters move with the framework, and a line left below what this gtb
	// links surfaces as a compile error inside the adapter after an upgrade.
	// The framework itself is the first floor: the regenerated code is written
	// against the running gtb's version, so a line left below it fails
	// typecheck on the symbols the new templates use.
	want := gomod.LockstepFloors(gomod.Requirements(g.modulesFor(imports), g.versions, gomod.Floors), adapterModules())
	want = append([]gomod.Requirement{{Path: frameworkModule, Version: frameworkVersion, Floor: frameworkVersion != gomod.Latest}}, want...)

	var replace *gomod.Replace
	if dir := frameworkReplace(); dir != "" {
		replace = &gomod.Replace{Path: frameworkModule, Dir: dir}
	}

	out, report, err := gomod.Seed(src, want, replace,
		gomod.Owned(adapterModules()...),
		gomod.WithModule(modulePath, goVersion),
		gomod.WithTools(scaffoldTools...),
		gomod.WithoutTools(legacyTools...))
	if err != nil {
		return errors.Wrap(err, "seed go.mod")
	}

	g.reportSeed(report)

	if err := g.props.FS.MkdirAll(projectPath, DefaultDirMode); err != nil {
		return errors.Wrapf(err, "create %s", projectPath)
	}

	return afero.WriteFile(g.props.FS, path, out, DefaultFileMode)
}

func (g *Generator) reportSeed(report gomod.Report) {
	for _, p := range report.Added {
		g.props.Logger.Info("go.mod: required", "module", p)
	}

	for _, p := range report.Dropped {
		g.props.Logger.Info("go.mod: dropped, nothing imports it now", "module", p)
	}

	for _, r := range report.Raised {
		g.props.Logger.Info("go.mod: raised to the version this gtb requires", "module", r.Path, "from", r.From, "to", r.To)
	}

	for _, p := range report.DroppedTools {
		g.props.Logger.Info("go.mod: dropped a tool directive; the tool is installed now, not run through go tool", "tool", p)
	}

	for _, p := range report.Unpinned {
		g.props.Logger.Info("go.mod: no version known for a module the tree imports; go mod tidy resolves it", "module", p)
	}
}

// projectImports is every import path the project's Go files name outside
// the standard library and the project itself. Test files and files behind
// build tags are included: a superset never drops a module something needs.
func (g *Generator) projectImports(projectPath, modulePath string) ([]string, error) {
	seen := map[string]bool{}

	err := afero.Walk(g.props.FS, projectPath, func(path string, info os.FileInfo, err error) error {
		switch {
		case err != nil:
			return err
		case info.IsDir() && skippedDir(info.Name()):
			return filepath.SkipDir
		case info.IsDir() || !strings.HasSuffix(path, ".go"):
			return nil
		}

		return g.collectExternalImports(path, modulePath, seen)
	})
	if err != nil {
		return nil, errors.Wrap(err, "scan imports")
	}

	return sortedKeys(seen), nil
}

// collectExternalImports adds the file's imports from outside the standard
// library and the project to seen.
func (g *Generator) collectExternalImports(path, modulePath string, seen map[string]bool) error {
	src, err := afero.ReadFile(g.props.FS, path)
	if err != nil {
		return err
	}

	for _, imp := range importsOf(path, src) {
		if !isStdlib(imp) && !isWithin(imp, modulePath) {
			seen[imp] = true
		}
	}

	return nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for p := range m {
		out = append(out, p)
	}

	sort.Strings(out)

	return out
}

func skippedDir(name string) bool {
	return name == "vendor" || name == "testdata" || name == ".git"
}

// importsOf is the import paths of one Go file. A file that does not parse
// contributes none: that is the build's to report, not the seed's.
func importsOf(path string, src []byte) []string {
	f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.ImportsOnly)
	if err != nil {
		return nil
	}

	out := make([]string, 0, len(f.Imports))

	for _, imp := range f.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil {
			out = append(out, p)
		}
	}

	return out
}

// modulesFor maps import paths to the modules that provide them: the
// framework's packages to the framework (seeded separately), an estate
// toolkit package to its two-level module, and anything else to the longest
// prefix the version source knows. An import nothing can place is left to
// tidy.
func (g *Generator) modulesFor(imports []string) []string {
	var modules []string

	for _, imp := range imports {
		var module string

		switch {
		case isWithin(imp, frameworkModule):
			continue
		case strings.HasPrefix(imp, toolkitPrefix):
			rest := strings.TrimPrefix(imp, toolkitPrefix)
			module = toolkitPrefix + strings.SplitN(rest, "/", 2)[0] //nolint:mnd // the module's own element
		default:
			module = g.knownModuleFor(imp)
		}

		if module != "" && !slices.Contains(modules, module) {
			modules = append(modules, module)
		}
	}

	return modules
}

// knownModuleFor shortens an import path until the version source knows it as
// a module; "" when nothing does.
func (g *Generator) knownModuleFor(imp string) string {
	for p := imp; p != "" && strings.Contains(p, "/"); p = p[:strings.LastIndex(p, "/")] {
		if _, ok := g.versions.Version(p); ok {
			return p
		}
	}

	return ""
}

// adapterModules is every module the adapter tables can link: the lines the
// generator writes for an enabled forge or provider, and so the only lines it
// drops when that forge or provider goes (spec 0200 D3).
func adapterModules() []string {
	var modules []string

	for _, entry := range chat.ProviderModules() {
		if !slices.Contains(modules, entry.Module) {
			modules = append(modules, entry.Module)
		}
	}

	for _, d := range forge.Displays() {
		if m, ok := forge.ModuleFor(string(d.ID)); ok && !slices.Contains(modules, m) {
			modules = append(modules, m)
		}
	}

	return modules
}

func isStdlib(imp string) bool {
	first, _, _ := strings.Cut(imp, "/")

	return !strings.Contains(first, ".")
}

func isWithin(imp, module string) bool {
	return imp == module || strings.HasPrefix(imp, module+"/")
}
