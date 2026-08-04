package generator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/afero"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"

	gochat "gitlab.com/phpboyscout/go/chat"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
)

var ErrInvalidPackageName = errors.NewSentinel("gtb.generator.invalid_package_name", "invalid package name")

// ErrNoFrontmatter signals that an AI documentation response contained no YAML
// frontmatter fence at all, even after stripping any conversational preamble.
// The generator treats this as a generation failure and falls back to
// deterministic boilerplate rather than committing a frontmatter-less page.
var ErrNoFrontmatter = errors.NewSentinel("gtb.generator.no_frontmatter", "AI documentation response contained no frontmatter")

var packageDocumentationSystemPrompt = `You are an expert technical writer and software engineer.
Your goal is to generate understanding-oriented Markdown documentation for a Go package — explanation, NOT an auto-generated API dump.
Audience: Software Engineers integrating with or maintaining this package.
Tone: Technical, Precise, and Resourceful.

STYLE GUIDELINES (CRITICAL):
0. **Frontmatter**: You MUST include a YAML frontmatter block.
   - title: The package name (e.g. "%s").
   - description: A concise summary of the package.
   - date: %s
   - tags: [go, package, %s]
   - %s

1. **Format**: Use Standard Markdown. Leave a blank line before and after every heading, paragraph, list, and code block.

2. **Diátaxis — explanation quadrant**: Explain WHAT the package is for and HOW it fits together: its purpose, the main exported types/interfaces and their roles, and a short, realistic usage sketch. Do NOT paste large interface/struct definitions or an exhaustive symbol list — that is reference material and does not belong here.

CONTEXT:
Existing Documentation (content below separator):
================================================================================
%s
================================================================================

INSTRUCTIONS:
- Preserve manual edits and tags. %s
- For the full, exhaustive API reference: %s

IMPORT MAPPING:
Module: "%s".

Provide these explanation-oriented sections:
* "# Package {Name}"
* "## Overview" — what it is and the problem it solves.
* "## Key Types" — the main exported types/interfaces and their roles, described narratively (no full definitions).
* "## Usage" — a short, realistic usage sketch.
* "## API Reference" — follow the API-reference instruction above.

IMPORTANT: Return ONLY raw Markdown. Do NOT include any "auto-generated" notices, machine-generated disclaimers, or generation timestamps in the output.
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
   - %s

   Do NOT wrap this frontmatter in a code block. Return it as raw text.
   Example:
   ---
   title: az login
   description: Authenticates the user with the system.
   date: 2023-10-27
   tags: [cli, azure, auth]
   %s
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
- %s
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

IMPORTANT: Return ONLY the raw Markdown content. Do not wrap the output in ` + "`" + `markdown` + "`" + ` code blocks. Do NOT include any "auto-generated" notices, machine-generated disclaimers, or generation timestamps in the output.
`

// GenerateDocs generates documentation for the command or package.
func (g *Generator) GenerateDocs(ctx context.Context, target string, isPackage bool) error {
	if g.config.DryRun {
		g.props.Logger.Info("Dry run: skipping docs generation (requires AI)")

		return nil
	}

	// 1. Resolve Target
	name, relPath, absPath, err := g.resolveDocsTarget(target, isPackage)
	if err != nil {
		return err
	}

	moduleName := g.getModuleNameSafe()

	// --public-api is a durable choice: persist it so later regenerations keep
	// linking pkg.go.dev instead of reverting to the local go doc hint.
	g.persistModulePublished()

	sysPrompt, fullCmdName, outputPath := g.getPromptAndOutput(name, relPath, moduleName, isPackage)

	// AI doc-gen is opt-in: only call a provider when one is explicitly
	// configured and --agentless is not set. Otherwise write deterministic
	// boilerplate with no API call (quietly), so `generate command` never
	// reaches out to Anthropic by default.
	if !g.aiDocsEnabled() {
		g.props.Logger.Debug("AI docs not enabled (no provider configured or --agentless); writing boilerplate", "name", name)

		return g.handleNoAIDocs(name, fullCmdName, relPath, moduleName, outputPath, isPackage)
	}

	// 2. Read Source
	content, err := g.readSource(absPath, isPackage)
	if err != nil {
		return err
	}

	provider, model := g.resolveAIConfig()

	client, err := g.createAIDocsClient(ctx, provider, model, sysPrompt)
	if err != nil {
		g.props.Logger.Info("AI docs unavailable, writing boilerplate", "name", name, "error", err)

		return g.handleNoAIDocs(name, fullCmdName, relPath, moduleName, outputPath, isPackage)
	}

	err = g.writeAIDocs(ctx, client, content, outputPath, isPackage)
	if errors.Is(err, ErrNoFrontmatter) {
		// The model returned no frontmatter at all: rather than commit a broken,
		// frontmatter-less page, fall back to the deterministic boilerplate that
		// GenerateDocs would have written without an AI provider.
		g.props.Logger.Warn("AI response had no frontmatter; writing deterministic boilerplate instead", "name", name)

		return g.handleNoAIDocs(name, fullCmdName, relPath, moduleName, outputPath, isPackage)
	}

	return err
}

