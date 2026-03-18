package generator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/invopop/jsonschema"
	"github.com/spf13/afero"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"

	"github.com/phpboyscout/gtb/pkg/chat"
)

var ErrInvalidPackageName = errors.Newf("invalid package name")

var packageDocumentationSystemPrompt = `You are an expert technical writer and software engineer.
Your goal is to generate comprehensive, professional Markdown documentation for a Go package (Developer Documentation).
Audience: Software Engineers integrating with or maintaining this package.
Tone: Technical, Precise, and Resourceful.

STYLE GUIDELINES (CRITICAL):
0. **Frontmatter**: You MUST include a YAML frontmatter block.
   - title: The package name (e.g. "%s").
   - description: A concise summary of the package.
   - date: %s
   - tags: [go, package, %s]
   - authors: A list of authors. Append "%s" to existing.

1. **Format**: Use Standard Markdown.

CONTEXT:
Existing Documentation (content below separator):
================================================================================
%s
================================================================================

INSTRUCTIONS:
- Preserve manual edits/tags/authors.
- Merge authors with current AI model.
- Document Exported Types, Interfaces, Functions.
- Provide Usage Examples.

IMPORT MAPPING:
Module: "%s".

Sections:
* "# Package {Name}"
* "## Overview"
* "## Index" (List of exported symbols)
* "## types"
* "## Functions"
* "## Usage"

IMPORTANT: Return ONLY raw Markdown.
`

var commandDocumentationSystemPrompt = `You are an expert technical writer and software engineer.
Your goal is to generate comprehensive, professional Markdown documentation for a Go command line tool.
The audience is typically technical (software engineers), but may also include non-technical users.
Tone: Informative, Friendly, and Precise.

STYLE GUIDELINES (CRITICAL):
0. **Frontmatter**: You MUST include a YAML frontmatter block at the very top of the file, starting with three dashes ---.
   It must contain the following fields:
   - title: The name of the command (e.g. "%s").
   - description: A concise, one-sentence summary of the command.
   - date: The current date (%s).
   - tags: A list of relevant tags (e.g. [cli, command, %s]).
   - authors: A list of authors. You MUST append the current AI model ("%s") to any existing authors.

   Do NOT wrap this frontmatter in a code block. Return it as raw text.
   Example:
   ---
   title: az login
   description: Authenticates the user with the system.
   date: 2023-10-27
   tags: [cli, azure, auth]
   authors: [human-maintainer, gemini-2.0-flash-exp]
   ---

1. **MkDocs Syntax**: Use MkDocs Admonitions for callouts, warnings, or tips.
   Example:
   !!! note "Note Title"
       Content of the note.

   !!! tip
       Helpful tip.

2. **Spacing & Formatting**:
   - You MUST leave a blank line BEFORE and AFTER every headers, paragraph, list, code block, or admonition.
   - This is required for correct rendering.
   - Do NOT squash elements together.

CONTEXT:
Current Date: %s
Existing Documentation (content below separator):
================================================================================
%s
================================================================================

INSTRUCTIONS:
- The content above between separator lines is the EXISTING DOCUMENTATION.
- If it is not empty, you MUST preserve any manual edits, extra sections, or custom frontmatter fields (like tags or authors).
- You MUST merge existing authors with the current AI model.
- You MUST merge existing tags with any new relevant tags.
- Update the 'date' field to the current date.

IMPORT MAPPING:
The module name is "%s". Imports starting with "%s/" correspond to local directories.
Example: "%s/pkg/foo" -> "pkg/foo".
If you see such imports, delegates logic to another package in the project, you MUST use 'read_file' or 'list_dir' to inspect those files/directories to ensure the documentation accurately reflects the underlying behavior. Do not guess.

TOOLS:
- read_file: Read file content.
- list_dir: List directory content.
- go_doc: Get Go documentation for a package (useful for standard lib or external dependencies).

You must provide content for the following sections as a minimum:

* "# {Command Name}" - A heading with the name of the command
* "## Usage" - The usage of the command. The command name is "%s".
* "## Description" - A description of what the command does.
* "## Flags" - A Markdown table of flags. Columns: Name, Description, Default, Required. Do not include a raw help dump.
* "## Examples" - Examples of how to use the command. Start commands with "$ %s".

IMPORTANT: Return ONLY the raw Markdown content. Do not wrap the output in ` + "`" + `markdown` + "`" + ` code blocks.
`

