package generator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/cockroachdb/errors"
	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"github.com/phpboyscout/gtb/internal/generator/templates"
	"github.com/phpboyscout/gtb/internal/generator/verifier"
	"github.com/phpboyscout/gtb/pkg/chat"
)

var commandGenerationSystemPrompt = `PHASE 1 (Generation/Conversion):
If provided with a script, convert it into a COMPLETE, valid, safe, and secure Go file.
If provided with a description, implement the described functionality from scratch in a COMPLETE, valid, safe, and secure Go file.
The code will act as the body of a cobra command.

PHASE 2 (Fixing):
If provided with LINTER ERRORS, fix the code accordingly. If provided with TEST ERRORS, fix the code accordingly.

CONTEXT:
- You are writing a file for package '%s'.
- You must export a function with signature: func %s(ctx context.Context, props *props.Props, opts *%s, args []string) error
- 'props' provides the application context:
  - props.Logger (*log.Logger): Structured logger.
  - props.FS (afero.Fs): File system interface.
  - props.Config: Application configuration.
- 'opts' contains flags defined for the command.
- 'args' contains positional arguments.

FLAGS IN OPTS:
The '%s' struct ONLY has fields corresponding to these flags:
%s

REQUIREMENTS:
- Generate the ENTIRE file content, including 'package' declaration and 'import' block.
- You MUST import "context" and "github.com/phpboyscout/gtb/pkg/props".
- You can optionally import other packages if needed (e.g., standard library), but consolidated imports are preferred.
- Use 'github.com/cockroachdb/errors' for ALL error handling (aliased as 'errors').
- Define any helper functions as unexported top-level functions in the same file.
- DO NOT define the options struct '%s'. It is already defined in another file.
- DO NOT access fields in 'opts' that are not listed above. If the script uses variables that are not flags, define them as local variables.
- DO NOT use 'fmt' for logging. Use 'props.Logger' instead.
- DO NOT use the exec command to run external commands, the converted code should be self contained, using only go code.
- Use explicit error handling.
- Use structured logging: props.Logger.Info("msg", "key", "val").
- You must also generate a unit test file for the converted code.
- The test file MUST use the black-box testing pattern (package name ends in '_test', e.g., '%s_test').
- You MUST import the package under test (import path: '%s').
- You MUST import "github.com/phpboyscout/gtb/pkg/props" if you reference 'props.Props' in the test.
- You MUST reference functions and structs from the package under test (e.g., '%s.%s').
- If mocking is required for props.Config, use the shared mocks in 'github.com/phpboyscout/gtb/mocks/pkg/config'.
- For the Logger ('props.Logger'), DO NOT use a mock. Instead, use an actual implementation:
- Import "github.com/charmbracelet/log" and "io".
- Initialize it with: logger := log.New(io.Discard).
- For the FS ('props.FS'), DO NOT use a mock. Instead, use an actual implementation:
- Import "github.com/spf13/afero".
- Initialize it with: fs := afero.NewMemMapFs().
- If the supplied code provided is not convertible or appears broken, provide recommendations for how to convert it.
`

func (g *Generator) Generate(ctx context.Context) error {
	if err := g.verifyProject(); err != nil {
		return err
	}

	cmdDir, err := g.prepareAndVerify()
	if err != nil {
		if errors.Is(err, ErrCommandProtected) {
			return nil
		}

		return err
	}

	g.props.Logger.Infof("Generating command %s in %s...", g.config.Name, cmdDir)

	flags := g.resolveGenerationFlags()
	data := g.prepareGenerationData(flags)

	if err := g.checkExistingMain(cmdDir); err != nil {
		return err
	}

	aiClient := g.processAIGeneration(ctx, &data, flags)

	if err := g.performGeneration(ctx, cmdDir, &data); err != nil {
		return err
	}

	if aiClient != nil {
		if err := g.verifyAndFixProject(ctx, cmdDir, &data, aiClient); err != nil {
			return err
		}
	}

	return g.finalizeProject(ctx, data, cmdDir)
}