// aiDocsEnabled reports whether AI-assisted documentation generation should
// run. It is opt-in: a provider must be explicitly configured — via the
// `--provider` flag (g.config.AIProvider), the `ai.provider` config key, or an
// injected chat client — and `--agentless` must not be set. When false the
// generator writes deterministic boilerplate with no network call, so the
// generator never reaches out to a paid AI API by default.
func (g *Generator) aiDocsEnabled() bool {
	if g.config.Agentless {
		return false
	}

	if g.chatClient != nil {
		return true
	}

	if g.config.AIProvider != "" {
		return true
	}

	return g.props.Config != nil && g.props.Config.View().GetString("ai.provider") != ""
}

// handleNoAIDocs writes documentation without AI assistance.
// For commands it generates a basic template from manifest data.
// For packages it writes a stub file with a notice that AI is required.
func (g *Generator) handleNoAIDocs(name, fullCmdName, relPath, moduleName, outputPath string, isPackage bool) error {
	if isPackage {
		if err := g.writePackageDocStub(name, relPath, moduleName, outputPath); err != nil {
			return err
		}

		return g.generatePackagesIndex()
	}

	if err := g.writeBasicCommandDocs(name, fullCmdName, outputPath); err != nil {
		return err
	}

	return g.generateCommandsIndex()
}

// writeAIDocs sends source content to the AI client and writes the result.
func (g *Generator) writeAIDocs(ctx context.Context, client gochat.ChatClient, content, outputPath string, isPackage bool) error {
	userPrompt := fmt.Sprintf("Generate documentation for the following Go command code:\n\n%s", content)

	g.props.Logger.Info("Requesting documentation from AI...")

	var (
		docsContent string
		err         error
	)

	if streamer, ok := client.(gochat.StreamingChatClient); ok {
		docsContent, err = streamer.StreamChat(ctx, userPrompt, func(e gochat.StreamEvent) error {
			if e.Type == gochat.EventTextDelta {
				g.props.Logger.Debug("AI delta", "len", len(e.Delta))
			}

			return nil
		})
	} else {
		docsContent, err = client.Chat(ctx, userPrompt)
	}

	if err != nil {
		return errors.Newf("AI request failed: %w", err)
	}

	docsContent = g.sanitizeAIOutput(docsContent)

	// Issue #7 defect 1: models emit conversational narration ahead of the YAML
	// frontmatter, which pushes the frontmatter off byte 0 and stops any static
	// site generator from parsing it. Discard everything before the first `---`
	// fence, then assert the frontmatter-first invariant on the bytes we write.
	stripped, ok := stripToFrontmatter(docsContent)
	if !ok {
		return ErrNoFrontmatter
	}

	docsContent = stripped

	if !strings.HasPrefix(docsContent, "---") {
		return errors.Newf("generated documentation is not frontmatter-first")
	}

	docsDir := filepath.Dir(outputPath)
	if err := g.props.FS.MkdirAll(docsDir, os.ModePerm); err != nil {
		return errors.Wrap(err, "failed to create docs directory")
	}

	g.props.Logger.Info("writing documentation", "path", outputPath)

	if err := afero.WriteFile(g.props.FS, outputPath, []byte(docsContent), DefaultFileMode); err != nil {
		return errors.Wrap(err, "failed to write documentation file")
	}

	if isPackage {
		return g.generatePackagesIndex()
	}

	return g.generateCommandsIndex()
}

// writePackageDocStub writes a minimal package doc file with a notice that AI
// is required to generate meaningful content.
func (g *Generator) writePackageDocStub(name, relPath, moduleName, outputPath string) error {
	currentDate := time.Now().Format("2006-01-02")

	content := fmt.Sprintf(`---
title: %s
description: ''
date: %s
tags: [go, package, %s]
---

# %s

## Overview

_TODO: what this package is for and the problem it solves._

## Key Types

_TODO: the main exported types and their roles._

## Usage

_TODO: a short usage sketch._

## API Reference

%s
`, name, currentDate, name, name, g.apiReferenceNote(relPath, moduleName))

	g.props.Logger.Info("writing package documentation stub", "path", outputPath)

	return g.writeDocFile(outputPath, []byte(content))
}

// apiIsPublic reports whether the module's public API should be linked to
// pkg.go.dev — true when --public-api is set (g.config.PublicAPI) or the manifest
// records module_published. Otherwise package docs defer to a local `go doc` hint.
func (g *Generator) apiIsPublic() bool {
	if g.config.PublicAPI {
		return true
	}

	m := g.readManifestQuiet()

	return m != nil && m.Properties.ModulePublished
}

// persistModulePublished stamps module_published: true on the manifest when
// --public-api was passed, so the choice survives future regenerations (package
// API references would otherwise revert to the local go doc hint). Idempotent and
// best-effort: a missing manifest or write failure is non-fatal.
func (g *Generator) persistModulePublished() {
	if !g.config.PublicAPI {
		return
	}

	path := ManifestPathFor(g.config.Path)

	m, err := g.decodeManifestFile(path)
	if err != nil || m.Properties.ModulePublished {
		return
	}

	m.Properties.ModulePublished = true
	if err := g.writeManifestFile(path, *m); err != nil {
		g.props.Logger.Warn("could not persist module_published", "error", err)
	}
}

