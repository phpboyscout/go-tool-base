package generator

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
)

// writeVerb returns the present-participle verb for a generation write
// ("Writing"), or the conditional "Would write" during a dry run so the
// preview's logs don't imply files were actually changed.
func (g *Generator) writeVerb() string {
	if g.config.DryRun {
		return "Would write"
	}

	return "Writing"
}

func (g *Generator) generateAssetFiles(cmdDir string) error {
	// The init template: written to the user's config file by setup.Initialise.
	if err := g.seedAssetFile(
		filepath.Join(cmdDir, "assets", "init", "config.yaml"),
		fmt.Sprintf("%s:\n", g.config.Name),
	); err != nil {
		return err
	}

	// The defaults document: merged into the tool's embedded-defaults layer,
	// where keys always apply. Seeded as commentary so scaffolding does not
	// inject a null section into every tool's resolved configuration.
	return g.seedAssetFile(
		filepath.Join(cmdDir, "assets", "config.yaml"),
		fmt.Sprintf(
			"# Baseline defaults for %[1]s, merged into the tool's embedded-defaults\n"+
				"# layer. Keys here always apply; user config, env vars and flags override\n"+
				"# them.\n"+
				"# %[1]s:\n"+
				"#   example_key: value\n",
			g.config.Name,
		),
	)
}