func (g *Generator) prepareAndVerify() (string, error) {
	if g.config.Name == "root" {
		return "", errors.New("cannot create a command named 'root'")
	}

	cmdDir, err := g.getCommandPath()
	if err != nil {
		return "", err
	}

	if err := g.checkProtection(); err != nil {
		if errors.Is(err, ErrCommandProtected) {
			g.props.Logger.Warn("Command is protected. Skipping generation.")

			return "", ErrCommandProtected
		}

		return "", err
	}

	return cmdDir, nil
}

func (g *Generator) finalizeProject(ctx context.Context, data templates.CommandData, cmdDir string) error {
	if err := g.postGenerate(ctx, data, cmdDir); err != nil {
		return err
	}

	g.props.Logger.Infof("Successfully generated command %s in %s.", g.config.Name, cmdDir)

	return nil
}

var ErrCommandProtected = errors.New("command is protected")

func (g *Generator) checkProtection() error {
	// We need to check if the command already exists in the manifest
	// and if it is protected.

	// findManifestCommand returns error if not found.
	cmd, err := g.findManifestCommand()

	isProtected := err == nil && cmd != nil && cmd.Protected != nil && *cmd.Protected

	// Case 3: Protected=nil (Default)
	if g.config.Protected == nil {
		if isProtected {
			return ErrCommandProtected
		}

		return nil
	}

	// Case 1: Protected=true
	if *g.config.Protected {
		if isProtected {
			return errors.Newf("command %s is already protected", g.config.Name)
		}
		// Proceed. Will be marked protected in postGenerate via updateManifest.
		return nil
	}

	// Case 2: Protected=false
	// Check if we need to explicitly unprotect before generation?
	// postGenerate will update manifest with Protected=false (from config).
	// So we just proceed.
	return nil
}

func (g *Generator) resolveGenerationFlags() []CommandFlag {
	flags := g.parseFlags()
	if len(flags) == 0 {
		manifestFlags, err := g.loadFlagsFromManifest()
		if err == nil && len(manifestFlags) > 0 {
			return manifestFlags
		}
	}

	return flags
}

func (g *Generator) checkExistingMain(cmdDir string) error {
	mainFile := filepath.Join(cmdDir, "main.go")

	exists, err := afero.Exists(g.props.FS, mainFile)
	if err != nil {
		return err
	}

	if exists && !g.config.Force {
		g.props.Logger.Warnf("%s already exists and will not be overwritten. Use --force to overwrite.", mainFile)
	}

	return nil
}

func (g *Generator) processAIGeneration(ctx context.Context, data *templates.CommandData, flags []CommandFlag) chat.ChatClient {
	if g.config.ScriptPath == "" && g.config.Prompt == "" {
		return nil
	}

	aiClient, err := g.handleAIGeneration(ctx, data, flags)
	if err != nil {
		g.props.Logger.Warnf("AI generation failed: %v. Using best effort placeholder.", err)
		data.Logic = "// AI generation failed: " + err.Error() + "\nreturn nil"
	}

	return aiClient
}

func (g *Generator) prepareGenerationData(flags []CommandFlag) templates.CommandData {
	hasSubcommands := false

	persistentFlags, normalFlags := g.categorizeFlags(flags)
	ancestralPersistentFlags := g.resolveAncestralFlags()

	aliases := g.config.Aliases
	desc := g.config.Short
	longDesc := g.config.Long
	args := g.config.Args

	if cmd, err := g.findManifestCommand(); err == nil {
		hasSubcommands = len(cmd.Commands) > 0
		if len(aliases) == 0 {
			aliases = cmd.Aliases
		}

		if desc == "" {
			desc = string(cmd.Description)
		}

		if longDesc == "" {
			longDesc = string(cmd.LongDescription)
		}

		if args == "" {
			args = cmd.Args
		}
	}

	data := templates.CommandData{
		Name:                     g.config.Name,
		PascalName:               PascalCase(g.config.Name),
		Short:                    desc,
		Long:                     longDesc,
		Aliases:                  aliases,
		Args:                     args,
		Package:                  strings.ReplaceAll(g.config.Name, "-", "_"),
		WithAssets:               g.config.WithAssets,
		PersistentPreRun:         g.config.PersistentPreRun,
		PreRun:                   g.config.PreRun,
		Flags:                    normalFlags,
		PersistentFlags:          persistentFlags,
		AncestralPersistentFlags: ancestralPersistentFlags,
		HasSubcommands:           hasSubcommands,
		WithInitializer:          g.config.WithInitializer,
	}

	if cmd, err := g.findManifestCommand(); err == nil {
		data.MutuallyExclusive = cmd.MutuallyExclusive
		data.RequiredTogether = cmd.RequiredTogether
	}

	return data
}