// apiReferenceNote returns the package doc's "API Reference" body for no-AI
// (boilerplate) generation: a pkg.go.dev link when the module is published
// (--public-api / manifest module_published), else a local `go doc` hint, so a
// private/unpublished module never gets a dead registry link.
func (g *Generator) apiReferenceNote(pkgRel, moduleName string) string {
	if g.apiIsPublic() && moduleName != "" {
		return fmt.Sprintf("See [%s/%s](https://pkg.go.dev/%s/%s) for the full API reference.", moduleName, pkgRel, moduleName, pkgRel)
	}

	return fmt.Sprintf("Run `go doc ./%s` for the full API reference.", pkgRel)
}

// writeBasicCommandDocs generates a markdown template for a command using data
// available without AI: the manifest (description, flags, subcommands) and
// generator config (Short/Long when set from generate command flow).
func (g *Generator) writeBasicCommandDocs(name, fullCmdName, outputPath string) error {
	currentDate := time.Now().Format("2006-01-02")

	var sb strings.Builder

	fmt.Fprintf(&sb, "---\ntitle: %s\ndate: %s\ntags: [cli, command, %s]\n---\n\n", fullCmdName, currentDate, name)
	fmt.Fprintf(&sb, "# %s\n\n", fullCmdName)

	// Best-effort manifest lookup — not fatal if missing.
	var cmd *ManifestCommand

	if m, err := g.loadManifest(); err == nil {
		parentPath, _ := g.FindCommandParentPath(name)
		cmd = findCommandAt(m.Commands, parentPath, name)
	}

	appendCommandDescription(&sb, cmd, g.config.Short, g.config.Long)
	fmt.Fprintf(&sb, "## Usage\n\n```\n%s [flags]\n```\n\n", fullCmdName)
	appendFlagsTable(&sb, cmd)
	appendSubcommandsTable(&sb, fullCmdName, cmd)
	fmt.Fprintf(&sb, "Run `%s --help` for the authoritative, always-current flag set.\n\n", fullCmdName)

	g.props.Logger.Info("writing basic documentation", "path", outputPath)

	return g.writeDocFile(outputPath, []byte(sb.String()))
}

func appendCommandDescription(sb *strings.Builder, cmd *ManifestCommand, short, long string) {
	description := ""
	if cmd != nil && string(cmd.Description) != "" {
		description = string(cmd.Description)
	} else if short != "" {
		description = short
	}

	if description != "" {
		fmt.Fprintf(sb, "## Description\n\n%s\n\n", escapeMarkdown(description))
	}

	longDesc := ""
	if cmd != nil && string(cmd.LongDescription) != "" {
		longDesc = string(cmd.LongDescription)
	} else if long != "" && long != short {
		longDesc = long
	}

	if longDesc != "" {
		sb.WriteString(escapeMarkdown(longDesc) + "\n\n")
	}
}

func appendFlagsTable(sb *strings.Builder, cmd *ManifestCommand) {
	if cmd == nil || len(cmd.Flags) == 0 {
		return
	}

	sb.WriteString("## Flags\n\n")
	sb.WriteString("| Flag | Description | Default | Required |\n")
	sb.WriteString("| :--- | :--- | :--- | :--- |\n")

	for _, f := range cmd.Flags {
		required := ""
		if f.Required {
			required = "Yes"
		}

		fmt.Fprintf(sb, "| `--%s` | %s | %s | %s |\n",
			f.Name,
			escapeMarkdownTableCell(escapeMarkdown(string(f.Description))),
			formatFlagDefaultCell(f.Default),
			required)
	}

	sb.WriteString("\n")
}

// formatFlagDefaultCell renders a flag default as a table-safe code span.
// A default containing a backtick cannot live in a code span (the
// backtick would close it), so it falls back to escaped plain text.
func formatFlagDefaultCell(def string) string {
	if def == "" {
		return ""
	}

	if strings.ContainsRune(def, '`') {
		return escapeMarkdownTableCell(escapeMarkdown(def))
	}

	return "`" + escapeMarkdownTableCell(def) + "`"
}

func appendSubcommandsTable(sb *strings.Builder, fullCmdName string, cmd *ManifestCommand) {
	if cmd == nil || len(cmd.Commands) == 0 {
		return
	}

	sb.WriteString("## Subcommands\n\n")
	sb.WriteString("| Command | Description |\n")
	sb.WriteString("| :--- | :--- |\n")

	for _, sub := range cmd.Commands {
		fmt.Fprintf(sb, "| `%s %s` | %s |\n",
			fullCmdName, sub.Name,
			escapeMarkdownTableCell(escapeMarkdown(string(sub.Description))))
	}

	sb.WriteString("\n")
}