// GenerateDocs generates documentation for the command or package.
func (g *Generator) GenerateDocs(ctx context.Context, target string, isPackage bool) error {
	// 1. Resolve Target
	name, relPath, absPath, err := g.resolveDocsTarget(target, isPackage)
	if err != nil {
		return err
	}

	moduleName := g.getModuleNameSafe()

	// 2. Read Source
	content, err := g.readSource(absPath, isPackage)
	if err != nil {
		return err
	}

	sysPrompt, outputPath := g.getPromptAndOutput(name, relPath, moduleName, isPackage)
	userPrompt := fmt.Sprintf("Generate documentation for the following Go command code:\n\n%s", content)

	// 4. Call AI
	provider, model := g.resolveAIConfig()

	client, err := g.createAIDocsClient(ctx, provider, model, sysPrompt)
	if err != nil {
		g.props.Logger.Warnf("AI client unavailable: %v. Skipping AI documentation generation.", err)

		return nil
	}

	g.props.Logger.Info("Requesting documentation from AI...")

	docsContent, err := client.Chat(ctx, userPrompt)
	if err != nil {
		return errors.Newf("AI request failed: %w", err)
	}

	// 5. Sanitize & Write
	docsContent = g.sanitizeAIOutput(docsContent)

	docsDir := filepath.Dir(outputPath)
	if err := g.props.FS.MkdirAll(docsDir, os.ModePerm); err != nil {
		return err
	}

	g.props.Logger.Infof("Writing documentation to %s", outputPath)

	if err := afero.WriteFile(g.props.FS, outputPath, []byte(docsContent), DefaultFileMode); err != nil {
		return err
	}

	if isPackage {
		return g.generatePackagesIndex()
	}

	return g.generateCommandsIndex()
}

func (g *Generator) getModuleNameSafe() string {
	moduleName, err := g.getModuleName()
	if err != nil {
		g.props.Logger.Warn("Could not determine module name from go.mod", "error", err)

		return "project"
	}

	return moduleName
}

func (g *Generator) readSource(absPath string, isPackage bool) (string, error) {
	if isPackage {
		return g.readPackageSource(absPath)
	}

	return g.readCommandSource(absPath)
}

func (g *Generator) getPromptAndOutput(name, relPath, moduleName string, isPackage bool) (sysPrompt, outputPath string) {
	fullCmdName, outputPath := g.prepareDocsContext(name, relPath, isPackage)
	existingDocsContent := g.readExistingDocs(outputPath)

	currentDate := time.Now().Format("2006-01-02")
	provider, model := g.resolveAIConfig()
	aiAuthor := fmt.Sprintf("%s (%s)", g.capitalize(provider), model)

	if isPackage {
		sysPrompt = fmt.Sprintf(packageDocumentationSystemPrompt, name, currentDate, name, aiAuthor, existingDocsContent, moduleName)
	} else {
		sysPrompt = fmt.Sprintf(commandDocumentationSystemPrompt, fullCmdName, currentDate, name, aiAuthor, currentDate, existingDocsContent, moduleName, moduleName, moduleName, fullCmdName, fullCmdName)
	}

	return sysPrompt, outputPath
}

func (g *Generator) readExistingDocs(path string) string {
	if exists, _ := afero.Exists(g.props.FS, path); exists {
		if data, err := afero.ReadFile(g.props.FS, path); err == nil {
			return string(data)
		}
	}

	return ""
}

func (g *Generator) capitalize(s string) string {
	if len(s) > 0 {
		return strings.ToUpper(s[:1]) + s[1:]
	}

	return s
}

func (g *Generator) resolvePathFromProjectRoot(configPath, target string) string {
	projectCmdPath := filepath.Join(configPath, "pkg/cmd", target)
	if absProjectCmdPath, err := filepath.Abs(projectCmdPath); err == nil {
		if exists, _ := afero.Exists(g.props.FS, absProjectCmdPath); exists {
			return absProjectCmdPath
		}
	}

	return target // fallback
}

