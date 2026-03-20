package generator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"github.com/phpboyscout/gtb/internal/generator/templates"
)

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

	writtenSkeletonHashes, err := g.regenerateSkeletonFiles(m)
	if err != nil {
		return err
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

		// Post-processing may have modified tracked skeleton files. Refresh
		// their hashes so the next run does not flag those changes as
		// user customisations.
		if err := g.refreshProjectFileHashes(g.config.Path, writtenSkeletonHashes); err != nil {
			g.props.Logger.Warn("Failed to refresh project file hashes after post-processing", "error", err)
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

	if err := g.postGenerate(ctx, data, cmdDir); err != nil {
		return err
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
	g.config.WithInitializer = cmd.WithInitializer
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

// buildSkeletonRootData constructs a complete SkeletonRootData from a Manifest
// so that regenerateRootCommand produces root/cmd.go with all project settings
// intact — including help channel configuration stored in Properties.Help.
func buildSkeletonRootData(m Manifest, subcommands []templates.SkeletonSubcommand) templates.SkeletonRootData {
	releaseProvider, org, repoName := m.GetReleaseSource()

	return templates.SkeletonRootData{
		Name:             m.Properties.Name,
		Description:      string(m.Properties.Description),
		ReleaseProvider:  releaseProvider,
		Host:             m.ReleaseSource.Host,
		Org:              org,
		RepoName:         repoName,
		Private:          m.ReleaseSource.Private,
		DisabledFeatures: calculateDisabledFeatures(m.Properties.Features),
		EnabledFeatures:  calculateEnabledFeatures(m.Properties.Features),
		HelpType:         m.Properties.Help.Type,
		SlackChannel:     m.Properties.Help.SlackChannel,
		SlackTeam:        m.Properties.Help.SlackTeam,
		TeamsChannel:     m.Properties.Help.TeamsChannel,
		TeamsTeam:        m.Properties.Help.TeamsTeam,
		Subcommands:      subcommands,
	}
}

func (g *Generator) regenerateRootCommand(m Manifest) error {
	g.props.Logger.Info("Regenerating root command...")

	subcommands, err := g.buildSkeletonSubcommands(m.Commands)
	if err != nil {
		return err
	}

	data := buildSkeletonRootData(m, subcommands)

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

// buildSkeletonSubcommands converts the top-level manifest commands into the
// SkeletonSubcommand descriptors that SkeletonRoot uses to render the
// gtbRoot.NewCmdRoot(p, sub1.NewCmdSub1(p), ...) call.
func (g *Generator) buildSkeletonSubcommands(commands []ManifestCommand) ([]templates.SkeletonSubcommand, error) {
	moduleName, err := g.getModuleName()
	if err != nil {
		return nil, err
	}

	subs := make([]templates.SkeletonSubcommand, 0, len(commands))

	for _, cmd := range commands {
		pkgAlias := strings.ReplaceAll(cmd.Name, "-", "_")
		importPath := fmt.Sprintf("%s/pkg/cmd/%s", moduleName, cmd.Name)
		constructor := "NewCmd" + PascalCase(cmd.Name)

		subs = append(subs, templates.SkeletonSubcommand{
			ImportPath:  importPath,
			PkgAlias:    pkgAlias,
			Constructor: constructor,
		})
	}

	return subs, nil
}

// regenerateSkeletonFiles re-applies the project skeleton template files
// (non-Go files such as CI configs, justfile, .goreleaser.yaml, etc.) from
// the current GTB version, protecting any user customisations via hash
// comparison before overwriting. Resulting hashes are persisted back to the
// manifest so subsequent runs can detect further modifications.
func (g *Generator) regenerateSkeletonFiles(m Manifest) (map[string]string, error) {
	g.props.Logger.Info("Regenerating project skeleton files...")

	_, org, repoName := m.GetReleaseSource()

	// Reconstruct template data from the manifest. GoVersion is not persisted
	// so we fall back to the current runtime version.
	data := struct {
		Name              string
		Repo              string
		Host              string
		ModulePath        string
		Description       string
		Org               string
		RepoName          string
		ReleaseProvider   string
		GoToolBaseVersion string
		GoVersion         string
		DisabledFeatures  []string
		EnabledFeatures   []string
		Private           bool
		HelpType          string
		SlackChannel      string
		SlackTeam         string
		TeamsChannel      string
		TeamsTeam         string
	}{
		Name:              m.Properties.Name,
		Repo:              org + "/" + repoName,
		Host:              m.ReleaseSource.Host,
		ModulePath:        m.ReleaseSource.Host + "/" + org + "/" + repoName,
		Description:       string(m.Properties.Description),
		Org:               org,
		RepoName:          repoName,
		ReleaseProvider:   m.ReleaseSource.Type,
		GoToolBaseVersion: g.currentVersion(),
		GoVersion:         resolveGoVersion(""),
		DisabledFeatures:  calculateDisabledFeatures(m.Properties.Features),
		EnabledFeatures:   calculateEnabledFeatures(m.Properties.Features),
		Private:           m.ReleaseSource.Private,
		HelpType:          m.Properties.Help.Type,
		SlackChannel:      m.Properties.Help.SlackChannel,
		SlackTeam:         m.Properties.Help.SlackTeam,
		TeamsChannel:      m.Properties.Help.TeamsChannel,
		TeamsTeam:         m.Properties.Help.TeamsTeam,
	}

	storedHashes := m.Hashes
	if storedHashes == nil {
		storedHashes = make(map[string]string)
	}

	writtenHashes, err := g.generateSkeletonTemplateFiles(g.config.Path, data, storedHashes)
	if err != nil {
		return nil, err
	}

	// Merge: keep stored hashes for files the user chose to skip so that
	// subsequent runs can still detect further modifications to those files.
	finalHashes := make(map[string]string, len(storedHashes)+len(writtenHashes))
	for k, v := range storedHashes {
		finalHashes[k] = v
	}

	for k, v := range writtenHashes {
		finalHashes[k] = v
	}

	return writtenHashes, g.persistProjectHashes(finalHashes)
}

// persistProjectHashes reads the current manifest, updates the top-level
// Hashes field, and writes it back to disk.
func (g *Generator) persistProjectHashes(hashes map[string]string) error {
	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")

	raw, err := afero.ReadFile(g.props.FS, manifestPath)
	if err != nil {
		return errors.Newf("failed to read manifest: %w", err)
	}

	var m Manifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return errors.Newf("failed to unmarshal manifest: %w", err)
	}

	m.Hashes = hashes

	f, err := g.props.FS.Create(manifestPath)
	if err != nil {
		return errors.Newf("failed to open manifest for writing: %w", err)
	}

	defer func() { _ = f.Close() }()

	enc := yaml.NewEncoder(f)

	const indent = 2
	enc.SetIndent(indent)

	if err := enc.Encode(m); err != nil {
		return errors.Newf("failed to write manifest: %w", err)
	}

	return nil
}