func (g *Generator) categorizeFlags(flags []CommandFlag) ([]templates.CommandFlag, []templates.CommandFlag) {
	var persistentFlags, normalFlags []templates.CommandFlag

	parsedFlags := g.convertFlagsToTemplate(flags)
	for _, f := range parsedFlags {
		if f.Persistent {
			persistentFlags = append(persistentFlags, f)
		} else {
			normalFlags = append(normalFlags, f)
		}
	}

	return persistentFlags, normalFlags
}

func (g *Generator) resolveAncestralFlags() []templates.CommandFlag {
	var ancestralPersistentFlags []templates.CommandFlag

	pathParts := g.getParentPathParts()
	if len(pathParts) > 0 {
		if m, err := g.loadManifest(); err == nil {
			ancestorFlags := g.collectAncestoralPersistentFlags(m.Commands, pathParts)
			ancestralPersistentFlags = g.convertManifestFlagsToTemplate(ancestorFlags)
		}
	}

	return ancestralPersistentFlags
}

func (g *Generator) collectAncestoralPersistentFlags(commands []ManifestCommand, pathParts []string) []ManifestFlag {
	var flags []ManifestFlag

	seen := make(map[string]bool)
	current := commands

	for _, part := range pathParts {
		found := false

		for i := range current {
			if current[i].Name == part {
				for _, f := range current[i].Flags {
					if f.Persistent && !seen[f.Name] {
						flags = append(flags, f)
						seen[f.Name] = true
					}
				}

				current = current[i].Commands
				found = true

				break
			}
		}

		if !found {
			break
		}
	}

	return flags
}

func (g *Generator) convertManifestFlagsToTemplate(mFlags []ManifestFlag) []templates.CommandFlag {
	tFlags := make([]templates.CommandFlag, 0, len(mFlags))
	for _, f := range mFlags {
		tFlags = append(tFlags, templates.CommandFlag{
			Name:        f.Name,
			Type:        f.Type,
			Description: string(f.Description),
			Persistent:  f.Persistent,
			Shorthand:   f.Shorthand,
			Default:     f.Default,
			Required:    f.Required,
			Hidden:      f.Hidden,
		})
	}

	return tFlags
}

func (g *Generator) performGeneration(ctx context.Context, cmdDir string, data *templates.CommandData) error {
	if err := g.props.FS.MkdirAll(cmdDir, os.ModePerm); err != nil {
		return errors.Newf("failed to create directory %s: %w", cmdDir, err)
	}

	data.OmitRun = g.shouldOmitRun(*data, cmdDir)

	g.props.Logger.Info("Generating command boilerplate...")

	return g.GenerateCommandFile(ctx, cmdDir, data)
}