func (g *Generator) resolveDocsTarget(target string, isPackage bool) (name, relPath, absPath string, err error) {
	configPath, err := filepath.Abs(g.config.Path)
	if err != nil {
		return "", "", "", errors.Newf("failed to resolve absolute config path: %w", err)
	}

	if isPackage {
		relPath = target
		name = filepath.Base(target)
		absPath = filepath.Join(configPath, relPath)

		return name, relPath, absPath, nil
	}

	absPath, err = filepath.Abs(target)
	if err != nil {
		return "", "", "", errors.Newf("failed to get absolute path: %w", err)
	}

	if exists, _ := afero.Exists(g.props.FS, absPath); !exists {
		absPath = g.resolvePathFromProjectRoot(configPath, target)
	}

	relPath, err = filepath.Rel(configPath, absPath)
	if err != nil {
		return "", "", "", errors.Newf("failed to get relative path for command: %w", err)
	}

	name = filepath.Base(absPath)
	if name == "." || name == "main.go" {
		name = filepath.Base(filepath.Dir(absPath))
	}

	if g.config.Name != "" {
		name = g.config.Name
	}

	return name, relPath, absPath, nil
}

func (g *Generator) prepareDocsContext(name, relPath string, isPackage bool) (fullCmdName, outputPath string) {
	if isPackage {
		outputPath = filepath.Join(g.config.Path, "docs", "packages", relPath, "index.md")

		return name, outputPath
	}

	toolName := g.props.Tool.Name

	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")
	if manifestData, err := afero.ReadFile(g.props.FS, manifestPath); err == nil {
		var m Manifest
		if err := yaml.Unmarshal(manifestData, &m); err == nil && m.Properties.Name != "" {
			toolName = m.Properties.Name
		}
	}

	promptParentParts, _ := g.FindCommandParentPath(name)

	fullCmdName = toolName
	if len(promptParentParts) > 0 {
		fullCmdName += " " + strings.Join(promptParentParts, " ")
	}

	fullCmdName += " " + name

	outRelPath := name
	if len(promptParentParts) > 0 {
		outRelPath = filepath.Join(filepath.Join(promptParentParts...), name)
	}

	outputPath = filepath.Join(g.config.Path, "docs", "commands", outRelPath, "index.md")

	return fullCmdName, outputPath
}

func (g *Generator) resolveAIConfig() (provider, model string) {
	provider = g.config.AIProvider
	if provider == "" {
		provider = g.props.Config.GetString("ai.provider")
	}

	if provider == "" {
		provider = string(chat.ProviderClaude)
	}

	model = g.config.AIModel
	if model == "" {
		model = g.props.Config.GetString("ai.model")
	}

	if model == "" {
		model = g.resolveModel(chat.Provider(provider))
	}

	return provider, model
}

func (g *Generator) sanitizeAIOutput(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		if idx := strings.Index(content, "\n"); idx != -1 {
			content = content[idx+1:]
		}
	}

	return strings.TrimSpace(content)
}

func (g *Generator) createAIDocsClient(ctx context.Context, provider, model, sysPrompt string) (chat.ChatClient, error) {
	if g.chatClient != nil {
		return g.chatClient, nil
	}

	chatCfg := chat.Config{
		Provider:     chat.Provider(provider),
		Model:        model,
		SystemPrompt: sysPrompt,
	}

	client, err := chat.New(ctx, g.props, chatCfg)
	if err != nil {
		return nil, errors.Newf("failed to create AI client: %w", err)
	}

	pathSchema := chat.GenerateSchema[struct {
		Path string `json:"path" jsonschema:"description=Relative path to the file or directory"`
	}]()
	pkgSchema := chat.GenerateSchema[struct {
		Package string `json:"package" jsonschema:"description=Go package path (e.g. fmt, github.com/foo/bar)"`
	}]()

	jsonSchema, ok := pathSchema.(*jsonschema.Schema)
	if !ok {
		return nil, errors.New("failed to generate tool schema")
	}

	pkgJsonSchema, ok := pkgSchema.(*jsonschema.Schema)
	if !ok {
		return nil, errors.New("failed to generate pkg tool schema")
	}

	ReadFileTool := chat.Tool{
		Name:        "read_file",
		Description: "Read the contents of a file from the project. Use this to inspect referenced types or subcommands.",
		Parameters:  jsonSchema,
		Handler:     g.handleReadFileTool,
	}

	ListDirTool := chat.Tool{
		Name:        "list_dir",
		Description: "List files and directories in a given path.",
		Parameters:  jsonSchema,
		Handler:     g.handleListDirTool,
	}

	GoDocTool := chat.Tool{
		Name:        "go_doc",
		Description: "Get documentation for a Go package.",
		Parameters:  pkgJsonSchema,
		Handler:     g.handleGoDocTool,
	}

	if err := client.SetTools([]chat.Tool{ReadFileTool, ListDirTool, GoDocTool}); err != nil {
		return nil, errors.Newf("failed to set tools: %w", err)
	}

	return client, nil
}

