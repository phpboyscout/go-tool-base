package generator

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cockroachdb/errors"
	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/spf13/afero"

	"github.com/phpboyscout/go-tool-base/internal/generator/templates"
)

func (g *Generator) generateAssetFiles(cmdDir string) error {
	assetDir := filepath.Join(cmdDir, "assets", "init")

	if err := g.props.FS.MkdirAll(assetDir, os.ModePerm); err != nil {
		return errors.Newf("failed to create asset directory: %w", err)
	}

	configPath := filepath.Join(assetDir, "config.yaml")

	exists, err := afero.Exists(g.props.FS, configPath)
	if err != nil {
		return errors.Newf("failed to check for config file: %w", err)
	}

	if exists {
		g.props.Logger.Warnf("Config file %s already exists, skipping creation", configPath)

		return nil
	}

	f, err := g.props.FS.Create(configPath)
	if err != nil {
		return errors.Newf("failed to create config file: %w", err)
	}

	if _, err := fmt.Fprintf(f, "%s:\n", g.config.Name); err != nil {
		_ = f.Close()

		return errors.Newf("failed to write config file: %w", err)
	}

	if err := f.Close(); err != nil {
		return errors.Newf("failed to close config file: %w", err)
	}

	return nil
}

func (g *Generator) GenerateCommandFile(ctx context.Context, cmdDir string, data *templates.CommandData) error {
	data.Hashes = make(map[string]string)

	g.props.Logger.Infof("Writing registration file: %s", filepath.Join(cmdDir, "cmd.go"))

	hash, err := g.generateRegistrationFile(cmdDir, *data)
	if err != nil {
		return err
	}

	data.Hashes["cmd.go"] = hash

	if err := g.handleExecutionFile(ctx, cmdDir, data); err != nil {
		return err
	}

	if err := g.handleInitializerFile(cmdDir, data); err != nil {
		return err
	}

	if data.TestCode != "" {
		g.props.Logger.Infof("Writing test file: %s", filepath.Join(cmdDir, "main_test.go"))

		hash, err := g.generateTestFile(ctx, cmdDir, *data)
		if err != nil {
			return err
		}

		data.Hashes["main_test.go"] = hash
	}

	return nil
}

func (g *Generator) generateRegistrationFile(cmdDir string, data templates.CommandData) (string, error) {
	cmdPath := filepath.Join(cmdDir, "cmd.go")

	g.props.Logger.Debugf("Rendering registration template for %s", cmdPath)

	regFile := templates.CommandRegistration(data)

	var buf bytes.Buffer
	if err := regFile.Render(&buf); err != nil {
		return "", errors.Newf("failed to render registration file: %w", err)
	}

	content := buf.Bytes()
	newHash := calculateHash(content)

	// Check if file exists to perform hash verification
	if exists, _ := afero.Exists(g.props.FS, cmdPath); exists {
		g.props.Logger.Debugf("Verifying hash for existing file %s", cmdPath)

		if err := g.verifyHash(cmdPath); err != nil {
			return "", err
		}
	}

	g.props.Logger.Debugf("Writing registration file: %s (%d bytes, hash=%s)", cmdPath, len(content), newHash)

	out, err := g.props.FS.Create(cmdPath)
	if err != nil {
		return "", errors.Newf("failed to create registration file: %w", err)
	}

	defer func() {
		_ = out.Close()
	}()

	if _, err := out.Write(content); err != nil {
		return "", errors.Newf("failed to write registration file: %w", err)
	}

	return newHash, nil
}

func (g *Generator) handleExecutionFile(ctx context.Context, cmdDir string, data *templates.CommandData) error {
	mainFile := filepath.Join(cmdDir, "main.go")

	exists, _ := afero.Exists(g.props.FS, mainFile)
	if !exists || g.config.Force {
		g.props.Logger.Infof("Writing execution file: %s", mainFile)

		return g.generateExecutionFile(ctx, cmdDir, *data)
	}

	// main.go is being preserved — inject any hook stubs that the options
	// require but that don't yet exist in the file.
	return g.ensureHookStubs(ctx, mainFile, *data)
}

func (g *Generator) handleInitializerFile(cmdDir string, data *templates.CommandData) error {
	initFile := filepath.Join(cmdDir, "init.go")

	if data.WithInitializer {
		g.props.Logger.Infof("Writing initializer file: %s", initFile)

		hash, err := g.generateInitializerFile(cmdDir, *data)
		if err != nil {
			return err
		}

		data.Hashes["init.go"] = hash

		return nil
	}

	if exists, _ := afero.Exists(g.props.FS, initFile); exists {
		g.props.Logger.Infof("Removing initializer file: %s", initFile)

		if err := g.props.FS.Remove(initFile); err != nil {
			return errors.Newf("failed to remove initializer file: %w", err)
		}

		delete(data.Hashes, "init.go")
	}

	return nil
}