func (g *Generator) postGenerate(ctx context.Context, data templates.CommandData, cmdDir string) error {
	if g.config.WithAssets {
		g.props.Logger.Info("Generating asset files...")

		if err := g.generateAssetFiles(cmdDir); err != nil {
			return err
		}
	}

	g.props.Logger.Infof("Registering subcommand %q...", data.Name)

	if err := g.registerSubcommand(); err != nil {
		g.props.Logger.Warnf("Failed to register subcommand %q: %v", data.Name, err)
	}

	g.props.Logger.Info("Updating manifest.yaml...")

	allFlags := append([]templates.CommandFlag{}, data.Flags...)
	allFlags = append(allFlags, data.PersistentFlags...)

	g.props.Logger.Info("DEBUG: Before updateManifest")

	if err := g.updateManifest(allFlags, data.Hashes); err != nil {
		g.props.Logger.Warnf("Failed to update manifest: %v", err)
	}

	g.props.Logger.Info("DEBUG: Before handleDocumentationGeneration")
	g.props.Logger.Info("Generating documentation...")

	return g.handleDocumentationGeneration(ctx, data, cmdDir)
}

func (g *Generator) handleDocumentationGeneration(ctx context.Context, data templates.CommandData, cmdDir string) error {
	// Check if documentation already exists
	_, docPath := g.prepareDocsContext(data.Name, "", false)
	exists, _ := afero.Exists(g.props.FS, docPath)

	switch {
	case g.config.UpdateDocs:
		g.props.Logger.Infof("AI documentation update/generation requested for %q...", data.Name)

		if err := g.GenerateDocs(ctx, cmdDir, false); err != nil {
			g.props.Logger.Warnf("Failed to generate documentation for %q with AI: %v", data.Name, err)
		}
	case !exists:
		g.props.Logger.Infof("No documentation found for %q, attempting AI generation...", data.Name)

		if err := g.GenerateDocs(ctx, cmdDir, false); err != nil {
			g.props.Logger.Warnf("Failed to generate documentation for %q with AI, falling back to boilerplate: %v", data.Name, err)

			if err := g.generateDocs(); err != nil {
				g.props.Logger.Warnf("Failed to generate documentation for %q: %v", data.Name, err)
			}
		}
	default:
		g.props.Logger.Infof("Documentation for %q already exists, skipping boilerplate generation. Use --update-docs to update with AI.", data.Name)
	}

	return nil
}

func (g *Generator) parseFlags() []CommandFlag {
	parsedFlags := make([]CommandFlag, 0, len(g.config.Flags))

	const maxFlagParts = 8

	for _, flag := range g.config.Flags {
		parts := strings.SplitN(flag, ":", maxFlagParts)
		f := CommandFlag{
			Name:       parts[0],
			Type:       "string", // Default
			Persistent: false,
		}

		if len(parts) > 1 {
			f.Type = parts[1]
		}

		const (
			shortFormat        = 2
			longFormat         = 3
			shorthandIndex     = 4
			requiredIndex      = 5
			defaultIndex       = 6
			defaultIsCodeIndex = 7
		)

		if len(parts) > shortFormat {
			f.Description = parts[2]
		}

		if len(parts) > longFormat {
			f.Persistent = parts[3] == "true"
		}

		if len(parts) > shorthandIndex {
			f.Shorthand = parts[shorthandIndex]
		}

		if len(parts) > requiredIndex {
			f.Required = parts[requiredIndex] == "true"
		}

		if len(parts) > defaultIndex {
			f.Default = parts[defaultIndex]
		}

		if len(parts) > defaultIsCodeIndex {
			f.DefaultIsCode = parts[defaultIsCodeIndex] == "true"
		}

		parsedFlags = append(parsedFlags, f)
	}

	return parsedFlags
}

