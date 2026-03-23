package generator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"

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
  - props.Logger (logger.Logger): Structured logger.
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
- DO NOT include any auto-generated code markers, "// Code generated" comments, or machine-generated file headers. Write only idiomatic, hand-authored Go code.
- Use explicit error handling.
- Use structured logging: props.Logger.Info("msg", "key", "val").
- You must also generate a unit test file for the converted code.
- The test file MUST use the black-box testing pattern (package name ends in '_test', e.g., '%s_test').
- You MUST import the package under test (import path: '%s').
- You MUST import "github.com/phpboyscout/gtb/pkg/props" if you reference 'props.Props' in the test.
- You MUST reference functions and structs from the package under test (e.g., '%s.%s').
- If mocking is required for props.Config, use the shared mocks in 'github.com/phpboyscout/gtb/mocks/pkg/config'.
- For the Logger ('props.Logger'), DO NOT use a mock. Instead, use an actual implementation:
- Import "github.com/phpboyscout/gtb/pkg/logger".
- Initialize it with: l := logger.NewNoop().
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
	_, err := newCommandPipeline(g, PipelineOptions{}).Run(ctx, data, cmdDir)

	return err
}

func (g *Generator) handleDocumentationGeneration(ctx context.Context, data templates.CommandData, cmdDir string) {
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
	if err := client.Ask(ctx, input, &resp); err != nil {
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
