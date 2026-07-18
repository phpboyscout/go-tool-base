package generator

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
)

// ensureHookStubs appends any missing hook function stubs to an existing main.go.
// It is called when main.go is being preserved (no --force) so that enabling
// PersistentPreRun, PreRun, or WithInitializer on a subsequent generate doesn't
// silently leave the required function absent. Existing code is never modified or
// removed. Any extra imports required by injected stubs are also inserted.
func (g *Generator) ensureHookStubs(ctx context.Context, mainPath string, data templates.CommandData) error {
	src, err := afero.ReadFile(g.props.FS, mainPath)
	if err != nil {
		return errors.Newf("failed to read %s: %w", mainPath, err)
	}

	type hookSpec struct {
		enabled         bool
		funcName        string
		stub            func() string
		requiredImports []string
	}

	hooks := []hookSpec{
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
				"gitlab.com/phpboyscout/go/config",
			},
			stub: func() string {
				return fmt.Sprintf(
					"\nfunc Init%s(p *props.Props, cfg config.Containable) error {\n"+
						"\t// TODO: Implement custom initialization logic for %s\n"+
						"\treturn nil\n"+
						"}\n",
					data.PascalName, data.Name,
				)
			},
		},
	}

	content := string(src)
	appended := false

	for _, h := range hooks {
		if !h.enabled {
			continue
		}

		if strings.Contains(content, "func "+h.funcName+"(") {
			continue
		}

		g.props.Logger.Info("appending missing stub", "func", h.funcName, "path", mainPath)

		for _, imp := range h.requiredImports {
			content = ensureImport(content, imp)
		}

		content += h.stub()
		appended = true
	}

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