func (g *Generator) convertFlagsToTemplate(flags []CommandFlag) []templates.CommandFlag {
	tFlags := make([]templates.CommandFlag, 0, len(flags))

	for _, f := range flags {
		tFlags = append(tFlags, templates.CommandFlag{
			Name:          f.Name,
			Type:          f.Type,
			Description:   f.Description,
			Persistent:    f.Persistent,
			Shorthand:     f.Shorthand,
			Default:       f.Default,
			DefaultIsCode: f.DefaultIsCode,
			Required:      f.Required,
			Hidden:        f.Hidden,
		})
	}

	return tFlags
}

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

	mainFile := filepath.Join(cmdDir, "main.go")

	exists, _ := afero.Exists(g.props.FS, mainFile)
	if !exists || g.config.Force {
		g.props.Logger.Infof("Writing execution file: %s", mainFile)

		if err := g.generateExecutionFile(ctx, cmdDir, *data); err != nil {
			return err
		}
	}

	if data.WithInitializer {
		g.props.Logger.Infof("Writing initializer file: %s", filepath.Join(cmdDir, "init.go"))

		hash, err := g.generateInitializerFile(cmdDir, *data)
		if err != nil {
			return err
		}

		data.Hashes["init.go"] = hash
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
	regFile := templates.CommandRegistration(data)

	var buf bytes.Buffer
	if err := regFile.Render(&buf); err != nil {
		return "", errors.Newf("failed to render registration file: %w", err)
	}

	content := buf.Bytes()
	newHash := calculateHash(content)

	// Check if file exists to perform hash verification
	if exists, _ := afero.Exists(g.props.FS, cmdPath); exists {
		if err := g.verifyHash(cmdPath); err != nil {
			return "", err
		}
	}

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

func (g *Generator) verifyHash(path string) error {
	existingContent, err := afero.ReadFile(g.props.FS, path)
	if err != nil {
		return err
	}

	currentHash := calculateHash(existingContent)

	// Retrieve stored hash from manifest if available
	var storedHash string

	if cmd, err := g.findManifestCommand(); err == nil && cmd != nil {
		filename := filepath.Base(path)

		storedHash = cmd.Hashes[filename]
		if storedHash == "" && filename == "cmd.go" {
			storedHash = cmd.Hash
		}
	}

	// If hashes differ and we are not forcing, prompt the user
	if storedHash != "" && storedHash != currentHash && !g.config.Force {
		g.props.Logger.Warnf("Conflict detected for %s: File has been manually modified.", path)

		confirm := g.promptOverwrite(path)
		if !confirm {
			g.props.Logger.Warnf("Skipping overwrite of %s", path)

			return errors.Newf("overwrite skipped by user")
		}

		g.props.Logger.Warnf("Overwriting modified file %s", path)
	}

	return nil
}

func (g *Generator) promptOverwrite(path string) bool {
	// Skip prompt in non-interactive environments
	if os.Getenv("GTB_NON_INTERACTIVE") == "true" {
		return false
	}

	confirm := false // Default to false for safety

	err := huh.NewConfirm().
		Title("Refusing to overwrite " + path).
		Description("The file has been modified since it was last generated. Do you want to overwrite it?").
		Value(&confirm).
		Run()
	if err != nil {
		g.props.Logger.Warnf("Prompt failed (non-interactive?): %v. Skipping overwrite.", err)

		return false
	}

	return confirm
}

func calculateHash(content []byte) string {
	hash := sha256.Sum256(content)

	return hex.EncodeToString(hash[:])
}

func (g *Generator) generateExecutionFile(ctx context.Context, cmdDir string, data templates.CommandData) error {
	mainPath := filepath.Join(cmdDir, "main.go")
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

	// Run go fmt on main.go if using OS filesystem
	if _, ok := g.props.FS.(*afero.OsFs); ok {
		cmd := exec.CommandContext(ctx, "go", "fmt", mainPath)
		_ = cmd.Run()
	}

	return nil
}

func (g *Generator) generateTestFile(ctx context.Context, cmdDir string, data templates.CommandData) (string, error) {
	if data.TestCode == "" {
		return "", nil
	}

	testPath := filepath.Join(cmdDir, "main_test.go")

	// Check if file exists to perform hash verification
	if exists, _ := afero.Exists(g.props.FS, testPath); exists {
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

	// Run go fmt on main_test.go if using OS filesystem
	if _, ok := g.props.FS.(*afero.OsFs); ok {
		cmd := exec.CommandContext(ctx, "go", "fmt", testPath)
		_ = cmd.Run()
	}

	return calculateHash([]byte(data.TestCode)), nil
}

func (g *Generator) verifyAndFixProject(ctx context.Context, cmdDir string, data *templates.CommandData, aiClient chat.ChatClient) error {
	var v verifier.Verifier
	if g.config.Agentless {
		v = verifier.NewLegacy(g.props, g.config.Path)
	} else {
		v = verifier.NewAgentVerifier(g.props)
	}

	genFunc := func(ctx context.Context, cmdDir string, data *templates.CommandData) error {
		return g.GenerateCommandFile(ctx, cmdDir, data)
	}

	return v.VerifyAndFix(ctx, g.config.Path, cmdDir, data, aiClient, genFunc)
}

func (g *Generator) resolveInput() (string, error) {
	if g.config.ScriptPath != "" {
		script, err := os.ReadFile(g.config.ScriptPath)
		if err != nil {
			return "", errors.Newf("failed to read script: %w", err)
		}

		return string(script), nil
	}

	if g.config.Prompt != "" {
		// Check if it's a file path
		if _, err := os.Stat(g.config.Prompt); err == nil {
			content, err := os.ReadFile(g.config.Prompt)
			if err == nil {
				return string(content), nil
			}
		}

		// Otherwise treat as raw string
		return g.config.Prompt, nil
	}

	return "", nil
}

func (g *Generator) startAIGeneration(ctx context.Context, importPath, packageName, funcName, optionsStructName string, flags []CommandFlag) (chat.ChatClient, verifier.AIResponse, error) {
	input, err := g.resolveInput()
	if err != nil {
		return nil, verifier.AIResponse{}, err
	}

	flagDescriptions := "None"

	if len(flags) > 0 {
		var descs []string
		for _, f := range flags {
			descs = append(descs, fmt.Sprintf("- %s (%s): %s", PascalCase(f.Name), f.Type, f.Description))
		}

		flagDescriptions = strings.Join(descs, "\n")
	}

	provider := g.resolveProvider()
	if g.props.Config.GetBool("ai.claude.local") {
		provider = chat.ProviderClaudeLocal
	}

	token := g.resolveToken(provider)
	model := g.resolveModel(provider)

	systemPrompt := fmt.Sprintf(commandGenerationSystemPrompt, packageName, funcName, optionsStructName, optionsStructName, flagDescriptions, optionsStructName, packageName, importPath, packageName, funcName)

	chatCfg := chat.Config{
		Provider:       provider,
		Model:          model,
		Token:          token,
		SystemPrompt:   systemPrompt,
		ResponseSchema: chat.GenerateSchema[verifier.AIResponse](),
		SchemaName:     "go_conversion",
	}

	client, err := chat.New(ctx, g.props, chatCfg)
	if err != nil {
		return nil, verifier.AIResponse{}, err
	}

	var resp verifier.AIResponse
	if err := client.Ask(input, &resp); err != nil {
		return client, verifier.AIResponse{}, err
	}

	g.props.Logger.Info("AI Response received", "go_code_len", len(resp.GoCode), "test_code_len", len(resp.TestCode))

	if len(resp.GoCode) == 0 {
		g.props.Logger.Warn("AI returned empty GoCode!")
	}

	return client, resp, nil
}

func (g *Generator) handleAIGeneration(ctx context.Context, data *templates.CommandData, flags []CommandFlag) (chat.ChatClient, error) {
	importPath, err := g.getImportPath()
	if err != nil {
		return nil, errors.Newf("failed to get import path: %w", err)
	}

	aiClient, resp, err := g.startAIGeneration(ctx, importPath, data.Package, "Run"+data.PascalName, data.PascalName+"Options", flags)
	if err != nil {
		return aiClient, err
	}

	data.FullFileContent = resp.GoCode
	data.TestCode = resp.TestCode
	data.Recommendations = resp.Recommendations

	if len(resp.Recommendations) > 0 {
		g.props.Logger.Info("AI Recommendations:")

		for _, rec := range resp.Recommendations {
			g.props.Logger.Infof("- %s", rec)
		}
	}

	return aiClient, nil
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
func (g *Generator) Remove(ctx context.Context) error {
	if err := g.verifyProject(); err != nil {
		return err
	}

	cmdDir, err := g.getCommandPath()
	if err != nil {
		return err
	}

	g.props.Logger.Infof("Removing command %s in %s...", g.config.Name, cmdDir)

	if err := g.performRemoval(cmdDir); err != nil {
		return err
	}

	g.cleanupDocumentation()

	// Also regenerate indices
	if err := g.generateCommandsIndex(); err != nil {
		g.props.Logger.Warnf("Failed to regenerate commands index: %v", err)
	}

	if err := g.regenerateMkdocsNav(); err != nil {
		g.props.Logger.Warnf("Failed to regenerate mkdocs navigation: %v", err)
	}

	g.props.Logger.Infof("Successfully removed command %s.", g.config.Name)

	return nil
}

func (g *Generator) performRemoval(cmdDir string) error {
	// 1. Deregister from parent
	if err := g.deregisterSubcommand(); err != nil {
		g.props.Logger.Warnf("Failed to deregister subcommand: %v", err)
	}

	// 2. Remove from manifest
	if err := g.removeFromManifest(); err != nil {
		return err
	}

	// 3. Delete command directory
	if err := g.props.FS.RemoveAll(cmdDir); err != nil {
		return errors.Newf("failed to remove command directory: %w", err)
	}

	return nil
}

func (g *Generator) cleanupDocumentation() {
	// 4. Delete documentation
	promptParentParts, _ := g.FindCommandParentPath(g.config.Name)

	outRelPath := g.config.Name
	if len(promptParentParts) > 0 {
		outRelPath = filepath.Join(filepath.Join(promptParentParts...), g.config.Name)
	}

	docDir := filepath.Join(g.config.Path, "docs", "commands", outRelPath)
	if exists, _ := afero.Exists(g.props.FS, docDir); exists {
		if err := g.props.FS.RemoveAll(docDir); err != nil {
			g.props.Logger.Warnf("Failed to remove documentation directory: %v", err)
		}
	}
}

func (g *Generator) RegenerateProject(ctx context.Context) error {
	if err := g.verifyProject(); err != nil {
		return err
	}

	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")

	data, err := afero.ReadFile(g.props.FS, manifestPath)
	if err != nil {
		return errors.Newf("failed to read manifest: %w", err)
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return errors.Newf("failed to unmarshal manifest: %w", err)
	}

	g.props.Logger.Info("Regenerating project from manifest...")

	if err := g.regenerateRootCommand(m); err != nil {
		return err
	}

	for _, cmd := range m.Commands {
		if err := g.regenerateCommandRecursive(ctx, cmd, []string{}); err != nil {
			return err
		}
	}

	// Run golangci-lint run --fix if using OS filesystem
	if _, ok := g.props.FS.(*afero.OsFs); ok {
		g.props.Logger.Info("Running golangci-lint run --fix...")

		cmd := exec.CommandContext(ctx, "golangci-lint", "run", "--fix")
		cmd.Dir = g.config.Path
		cmd.Stdout = os.Stdout

		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			g.props.Logger.Warn("Failed to run golangci-lint", "error", err)
		}
	}

	g.props.Logger.Info("Project regeneration complete.")

	return nil
}

func (g *Generator) regenerateCommandRecursive(ctx context.Context, cmd ManifestCommand, parentPath []string) error {
	// Create a sub-generator or update config for this specific command
	origConfig := *g.config

	defer func() { *g.config = origConfig }()

	g.setupCommandConfig(cmd, parentPath)

	cmdDir, err := g.getCommandPath()
	if err != nil {
		return err
	}

	g.props.Logger.Infof("Regenerating command %s in %s...", cmd.Name, cmdDir)

	if err := g.props.FS.MkdirAll(cmdDir, DefaultDirMode); err != nil {
		return errors.Newf("failed to create command directory: %w", err)
	}

	data := g.prepareRegenerationData(cmd, cmdDir)

	if err := g.performGeneration(ctx, cmdDir, &data); err != nil {
		return err
	}

	// Update manifest with new hash
	allFlags := append([]templates.CommandFlag{}, data.Flags...)
	allFlags = append(allFlags, data.PersistentFlags...)

	if err := g.updateManifest(allFlags, data.Hashes); err != nil {
		g.props.Logger.Warnf("Failed to update manifest hash for %s: %v", cmd.Name, err)
	}

	// Recurse for subcommands
	newParentPath := make([]string, len(parentPath)+1)
	copy(newParentPath, parentPath)

	newParentPath[len(parentPath)] = cmd.Name
	for _, subCmd := range cmd.Commands {
		if err := g.regenerateCommandRecursive(ctx, subCmd, newParentPath); err != nil {
			return err
		}
	}

	return nil
}

func (g *Generator) setupCommandConfig(cmd ManifestCommand, parentPath []string) {
	g.config.Name = cmd.Name
	g.config.Short = string(cmd.Description)
	g.config.Long = string(cmd.LongDescription)
	g.config.WithAssets = cmd.WithAssets
	g.config.PersistentPreRun = cmd.PersistentPreRun
	g.config.PreRun = cmd.PreRun
	g.config.Aliases = cmd.Aliases
	g.config.Args = cmd.Args
	g.config.Protected = cmd.Protected
	g.config.Hidden = cmd.Hidden

	g.config.Parent = "root"
	if len(parentPath) > 0 {
		g.config.Parent = strings.Join(parentPath, "/")
	}
}

func (g *Generator) prepareRegenerationData(cmd ManifestCommand, cmdDir string) templates.CommandData {
	flags := g.resolveGenerationFlags()
	data := g.prepareGenerationData(flags)
	data.Aliases = g.config.Aliases
	data.Args = g.config.Args
	data.Hidden = g.config.Hidden
	data.MutuallyExclusive = cmd.MutuallyExclusive
	data.RequiredTogether = cmd.RequiredTogether

	data.OmitRun = g.shouldOmitRun(data, cmdDir)

	return data
}

func (g *Generator) generateInitializerFile(cmdDir string, data templates.CommandData) (string, error) {
	cmdPath := filepath.Join(cmdDir, "init.go")
	initFile := templates.CommandInitializer(data)

	var buf bytes.Buffer
	if err := initFile.Render(&buf); err != nil {
		return "", errors.Newf("failed to render initializer file: %w", err)
	}

	content := buf.Bytes()

	// Check if file exists to perform hash verification
	if exists, _ := afero.Exists(g.props.FS, cmdPath); exists {
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

	return calculateHash(content), nil
}

func (g *Generator) regenerateRootCommand(m Manifest) error {
	g.props.Logger.Info("Regenerating root command...")

	disabledFeatures := calculateDisabledFeatures(m.Properties.Features)
	enabledFeatures := calculateEnabledFeatures(m.Properties.Features)

	releaseProvider, org, repoName := m.GetReleaseSource()

	data := templates.SkeletonRootData{
		Name:             m.Properties.Name,
		Description:      string(m.Properties.Description),
		ReleaseProvider:  releaseProvider,
		Host:             m.ReleaseSource.Host,
		Org:              org,
		RepoName:         repoName,
		Private:          m.ReleaseSource.Private,
		DisabledFeatures: disabledFeatures,
		EnabledFeatures:  enabledFeatures,
	}

	f := templates.SkeletonRoot(data)

	rootCmdPath := filepath.Join(g.config.Path, "pkg", "cmd", "root", "cmd.go")
	if err := g.props.FS.MkdirAll(filepath.Dir(rootCmdPath), DefaultDirMode); err != nil {
		return errors.Newf("failed to create root command directory: %w", err)
	}

	out, err := g.props.FS.Create(rootCmdPath)
	if err != nil {
		return errors.Newf("failed to create root command file: %w", err)
	}

	defer func() { _ = out.Close() }()

	if err := f.Render(out); err != nil {
		return errors.Newf("failed to render root command file: %w", err)
	}

	return nil
}