func (g *Generator) handleReadFileTool(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	targetPath := filepath.Join(g.config.Path, params.Path)

	data, err := afero.ReadFile(g.props.FS, targetPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", params.Path, err)
	}

	return string(data), nil
}

func (g *Generator) handleListDirTool(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	targetPath := filepath.Join(g.config.Path, params.Path)

	entries, err := afero.ReadDir(g.props.FS, targetPath)
	if err != nil {
		return nil, fmt.Errorf("failed to list dir %s: %w", params.Path, err)
	}

	var names []string

	for _, e := range entries {
		suffix := ""
		if e.IsDir() {
			suffix = "/"
		}

		names = append(names, e.Name()+suffix)
	}

	return strings.Join(names, "\n"), nil
}

func (g *Generator) handleGoDocTool(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Package string `json:"package"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	var (
		output []byte
		err    error
	)

	if g.runCommand != nil {
		output, err = g.runCommand(ctx, g.config.Path, "go", "doc", params.Package)
	} else {
		// Validate params.Package to prevent command injection
		validPackage := regexp.MustCompile(`^[a-zA-Z0-9_\-./]+$`)
		if !validPackage.MatchString(params.Package) {
			return nil, errors.Wrap(ErrInvalidPackageName, params.Package)
		}

		cmd := exec.CommandContext(ctx, "go", "doc", params.Package) //nolint:gosec // validated input
		cmd.Dir = g.config.Path
		output, err = cmd.CombinedOutput()
	}

	if err != nil {
		return nil, fmt.Errorf("go doc failed: %w\nOutput: %s", err, string(output))
	}

	return string(output), nil
}

// readCommandSource reads the content of the main go file in the directory.
func (g *Generator) readCommandSource(path string) (string, error) {
	info, err := g.props.FS.Stat(path)
	if err != nil {
		return "", err
	}

	if info.IsDir() {
		return g.readPackageSource(path)
	}

	data, err := afero.ReadFile(g.props.FS, path)
	if err != nil {
		return "", errors.Newf("failed to read command source: %w", err)
	}

	return string(data), nil
}

// readPackageSource reads all .go files in the package directory.
func (g *Generator) readPackageSource(path string) (string, error) {
	var contentBuilder strings.Builder

	files, err := afero.ReadDir(g.props.FS, path)
	if err != nil {
		return "", errors.Newf("failed to read package directory: %w", err)
	}

	foundGoFiles := false

	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".go") && !strings.HasSuffix(file.Name(), "_test.go") {
			foundGoFiles = true
			filePath := filepath.Join(path, file.Name())

			data, err := afero.ReadFile(g.props.FS, filePath)
			if err != nil {
				g.props.Logger.Warn("Failed to read file", "file", filePath, "error", err)

				continue
			}

			fmt.Fprintf(&contentBuilder, "// File: %s\n", file.Name())
			contentBuilder.Write(data)
			contentBuilder.WriteString("\n\n")
		}
	}

	if !foundGoFiles {
		return "", errors.New("no .go files found in package directory")
	}

	return contentBuilder.String(), nil
}

func (g *Generator) generatePackagesIndex() error {
	p := g.props
	p.Logger.Info("Updating packages index...")

	packagesDir := filepath.Join(g.config.Path, "docs", "packages")
	indexFile := filepath.Join(packagesDir, "index.md")

	packageRows := make([]string, 0)

	err := afero.Walk(g.props.FS, packagesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			return nil
		}

		packageIndexFile := filepath.Join(path, "index.md")
		if path == packagesDir || packageIndexFile == indexFile {
			return nil
		}

		if exists, _ := afero.Exists(g.props.FS, packageIndexFile); !exists {
			return nil
		}

		relPath, _ := filepath.Rel(packagesDir, path)
		frontmatter := getFrontmatter(g.props.FS, packageIndexFile)

		desc := "No description"
		if d, ok := frontmatter["description"].(string); ok {
			desc = d
		}

		row := fmt.Sprintf("| [%s](%s/) | %s |", relPath, relPath, desc)
		packageRows = append(packageRows, row)

		return nil
	})
	if err != nil {
		p.Logger.Warn("Error walking packages dir", "error", err)
	}

	content := fmt.Sprintf(`---
title: Package Reference
description: Index of project packages.
---

# Package Reference

| Package | Description |
| :--- | :--- |
%s
`, strings.Join(packageRows, "\n"))

	return afero.WriteFile(g.props.FS, indexFile, []byte(content), DefaultFileMode)
}

func getFrontmatter(fs afero.Fs, docPath string) map[string]any {
	data, err := afero.ReadFile(fs, docPath)
	if err != nil {
		return nil
	}

	contentStr := string(data)
	if strings.HasPrefix(contentStr, "---") {
		end := strings.Index(contentStr[3:], "---")
		if end != -1 {
			yamlBlock := contentStr[3 : end+3]

			var meta map[string]any
			if yaml.Unmarshal([]byte(yamlBlock), &meta) == nil {
				return meta
			}
		}
	}

	return nil
}

func (g *Generator) generateCommandsIndex() error {
	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")

	data, err := afero.ReadFile(g.props.FS, manifestPath)
	if err != nil {
		return errors.Newf("failed to read manifest: %w", err)
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return errors.Newf("failed to unmarshal manifest: %w", err)
	}

	var content strings.Builder
	content.WriteString("# Commands\n\n")
	content.WriteString("| Command | Description |\n")
	content.WriteString("| :--- | :--- |\n")

	var walk func(cmds []ManifestCommand, parentPath string)

	walk = func(cmds []ManifestCommand, parentPath string) {
		for _, cmd := range cmds {
			fullPath := cmd.Name
			if parentPath != "" {
				fullPath = parentPath + " " + cmd.Name
			}

			// Construct relative link path (e.g. "az/index.md", "az/login/index.md")
			relPath := strings.ReplaceAll(fullPath, " ", "/") + "/index.md"

			// Try to read description from generated doc frontmatter
			docPath := filepath.Join(g.config.Path, "docs", "commands", strings.ReplaceAll(fullPath, " ", string(filepath.Separator)), "index.md")
			fileDesc := ""

			if frontmatter := getFrontmatter(g.props.FS, docPath); frontmatter != nil {
				if d, ok := frontmatter["description"].(string); ok {
					fileDesc = d
				}
			}

			desc := string(cmd.Description)
			if fileDesc != "" {
				desc = fileDesc
			} else if desc == "" {
				desc = string(cmd.LongDescription)
			}

			fmt.Fprintf(&content, "| [%s](%s) | %s |\n", fullPath, relPath, desc)

			walk(cmd.Commands, fullPath)
		}
	}

	if len(m.Commands) > 0 {
		walk(m.Commands, "")
	}

	indexPath := filepath.Join(g.config.Path, "docs", "commands", "index.md")
	g.props.Logger.Infof("Updating commands index: %s", indexPath)

	return afero.WriteFile(g.props.FS, indexPath, []byte(content.String()), DefaultFileMode)
}

// Legacy doc functions.
func (g *Generator) generateDocs() error {
	parentParts := g.getParentPathParts()
	docsDir := filepath.Join(g.config.Path, "docs", "commands")

	for _, part := range parentParts {
		docsDir = filepath.Join(docsDir, part)
	}

	// Create directory for the command
	docsDir = filepath.Join(docsDir, g.config.Name)

	if err := g.props.FS.MkdirAll(docsDir, os.ModePerm); err != nil {
		return err
	}

	docPath := filepath.Join(docsDir, "index.md")

	f, err := g.props.FS.Create(docPath)
	if err != nil {
		return err
	}

	defer func() {
		_ = f.Close()
	}()

	if _, err = fmt.Fprintf(f, "# %s\n\n%s\n\n%s\n", g.config.Name, g.config.Short, g.config.Long); err != nil {
		return err
	}

	return g.regenerateMkdocsNav()
}

func (g *Generator) regenerateMkdocsNav() error {
	mkdocsPath := filepath.Join(g.config.Path, "mkdocs.yml")

	if exists, _ := afero.Exists(g.props.FS, mkdocsPath); !exists {
		g.props.Logger.Warn("mkdocs.yml not found, skipping navigation update")

		return nil
	}

	m, err := g.loadManifest()
	if err != nil {
		return err
	}

	rootNode, err := g.loadMkdocsNode(mkdocsPath)
	if err != nil {
		return err
	}

	if err := g.updateMkdocsNavNode(rootNode, m); err != nil {
		return err
	}

	return g.saveMkdocsNode(mkdocsPath, rootNode)
}

func (g *Generator) loadMkdocsNode(path string) (*yaml.Node, error) {
	data, err := afero.ReadFile(g.props.FS, path)
	if err != nil {
		return nil, err
	}

	var rootNode yaml.Node
	if err := yaml.Unmarshal(data, &rootNode); err != nil {
		return nil, errors.Newf("failed to unmarshal mkdocs.yml: %w", err)
	}

	return &rootNode, nil
}

func (g *Generator) saveMkdocsNode(path string, node *yaml.Node) error {
	updated, err := yaml.Marshal(node)
	if err != nil {
		return errors.Newf("failed to marshal mkdocs.yml: %w", err)
	}

	return afero.WriteFile(g.props.FS, path, updated, DefaultFileMode)
}

func (g *Generator) updateMkdocsNavNode(rootNode *yaml.Node, m *Manifest) error {
	if len(rootNode.Content) == 0 || rootNode.Content[0].Kind != yaml.MappingNode {
		return errors.New("mkdocs.yml is not a valid map")
	}

	navNode, navValueNode := g.findNavNode(rootNode)

	var nav []any
	if navValueNode != nil {
		if err := navValueNode.Decode(&nav); err != nil {
			return errors.Newf("failed to decode nav: %w", err)
		}
	} else {
		nav = []any{}
	}

	cliNav := buildNavFromCommands(m.Commands, []string{})
	updatedNav := updateNavSection(nav, "CLI", cliNav)

	newNavNode, err := g.marshalNavToNode(updatedNav)
	if err != nil {
		return err
	}

	if navNode != nil {
		*navValueNode = newNavNode
	} else {
		rootNode.Content[0].Content = append(rootNode.Content[0].Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "nav"},
			&newNavNode,
		)
	}

	return nil
}

func (g *Generator) findNavNode(rootNode *yaml.Node) (navNode, navValueNode *yaml.Node) {
	for i := 0; i < len(rootNode.Content[0].Content); i += 2 {
		keyNode := rootNode.Content[0].Content[i]
		if keyNode.Value == "nav" {
			return keyNode, rootNode.Content[0].Content[i+1]
		}
	}

	return nil, nil
}

func (g *Generator) marshalNavToNode(nav []any) (yaml.Node, error) {
	navBytes, err := yaml.Marshal(nav)
	if err != nil {
		return yaml.Node{}, errors.Newf("failed to marshal updated nav: %w", err)
	}

	var newNavNode yaml.Node
	if err := yaml.Unmarshal(navBytes, &newNavNode); err != nil {
		return yaml.Node{}, errors.Newf("failed to unmarshal updated nav node: %w", err)
	}

	if len(newNavNode.Content) > 0 {
		return *newNavNode.Content[0], nil
	}

	return newNavNode, nil
}

func buildNavFromCommands(commands []ManifestCommand, parentPath []string) []any {
	nav := make([]any, 0, len(commands))

	for _, cmd := range commands {
		currentPath := make([]string, len(parentPath)+1)
		copy(currentPath, parentPath)
		currentPath[len(parentPath)] = cmd.Name

		relPath := filepath.Join("commands", filepath.Join(currentPath...), "index.md")

		item := map[string]any{}
		displayName := toTitle(cmd.Name) // Simple title case or PascalCase if available

		if len(cmd.Commands) > 0 {
			childrenNav := buildNavFromCommands(cmd.Commands, currentPath)
			sectionItems := make([]any, 0, 1+len(childrenNav))

			sectionItems = append(sectionItems, relPath)
			sectionItems = append(sectionItems, childrenNav...)

			item[displayName] = sectionItems // Section Index
		} else {
			item[displayName] = relPath
		}

		nav = append(nav, item)
	}

	return nav
}

func toTitle(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")

	return cases.Title(language.English).String(s)
}

func updateNavSection(nav []any, sectionName string, newContent []any) []any {
	found := false

	for i, item := range nav {
		if m, ok := item.(map[string]any); ok {
			if _, exists := m[sectionName]; exists {
				nav[i] = map[string]any{
					sectionName: newContent,
				}
				found = true

				break
			}
		}
	}

	if !found {
		nav = append(nav, map[string]any{
			sectionName: newContent,
		})
	}

	return nav
}