// seedAssetFile writes content to path unless the file already exists.
func (g *Generator) seedAssetFile(path, content string) error {
	if err := g.props.FS.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return errors.Newf("failed to create asset directory: %w", err)
	}

	exists, err := afero.Exists(g.props.FS, path)
	if err != nil {
		return errors.Newf("failed to check for config file: %w", err)
	}

	if exists {
		g.props.Logger.Warn("config file already exists, skipping creation", "path", path)

		return nil
	}

	f, err := g.props.FS.Create(path)
	if err != nil {
		return errors.Newf("failed to create config file: %w", err)
	}

	if _, err := f.WriteString(content); err != nil {
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

	// Decided BEFORE cmd.go is rendered, because cmd.go is what would carry the
	// dangling reference. A sealed main.go that is absent can be neither created
	// nor stubbed, so Run<Name> cannot be made to exist and cmd.go must not call
	// it — otherwise the package stops compiling, which is the outcome
	// wiringSealed exists to avoid.
	data.OmitRunWiring = g.runTargetUnreachable(cmdDir)

	g.props.Logger.Info(fmt.Sprintf("%s registration file: %s", g.writeVerb(), filepath.Join(cmdDir, "cmd.go")))

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

	if err := g.handleConfigValidationFile(ctx, cmdDir, data); err != nil {
		return err
	}

	if data.TestCode != "" {
		g.props.Logger.Info(fmt.Sprintf("%s test file: %s", g.writeVerb(), filepath.Join(cmdDir, "main_test.go")))

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

	g.props.Logger.Debug("rendering registration template", "path", cmdPath)

	regFile := templates.CommandRegistration(data)

	var buf bytes.Buffer
	if err := regFile.Render(&buf); err != nil {
		return "", errors.Newf("failed to render registration file: %w", err)
	}

	content := buf.Bytes()
	newHash := calculateHash(content)

	if decision := g.resolveCommandFileConflict(cmdPath, content); !decision.Write() {
		// Kept or ignored: leave the file alone and carry on. The recorded
		// hash is the resolver's, not this render's — see D3/D4 of spec 0187.
		return decision.RecordHash, nil
	}

	g.props.Logger.Debug("writing registration file", "path", cmdPath, "bytes", len(content), "hash", newHash)

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

// runTargetUnreachable reports whether Run<Name> can never exist for this
// command: main.go is absent and a seal forbids creating it.
//
// A plain ignore rule is not this. That rule still allows creation precisely so
// the reference stays resolvable (spec 0188 D2), so only a seal — where the
// developer has stated the file is untouchable — leaves nothing to call.
func (g *Generator) runTargetUnreachable(cmdDir string) bool {
	mainFile := filepath.Join(cmdDir, "main.go")

	if exists, _ := afero.Exists(g.props.FS, mainFile); exists {
		return false
	}

	return g.ignoreRules().IsSealed(g.relProjectPath(mainFile))
}

func (g *Generator) handleExecutionFile(ctx context.Context, cmdDir string, data *templates.CommandData) error {
	mainFile := filepath.Join(cmdDir, "main.go")

	// A seal forbids every write to the path, creation included (0188 D3). The
	// preserve branch below already checks this inside ensureHookStubs; without
	// the same check here, sealing a main.go and deleting it got the file
	// recreated on the next run, and `ignore check` reported `sealed` while the
	// generator wrote anyway (issue #17).
	//
	// wiringSealed rather than IsIgnored, deliberately: 0188 D2 lets a *plain*
	// ignore rule through here, because the rendered cmd.go references a RunX
	// this file defines and refusing to write it is a hard compile error. Only
	// the explicit `sealed` attribute — where the developer has accepted that
	// consequence — stops it, and the refusal is recorded so the end-of-run
	// summary names it (D6).
	if g.wiringSealed(mainFile, "creating the execution file") {
		return nil
	}

	exists, _ := afero.Exists(g.props.FS, mainFile)
	if !exists || g.config.Force {
		g.props.Logger.Info(fmt.Sprintf("%s execution file: %s", g.writeVerb(), mainFile))

		return g.generateExecutionFile(ctx, cmdDir, *data)
	}

	// main.go is being preserved — inject any hook stubs that the options
	// require but that don't yet exist in the file.
	return g.ensureHookStubs(ctx, mainFile, *data)
}

func (g *Generator) handleInitializerFile(cmdDir string, data *templates.CommandData) error {
	initFile := filepath.Join(cmdDir, "init.go")

	if data.WithInitializer {
		g.props.Logger.Info(fmt.Sprintf("%s initializer file: %s", g.writeVerb(), initFile))

		hash, err := g.generateInitializerFile(cmdDir, *data)
		if err != nil {
			return err
		}

		data.Hashes["init.go"] = hash

		return nil
	}

	if exists, _ := afero.Exists(g.props.FS, initFile); exists {
		g.props.Logger.Info("removing initializer file", "path", initFile)

		if err := g.props.FS.Remove(initFile); err != nil {
			return errors.Newf("failed to remove initializer file: %w", err)
		}

		delete(data.Hashes, "init.go")
	}

	return nil
}

func (g *Generator) handleConfigValidationFile(ctx context.Context, cmdDir string, data *templates.CommandData) error {
	configFile := filepath.Join(cmdDir, "config.go")

	if !data.WithConfigValidation {
		if exists, _ := afero.Exists(g.props.FS, configFile); exists {
			g.props.Logger.Warn(fmt.Sprintf("Config validation file %s exists but with_config_validation is disabled — consider removing it or re-enabling the flag", configFile))
		}

		return nil
	}

	exists, _ := afero.Exists(g.props.FS, configFile)
	if exists {
		g.props.Logger.Debug("config validation file already exists, preserving user customisations", "path", configFile)

		return nil
	}

	g.props.Logger.Info(fmt.Sprintf("%s config validation file: %s", g.writeVerb(), configFile))

	content := templates.CommandConfigValidation(*data)

	out, err := g.props.FS.Create(configFile)
	if err != nil {
		return errors.Newf("failed to create config validation file: %w", err)
	}

	defer func() {
		_ = out.Close()
	}()

	if _, err := out.WriteString(content); err != nil {
		return errors.Newf("failed to write config validation file: %w", err)
	}

	if _, ok := g.props.FS.(*afero.OsFs); ok {
		cmd := exec.CommandContext(ctx, "go", "fmt", configFile)
		_ = cmd.Run()
	}

	return nil
}

func (g *Generator) generateExecutionFile(ctx context.Context, cmdDir string, data templates.CommandData) error {
	mainPath := filepath.Join(cmdDir, "main.go")

	g.props.Logger.Debug("rendering execution template", "path", mainPath)

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

	g.props.Logger.Debug("wrote execution file", "path", mainPath, "bytes", len(mainContent))

	// Run go fmt on main.go if using OS filesystem
	if _, ok := g.props.FS.(*afero.OsFs); ok {
		g.props.Logger.Debug("running go fmt", "path", mainPath)

		cmd := exec.CommandContext(ctx, "go", "fmt", mainPath)
		_ = cmd.Run()
	}

	return nil
}

func (g *Generator) generateInitializerFile(cmdDir string, data templates.CommandData) (string, error) {
	cmdPath := filepath.Join(cmdDir, "init.go")

	g.props.Logger.Debug("rendering initializer template", "path", cmdPath)

	initFile := templates.CommandInitializer(data)

	var buf bytes.Buffer
	if err := initFile.Render(&buf); err != nil {
		return "", errors.Newf("failed to render initializer file: %w", err)
	}

	content := buf.Bytes()

	if decision := g.resolveCommandFileConflict(cmdPath, content); !decision.Write() {
		return decision.RecordHash, nil
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

	g.props.Logger.Debug("wrote initializer file", "path", cmdPath, "bytes", len(content), "hash", hash)

	return hash, nil
}

func (g *Generator) generateTestFile(ctx context.Context, cmdDir string, data templates.CommandData) (string, error) {
	if data.TestCode == "" {
		g.props.Logger.Debug("No test code provided, skipping test file generation")

		return "", nil
	}

	testPath := filepath.Join(cmdDir, "main_test.go")

	g.props.Logger.Debug("generating test file", "path", testPath)

	if decision := g.resolveCommandFileConflict(testPath, []byte(data.TestCode)); !decision.Write() {
		return decision.RecordHash, nil
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

	g.props.Logger.Debug("wrote test file", "path", testPath, "bytes", len(data.TestCode))

	// Run go fmt on main_test.go if using OS filesystem
	if _, ok := g.props.FS.(*afero.OsFs); ok {
		g.props.Logger.Debug("running go fmt", "path", testPath)

		cmd := exec.CommandContext(ctx, "go", "fmt", testPath)
		_ = cmd.Run()
	}

	return calculateHash([]byte(data.TestCode)), nil
}