func (g *Generator) writeDocFile(outputPath string, content []byte) error {
	docsDir := filepath.Dir(outputPath)
	if err := g.props.FS.MkdirAll(docsDir, os.ModePerm); err != nil {
		return errors.Wrap(err, "failed to create docs directory")
	}

	if err := afero.WriteFile(g.props.FS, outputPath, content, DefaultFileMode); err != nil {
		return errors.Wrap(err, "failed to write documentation file")
	}

	return nil
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

func (g *Generator) getPromptAndOutput(name, relPath, moduleName string, isPackage bool) (sysPrompt, fullCmdName, outputPath string) {
	fullCmdName, outputPath = g.prepareDocsContext(name, relPath, isPackage)
	existingDocsContent := g.readExistingDocs(outputPath)

	currentDate := time.Now().Format("2006-01-02")
	provider, model := g.resolveAIConfig()
	frontmatterAuthors, mergeAuthors, exampleAuthors := g.authorsDirectives(provider, model)

	if isPackage {
		sysPrompt = fmt.Sprintf(packageDocumentationSystemPrompt, name, currentDate, name, frontmatterAuthors, existingDocsContent, mergeAuthors, g.apiReferencePolicy(relPath, moduleName), moduleName)
	} else {
		sysPrompt = fmt.Sprintf(commandDocumentationSystemPrompt, fullCmdName, currentDate, name, frontmatterAuthors, exampleAuthors, currentDate, existingDocsContent, mergeAuthors, moduleName, moduleName, moduleName, fullCmdName, fullCmdName)
	}

	return sysPrompt, fullCmdName, outputPath
}

// authorsDirectives returns the three authors-related instructions injected into
// the documentation system prompt: the frontmatter `authors:` field description,
// the INSTRUCTIONS-section merge rule, and the worked-example authors line.
//
// By default (issue #7 maintainer decision) AI attribution in generated docs is
// acceptable, but ADDITIVE: the model is told to preserve every existing (human)
// author and merely append the current AI model as an extra co-author, never to
// replace the human. When --no-ai-attribution is set the directives flip: the
// model is told the authors field must carry the project's human author(s) only,
// and to add no AI/model/assistant identity at all.
func (g *Generator) authorsDirectives(provider, model string) (frontmatter, merge, example string) {
	if g.config.NoAIAttribution {
		frontmatter = "authors: A list of the project's human author(s). Preserve any existing authors already present in the documentation. Do NOT add, invent, or append any AI, model, assistant, or tool identity — the authors field must contain the project's human authors only."
		merge = "Preserve the existing human author(s) exactly; do NOT add any AI, model, assistant, or tool identity to the authors field."
		example = "authors: [human-maintainer]"

		return frontmatter, merge, example
	}

	aiAuthor := fmt.Sprintf("%s (%s)", g.capitalize(provider), model)
	frontmatter = fmt.Sprintf(`authors: A list of authors. Preserve every existing author already present in the documentation (never replace or drop the human author(s)), then additionally append the current AI model ("%s") as a co-author if it is not already listed.`, aiAuthor)
	merge = fmt.Sprintf(`Preserve all existing authors — never replace the human author(s) — and additionally append the current AI model ("%s") as a co-author.`, aiAuthor)
	example = "authors: [human-maintainer, gemini-2.0-flash-exp]"

	return frontmatter, merge, example
}

// apiReferencePolicy returns the instruction the package-doc prompt uses for its
// "API Reference" section. Code APIs are deferred to pkg.go.dev only when the
// module is published (the manifest module_published property or the --public-api
// flag); a private/unpublished module has no registry page, so the reference is
// stubbed to a local `go doc` hint instead of a dead link.
func (g *Generator) apiReferencePolicy(pkgRel, moduleName string) string {
	if g.apiIsPublic() {
		return fmt.Sprintf("link to the package's registry page (https://pkg.go.dev/%s/%s) and give a one-line purpose per major symbol, rather than pasting definitions.", moduleName, pkgRel)
	}

	return fmt.Sprintf("do NOT link pkg.go.dev (this module may be private/unpublished); state that the full API is available locally via 'go doc ./%s', and do not paste large type/function definitions or invent a registry URL.", pkgRel)
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
		if err := ValidatePackagePath(target); err != nil {
			return "", "", "", err
		}

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

// parentPartsFromCmdRelPath extracts the parent command path from a command's
// source location relative to the project root — e.g. "pkg/cmd/voice/gen"
// yields ["voice"], "pkg/cmd/foo" yields [] (top-level), and "pkg/cmd/a/b/c"
// yields ["a","b"]. A trailing source file (cmd.go/main.go) is tolerated.
// Returns nil when relPath is empty or not under pkg/cmd, so the caller can fall
// back to a manifest lookup.
func parentPartsFromCmdRelPath(relPath string) []string {
	rel := filepath.ToSlash(strings.TrimSpace(relPath))
	if rel == "" {
		return nil
	}

	if strings.HasSuffix(rel, ".go") {
		rel = pathpkg.Dir(rel)
	}

	const prefix = "pkg/cmd/"
	if !strings.HasPrefix(rel, prefix) {
		return nil
	}

	rel = strings.Trim(strings.TrimPrefix(rel, prefix), "/")
	if rel == "" {
		return nil
	}

	parts := strings.Split(rel, "/")
	// parts is [parent..., leaf]; the parent path is everything but the leaf.
	return parts[:len(parts)-1]
}

func (g *Generator) prepareDocsContext(name, relPath string, isPackage bool) (fullCmdName, outputPath string) {
	m := g.readManifestQuiet()

	layout := DocsLayoutFlat
	if m != nil {
		layout = m.Properties.ResolvedDocsLayout()
	}

	if isPackage {
		if layout == DocsLayoutDiataxis {
			// Component overviews are explanation-quadrant; one file per package.
			outputPath = filepath.Join(g.config.Path, "docs", "explanation", "components", relPath+".md")
		} else {
			outputPath = filepath.Join(g.config.Path, "docs", "packages", relPath, "index.md")
		}

		return name, outputPath
	}

	toolName := g.props.Tool.Name
	if m != nil && m.Properties.Name != "" {
		toolName = m.Properties.Name
	}

	// Prefer the parent path encoded in the command's own source location
	// (pkg/cmd/<parent>/<leaf>), which is unambiguous. A name lookup in the
	// manifest collides when two parents share a leaf name (a/run vs b/run):
	// it returns the first match, so the doc lands under the wrong parent or is
	// skipped as already-existing (keryx v0.19.0 Bug 3).
	promptParentParts := parentPartsFromCmdRelPath(relPath)
	if promptParentParts == nil {
		promptParentParts, _ = g.FindCommandParentPath(name)
	}

	fullCmdName = toolName
	if len(promptParentParts) > 0 {
		fullCmdName += " " + strings.Join(promptParentParts, " ")
	}

	fullCmdName += " " + name

	outRelPath := name
	if len(promptParentParts) > 0 {
		outRelPath = filepath.Join(filepath.Join(promptParentParts...), name)
	}

	if layout == DocsLayoutDiataxis {
		outputPath = g.diataxisCommandDocPath(m, promptParentParts, name, outRelPath)
	} else {
		outputPath = filepath.Join(g.config.Path, "docs", "commands", outRelPath, "index.md")
	}

	return fullCmdName, outputPath
}

// readManifestQuiet loads the manifest without logging or side effects, so it is
// safe for path-resolution code (and minimal test fixtures) that hold a Generator
// with a nil Logger. Returns nil when the manifest is absent or unparseable.
func (g *Generator) readManifestQuiet() *Manifest {
	// Reuse the canonical decoder (DecodeManifestFile is log-free, so this stays
	// nil-Logger safe) and swallow errors to a nil manifest.
	m, err := g.decodeManifestFile(ManifestPathFor(g.config.Path))
	if err != nil {
		return nil
	}

	return m
}

// commandDocRelPath returns a command's doc path relative to its quadrant root,
// applying the single leaf-flat / parent-subsection rule used everywhere a CLI
// doc path is computed (generation, migration, nav, index). In the Diátaxis
// layout a leaf command is <path>.md and a command with subcommands is
// <path>/index.md (so its children sit beside it); the legacy flat layout always
// uses <path>/index.md.
func commandDocRelPath(relPath string, hasChildren, diataxis bool) string {
	if diataxis && !hasChildren {
		return relPath + ".md"
	}

	return filepath.Join(relPath, "index.md")
}

// diataxisCommandDocPath returns the absolute reference/cli output path for a
// command in the Diátaxis layout.
func (g *Generator) diataxisCommandDocPath(m *Manifest, parentParts []string, name, outRelPath string) string {
	cliBase := filepath.Join(g.config.Path, "docs", "reference", "cli")

	hasChildren := false

	if m != nil {
		if cmd := findCommandAt(m.Commands, parentParts, name); cmd != nil && len(cmd.Commands) > 0 {
			hasChildren = true
		}
	}

	return filepath.Join(cliBase, commandDocRelPath(outRelPath, hasChildren, true))
}

func (g *Generator) resolveAIConfig() (provider, model string) {
	view := g.props.Config.View()

	provider = g.config.AIProvider
	if provider == "" {
		provider = view.GetString("ai.provider")
	}

	if provider == "" {
		provider = string(gochat.ProviderClaude)
	}

	model = g.config.AIModel
	if model == "" {
		model = view.GetString("ai.model")
	}

	if model == "" {
		model = g.resolveModel(gochat.Provider(provider))
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

// stripToFrontmatter discards any conversational preamble a model emits ahead of
// the YAML frontmatter, returning the content from the first line that is exactly
// `---` onward and whether such a fence was found. It strips only a leading
// preamble; content within and after the frontmatter is left untouched. When no
// `---` line exists the original content is returned with ok=false so the caller
// can treat it as a generation failure rather than write a frontmatter-less page.
func stripToFrontmatter(content string) (stripped string, ok bool) {
	lines := strings.SplitAfter(content, "\n")
	for i, line := range lines {
		if strings.TrimRight(line, "\r\n") == "---" {
			return strings.Join(lines[i:], ""), true
		}
	}

	return content, false
}

func (g *Generator) createAIDocsClient(ctx context.Context, provider, model, sysPrompt string) (gochat.ChatClient, error) {
	if g.chatClient != nil {
		return g.chatClient, nil
	}

	chatCfg := gochat.Config{
		Provider:       gochat.Provider(provider),
		Model:          model,
		SystemPrompt:   sysPrompt,
		RequestTimeout: g.requestTimeout(),
	}

	client, err := chat.NewWithFallback(ctx, g.props, chatCfg)
	if err != nil {
		return nil, errors.Newf("failed to create AI client: %w", err)
	}

	// go/chat v0.9.0 types GenerateSchema as returning *jsonschema.Schema
	// rather than any, so the assertions these values used to need are gone —
	// and with them the "failed to generate tool schema" branches, which could
	// only ever have fired on a type mismatch the compiler now rules out.
	jsonSchema := gochat.GenerateSchema[struct {
		Path string `json:"path" jsonschema:"description=Relative path to the file or directory"`
	}]()
	pkgJsonSchema := gochat.GenerateSchema[struct {
		Package string `json:"package" jsonschema:"description=Go package path (e.g. fmt, github.com/foo/bar)"`
	}]()

	ReadFileTool := gochat.Tool{
		Name:        "read_file",
		Description: "Read the contents of a file from the project. Use this to inspect referenced types or subcommands.",
		Parameters:  jsonSchema,
		Handler:     g.handleReadFileTool,
	}

	ListDirTool := gochat.Tool{
		Name:        "list_dir",
		Description: "List files and directories in a given path.",
		Parameters:  jsonSchema,
		Handler:     g.handleListDirTool,
	}

	GoDocTool := gochat.Tool{
		Name:        "go_doc",
		Description: "Get documentation for a Go package.",
		Parameters:  pkgJsonSchema,
		Handler:     g.handleGoDocTool,
	}

	if err := client.SetTools([]gochat.Tool{ReadFileTool, ListDirTool, GoDocTool}); err != nil {
		return nil, errors.Newf("failed to set tools: %w", err)
	}

	return client, nil
}

// containedProjectPath joins a model-supplied relative path under the
// project root and rejects any result that escapes it, using the same
// filepath.Abs + filepath.Rel containment the go-doc tool path applies.
// The AI doc tools must never read or list outside the project tree.
func (g *Generator) containedProjectPath(rel string) (string, error) {
	root, err := filepath.Abs(g.config.Path)
	if err != nil {
		return "", errors.Wrap(err, "failed to resolve absolute project root")
	}

	target, _, err := joinContained(root, rel)
	if err != nil {
		return "", errors.Newf("path %q escapes the project root", rel)
	}

	return target, nil
}

func (g *Generator) handleReadFileTool(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, errors.Wrap(err, "failed to parse tool arguments")
	}

	targetPath, err := g.containedProjectPath(params.Path)
	if err != nil {
		return nil, err
	}

	data, err := afero.ReadFile(g.props.FS, targetPath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read file %s", params.Path)
	}

	return string(data), nil
}

func (g *Generator) handleListDirTool(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, errors.Wrap(err, "failed to parse tool arguments")
	}

	targetPath, err := g.containedProjectPath(params.Path)
	if err != nil {
		return nil, err
	}

	entries, err := afero.ReadDir(g.props.FS, targetPath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list dir %s", params.Path)
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
		return nil, errors.Wrap(err, "failed to parse tool arguments")
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
		return nil, errors.Wrapf(err, "go doc failed\nOutput: %s", string(output))
	}

	return string(output), nil
}

// readCommandSource reads the content of the main go file in the directory.
func (g *Generator) readCommandSource(path string) (string, error) {
	info, err := g.props.FS.Stat(path)
	if err != nil {
		return "", errors.Wrap(err, "failed to stat command source path")
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

	diataxis := false
	if m := g.readManifestQuiet(); m != nil && m.Properties.ResolvedDocsLayout() == DocsLayoutDiataxis {
		diataxis = true
	}

	packagesDir := filepath.Join(g.config.Path, "docs", "packages")
	if diataxis {
		// Component overviews are explanation-quadrant in the Diátaxis layout.
		packagesDir = filepath.Join(g.config.Path, "docs", "explanation", "components")
	}

	indexFile := filepath.Join(packagesDir, "index.md")

	packageRows := g.collectPackageIndexRows(packagesDir, indexFile, diataxis)

	content := fmt.Sprintf(`---
title: Package Reference
description: Index of project packages.
---

# Package Reference

| Package | Description |
| :--- | :--- |
%s
`, strings.Join(packageRows, "\n"))

	if err := g.props.FS.MkdirAll(packagesDir, DefaultDirMode); err != nil {
		return errors.Wrap(err, "failed to create packages index dir")
	}

	if err := afero.WriteFile(g.props.FS, indexFile, []byte(content), DefaultFileMode); err != nil {
		return errors.Wrap(err, "failed to write packages index")
	}

	return nil
}

// collectPackageIndexRows walks packagesDir and returns a Markdown table row for
// each documented package, skipping the index file itself. In the Diátaxis layout
// components are flat `<name>.md` files; in the legacy flat layout each package is
// a directory with its own index.md. Walk errors are logged, not fatal.
func (g *Generator) collectPackageIndexRows(packagesDir, indexFile string, diataxis bool) []string {
	packageRows := make([]string, 0)

	err := afero.Walk(g.props.FS, packagesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		var (
			row string
			ok  bool
		)

		if diataxis {
			row, ok = g.diataxisPackageRow(packagesDir, indexFile, path, info)
		} else {
			row, ok = g.flatPackageRow(packagesDir, indexFile, path, info)
		}

		if ok {
			packageRows = append(packageRows, row)
		}

		return nil
	})
	if err != nil {
		g.props.Logger.Warn("Error walking packages dir", "error", err)
	}

	return packageRows
}

// diataxisPackageRow builds the index row for a flat component file
// (explanation/components/<name>.md), or ok=false to skip dirs and the index.
func (g *Generator) diataxisPackageRow(packagesDir, indexFile, path string, info os.FileInfo) (string, bool) {
	if info.IsDir() || path == indexFile || !strings.HasSuffix(path, ".md") {
		return "", false
	}

	rel, _ := filepath.Rel(packagesDir, path)
	name := strings.TrimSuffix(rel, ".md")

	return g.packageIndexRow(name, filepath.ToSlash(rel), path), true
}

// flatPackageRow builds the index row for a legacy package directory (one with
// its own index.md), or ok=false to skip non-package directories.
func (g *Generator) flatPackageRow(packagesDir, indexFile, path string, info os.FileInfo) (string, bool) {
	if !info.IsDir() {
		return "", false
	}

	packageIndexFile := filepath.Join(path, "index.md")
	if path == packagesDir || packageIndexFile == indexFile {
		return "", false
	}

	if exists, _ := afero.Exists(g.props.FS, packageIndexFile); !exists {
		return "", false
	}

	rel, _ := filepath.Rel(packagesDir, path)

	return g.packageIndexRow(rel, filepath.ToSlash(rel)+"/", packageIndexFile), true
}

// packageIndexRow formats a single Markdown table row, reading the description
// from the doc's frontmatter.
func (g *Generator) packageIndexRow(name, link, docFile string) string {
	desc := "No description"

	if fm := getFrontmatter(g.props.FS, docFile); fm != nil {
		if d, ok := fm["description"].(string); ok {
			desc = d
		}
	}

	return fmt.Sprintf("| [%s](%s) | %s |", name, link, desc)
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

// Delimiters for the generated command table inside a commands index. Prose
// outside these markers survives regeneration; the table between them is
// rewritten in place. See docs/how-to/configure-generator-ignore.md and issue #6.
const (
	commandsIndexHeader      = "# Commands"
	commandsIndexMarkerStart = "<!-- gtb:commands:start -->"
	commandsIndexMarkerEnd   = "<!-- gtb:commands:end -->"
)

func (g *Generator) generateCommandsIndex() error {
	m, err := g.decodeManifestFile(ManifestPathFor(g.config.Path))
	if err != nil {
		return err
	}

	diataxis := m.Properties.ResolvedDocsLayout() == DocsLayoutDiataxis

	indexPath := filepath.Join(g.config.Path, "docs", "commands", "index.md")
	if diataxis {
		// CLI commands are reference-quadrant in the Diátaxis layout; there is no
		// docs/commands/ tree to write into.
		indexPath = filepath.Join(g.config.Path, "docs", "reference", "cli", "index.md")
	}

	relPath, err := filepath.Rel(g.config.Path, indexPath)
	if err != nil {
		relPath = indexPath
	}

	relPath = filepath.ToSlash(relPath)

	// A downstream can protect a hand-maintained index with .gtb/ignore, exactly
	// like a skeleton file — honour it and leave the file entirely untouched
	// (issue #6: the index writer must consult the same ignore rules).
	if LoadIgnoreRules(g.props.FS, g.config.Path).IsIgnored(relPath) {
		g.props.Logger.Debug("commands index ignored by .gtb/ignore; leaving as-is", "path", relPath)

		return nil
	}

	next, write := g.mergeCommandsIndex(indexPath, relPath, g.buildCommandsIndexTable(m.Commands, diataxis))
	if !write {
		return nil
	}

	g.props.Logger.Info("updating commands index", "path", indexPath)

	if err := g.props.FS.MkdirAll(filepath.Dir(indexPath), DefaultDirMode); err != nil {
		return errors.Wrap(err, "failed to create commands index dir")
	}

	if err := afero.WriteFile(g.props.FS, indexPath, []byte(next), DefaultFileMode); err != nil {
		return errors.Wrap(err, "failed to write commands index")
	}

	return nil
}

// mergeCommandsIndex computes the content to write for the commands index. It
// splices the freshly generated command table into the region delimited by the
// gtb:commands markers so any surrounding hand-added prose survives. It returns
// (content, true) when a write should happen, or ("", false) when the on-disk
// file has diverged from its generated form and must be preserved untouched —
// so `generate command` never silently discards manual content (issue #6).
func (g *Generator) mergeCommandsIndex(indexPath, relPath, table string) (string, bool) {
	existing, err := afero.ReadFile(g.props.FS, indexPath)
	if err != nil {
		// No existing file — write a fresh, marker-delimited index.
		return freshCommandsIndex(table), true
	}

	content := string(existing)

	start := strings.Index(content, commandsIndexMarkerStart)

	end := strings.Index(content, commandsIndexMarkerEnd)
	if start != -1 && end != -1 && end > start {
		block := commandsIndexMarkerStart + "\n" + table + commandsIndexMarkerEnd

		return content[:start] + block + content[end+len(commandsIndexMarkerEnd):], true
	}

	// No markers. Migrate an untouched, purely-generated legacy index to the
	// marker-delimited form so it keeps updating; preserve a hand-authored one.
	if isGeneratedCommandsIndex(content) {
		return freshCommandsIndex(table), true
	}

	g.props.Logger.Warn(
		"commands index has diverged from its generated form; preserving it "+
			"(wrap the command table in the gtb:commands markers, or list it in .gtb/ignore, to manage it)",
		"path", relPath)

	return "", false
}

// isGeneratedCommandsIndex reports whether content looks like an untouched,
// gtb-generated commands index — only the "# Commands" heading and Markdown
// table lines, with no hand-added prose. Used to safely migrate a pre-markers
// generated index to the marker-delimited form without clobbering user content.
func isGeneratedCommandsIndex(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		switch trimmed := strings.TrimSpace(line); {
		case trimmed == "",
			trimmed == commandsIndexHeader,
			strings.HasPrefix(trimmed, "|"):
		default:
			return false
		}
	}

	return true
}

// freshCommandsIndex renders a complete commands index from scratch: the heading
// followed by the marker-delimited command table.
func freshCommandsIndex(table string) string {
	return commandsIndexHeader + "\n\n" + commandsIndexMarkerStart + "\n" + table + commandsIndexMarkerEnd + "\n"
}

func (g *Generator) buildCommandsIndexContent(commands []ManifestCommand, diataxis bool) string {
	return freshCommandsIndex(g.buildCommandsIndexTable(commands, diataxis))
}

func (g *Generator) buildCommandsIndexTable(commands []ManifestCommand, diataxis bool) string {
	var content strings.Builder
	content.WriteString("| Command | Description |\n")
	content.WriteString("| :--- | :--- |\n")

	base := "commands"
	if diataxis {
		base = filepath.Join("reference", "cli")
	}

	var walk func(cmds []ManifestCommand, parentPath string)

	walk = func(cmds []ManifestCommand, parentPath string) {
		for _, cmd := range cmds {
			fullPath := cmd.Name
			if parentPath != "" {
				fullPath = parentPath + " " + cmd.Name
			}

			// One leaf-flat / parent-subsection rule for the link and the doc path.
			joined := strings.ReplaceAll(fullPath, " ", string(filepath.Separator))
			relPath := commandDocRelPath(joined, len(cmd.Commands) > 0, diataxis)

			// Try to read description from the generated doc's frontmatter.
			docPath := filepath.Join(g.config.Path, "docs", base, relPath)
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

			fmt.Fprintf(&content, "| [%s](%s) | %s |\n", fullPath, filepath.ToSlash(relPath), desc)

			walk(cmd.Commands, fullPath)
		}
	}

	walk(commands, "")

	return content.String()
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
		return errors.Wrap(err, "failed to create docs directory")
	}

	docPath := filepath.Join(docsDir, "index.md")

	f, err := g.props.FS.Create(docPath)
	if err != nil {
		return errors.Wrap(err, "failed to create docs file")
	}

	defer func() {
		_ = f.Close()
	}()

	if _, err = fmt.Fprintf(f, "# %s\n\n%s\n\n%s\n", g.config.Name, g.config.Short, g.config.Long); err != nil {
		return errors.Wrap(err, "failed to write docs content")
	}

	return g.regenerateMkdocsNav()
}

func (g *Generator) regenerateMkdocsNav() error {
	mkdocsPath := filepath.Join(g.config.Path, "mkdocs.yml")

	if exists, _ := afero.Exists(g.props.FS, mkdocsPath); !exists {
		// Projects on the current docs toolchain use zensical.toml, which has
		// no explicit nav list: zensical builds navigation from the docs/ tree
		// and the generated section index pages (commands/index.md,
		// packages/index.md). There is nothing to rewrite, so this is a quiet
		// no-op rather than the misleading "skipping navigation update" warning
		// it used to emit on every generate against a zensical project.
		zensicalPath := filepath.Join(g.config.Path, "zensical.toml")
		if exists, _ := afero.Exists(g.props.FS, zensicalPath); exists {
			g.props.Logger.Debug("zensical project: navigation is generated from the docs tree; no nav file to update")

			return nil
		}

		g.props.Logger.Debug("no mkdocs.yml or zensical.toml found; skipping navigation update")

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
		return nil, errors.Wrap(err, "failed to read mkdocs.yml")
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

	cliNav := buildNavFromCommands(m.Commands, []string{}, m.Properties.ResolvedDocsLayout() == DocsLayoutDiataxis)
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

func buildNavFromCommands(commands []ManifestCommand, parentPath []string, diataxis bool) []any {
	nav := make([]any, 0, len(commands))

	for _, cmd := range commands {
		currentPath := make([]string, len(parentPath)+1)
		copy(currentPath, parentPath)
		currentPath[len(parentPath)] = cmd.Name

		hasChildren := len(cmd.Commands) > 0
		relPath := navCommandPath(currentPath, hasChildren, diataxis)

		item := map[string]any{}
		displayName := toTitle(cmd.Name) // Simple title case or PascalCase if available

		if hasChildren {
			childrenNav := buildNavFromCommands(cmd.Commands, currentPath, diataxis)
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

// navCommandPath returns the docs-relative path used in the generated mkdocs nav
// for a command, honouring the docs layout: Diátaxis places CLI docs under
// reference/cli (a leaf as <path>.md, a parent as <path>/index.md), while the
// legacy flat layout uses commands/<path>/index.md. (zensical projects derive
// navigation from the docs tree and never reach this code.)
func navCommandPath(cmdPath []string, hasChildren, diataxis bool) string {
	joined := filepath.Join(cmdPath...)

	base := "commands"
	if diataxis {
		base = filepath.Join("reference", "cli")
	}

	return filepath.Join(base, commandDocRelPath(joined, hasChildren, diataxis))
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