func (g *Generator) generateExecutionFile(ctx context.Context, cmdDir string, data templates.CommandData) error {
	mainPath := filepath.Join(cmdDir, "main.go")

	g.props.Logger.Debugf("Rendering execution template for %s", mainPath)

	mainContent := templates.CommandExecution(data)

	out, err := g.props.FS.Create(mainPath)
	if err != nil {
		return errors.Newf("failed to create execution file: %w", err)
	}

	defer func() {
		_ = out.Close()
	}()

	if _, err := out.WriteString(mainContent); err != nil {
		return errors.Newf("failed to write execution file: %w", err)
	}

	g.props.Logger.Debugf("Wrote execution file: %s (%d bytes)", mainPath, len(mainContent))

	// Run go fmt on main.go if using OS filesystem
	if _, ok := g.props.FS.(*afero.OsFs); ok {
		g.props.Logger.Debugf("Running go fmt on %s", mainPath)

		cmd := exec.CommandContext(ctx, "go", "fmt", mainPath)
		_ = cmd.Run()
	}

	return nil
}

func (g *Generator) generateInitializerFile(cmdDir string, data templates.CommandData) (string, error) {
	cmdPath := filepath.Join(cmdDir, "init.go")

	g.props.Logger.Debugf("Rendering initializer template for %s", cmdPath)

	initFile := templates.CommandInitializer(data)

	var buf bytes.Buffer
	if err := initFile.Render(&buf); err != nil {
		return "", errors.Newf("failed to render initializer file: %w", err)
	}

	content := buf.Bytes()

	// Check if file exists to perform hash verification
	if exists, _ := afero.Exists(g.props.FS, cmdPath); exists {
		g.props.Logger.Debugf("Verifying hash for existing file %s", cmdPath)

		if err := g.verifyHash(cmdPath); err != nil {
			return "", err
		}
	}

	out, err := g.props.FS.Create(cmdPath)
	if err != nil {
		return "", errors.Newf("failed to create initializer file: %w", err)
	}

	defer func() { _ = out.Close() }()

	if _, err := out.Write(content); err != nil {
		return "", errors.Newf("failed to write initializer file: %w", err)
	}

	hash := calculateHash(content)

	g.props.Logger.Debugf("Wrote initializer file: %s (%d bytes, hash=%s)", cmdPath, len(content), hash)

	return hash, nil
}

func (g *Generator) generateTestFile(ctx context.Context, cmdDir string, data templates.CommandData) (string, error) {
	if data.TestCode == "" {
		g.props.Logger.Debug("No test code provided, skipping test file generation")

		return "", nil
	}

	testPath := filepath.Join(cmdDir, "main_test.go")

	g.props.Logger.Debugf("Generating test file: %s", testPath)

	// Check if file exists to perform hash verification
	if exists, _ := afero.Exists(g.props.FS, testPath); exists {
		g.props.Logger.Debugf("Verifying hash for existing file %s", testPath)

		if err := g.verifyHash(testPath); err != nil {
			return "", err
		}
	}

	out, err := g.props.FS.Create(testPath)
	if err != nil {
		return "", errors.Newf("failed to create test file: %w", err)
	}

	defer func() {
		_ = out.Close()
	}()

	if _, err := out.WriteString(data.TestCode); err != nil {
		return "", errors.Newf("failed to write test file: %w", err)
	}

	g.props.Logger.Debugf("Wrote test file: %s (%d bytes)", testPath, len(data.TestCode))

	// Run go fmt on main_test.go if using OS filesystem
	if _, ok := g.props.FS.(*afero.OsFs); ok {
		g.props.Logger.Debugf("Running go fmt on %s", testPath)

		cmd := exec.CommandContext(ctx, "go", "fmt", testPath)
		_ = cmd.Run()
	}

	return calculateHash([]byte(data.TestCode)), nil
}

func (g *Generator) shouldOmitRun(data templates.CommandData, cmdDir string) bool {
	// Only consider omitting Run for commands with subcommands
	if !data.HasSubcommands {
		return false
	}

	// If we are currently generating logic via script or prompt, do NOT omit Run
	if g.config.ScriptPath != "" || g.config.Prompt != "" {
		return false
	}

	// Check if main.go exists
	mainPath := filepath.Join(cmdDir, "main.go")

	fsrc, err := afero.ReadFile(g.props.FS, mainPath)
	if err != nil {
		// If it doesn't exist, it's a new command with subcommands,
		// so it will get the default logic. In this case, we SHOULD omit Run.
		return true
	}

	// Parse main.go to check for custom logic in the Run function
	f, err := decorator.Parse(fsrc)
	if err != nil {
		// If we can't parse it, assume it has custom logic (play it safe)
		return false
	}

	runFuncName := "Run" + data.PascalName
	for _, decl := range f.Decls {
		if fn, ok := decl.(*dst.FuncDecl); ok && fn.Name.Name == runFuncName {
			return checkRunFunc(fn)
		}
	}

	return false
}

func checkRunFunc(fn *dst.FuncDecl) bool {
	// If the function has more than one statement, it's custom
	if len(fn.Body.List) != 1 {
		return false
	}

	// If it's a return statement, check what it returns
	ret, ok := fn.Body.List[0].(*dst.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}

	sel, ok := ret.Results[0].(*dst.SelectorExpr)
	if !ok {
		return false
	}

	x, ok := sel.X.(*dst.Ident)
	if !ok || x.Name != "errorhandling" {
		return false
	}

	return sel.Sel.Name == "ErrNotImplemented" || sel.Sel.Name == "ErrRunSubCommand"
}
