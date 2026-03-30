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

	"github.com/phpboyscout/go-tool-base/internal/generator/templates"
)

func (g *Generator) RegenerateProject(ctx context.Context) error {
	if g.config.DryRun {
		result, err := g.RegenerateProjectDryRun(ctx)
		if err != nil {
			return err
		}

		result.Print(os.Stdout)

		return nil
	}

	return g.regenerateProject(ctx)
}

// RegenerateProjectDryRun previews what RegenerateProject would do without writing to disk.
func (g *Generator) RegenerateProjectDryRun(ctx context.Context) (*DryRunResult, error) {
	if err := g.verifyProject(); err != nil {
		return nil, err
	}

	g.props.Logger.Info("Dry run: previewing project regeneration...")

	return g.withDryRunOverlay(ctx, g.config.Path, func() error {
		return g.regenerateProjectFiles(ctx)
	}, &dryRunPostProcess{
		commands: [][]string{
			{"go", "mod", "tidy"},
			{"golangci-lint", "run", "--fix"},
		},
	})
}

func (g *Generator) regenerateProject(ctx context.Context) error {
	if err := g.verifyProject(); err != nil {
		return err
	}

	if err := g.regenerateProjectFiles(ctx); err != nil {
		return err
	}

	// Post-processing: run linter and refresh hashes on real filesystem only.
	writtenSkeletonHashes, err := g.collectSkeletonHashes()
	if err == nil {
		g.runPostRegenerationLint(ctx, writtenSkeletonHashes)
	}

	g.props.Logger.Info("Project regeneration complete.")

	return nil
}

// regenerateProjectFiles performs the core regeneration: root command, recursive
// commands, and skeleton files. It does not run post-processing shell commands.
func (g *Generator) regenerateProjectFiles(ctx context.Context) error {
	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")

	g.props.Logger.Debugf("Reading manifest from %s", manifestPath)

	data, err := afero.ReadFile(g.props.FS, manifestPath)
	if err != nil {
		return errors.Newf("failed to read manifest: %w", err)
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return errors.Newf("failed to unmarshal manifest: %w", err)
	}

	g.props.Logger.Info("Regenerating project from manifest...")
	g.props.Logger.Debugf("Manifest: %s, %d top-level commands", m.Properties.Name, len(m.Commands))

	// If the flag was provided, update all commands in the manifest
	if g.config.WrapSubcommandsWithMiddleware != nil {
		updateWrapSubcommandsRecursive(&m.Commands, *g.config.WrapSubcommandsWithMiddleware)

		// Save updated manifest
		updated, err := yaml.Marshal(m)
		if err == nil {
			_ = afero.WriteFile(g.props.FS, manifestPath, updated, DefaultFileMode)
		}
	}

	if err := g.regenerateRootCommand(m); err != nil {
		return err
	}

	for _, cmd := range m.Commands {
		g.props.Logger.Debugf("Processing top-level command: %s", cmd.Name)

		if err := g.regenerateCommandRecursive(ctx, cmd, []string{}); err != nil {
			return err
		}
	}

	g.props.Logger.Debug("Regenerating skeleton files...")

	_, err = g.regenerateSkeletonFiles(m)

	return err
}

// collectSkeletonHashes loads the current project file hashes from the manifest.
func (g *Generator) collectSkeletonHashes() (map[string]string, error) {
	m, err := g.loadManifest()
	if err != nil {
		return nil, err
	}

	return m.Hashes, nil
}

// runPostRegenerationLint runs golangci-lint --fix and refreshes skeleton file
// hashes on an OS filesystem. It is a no-op for in-memory filesystems used in
// tests.
func (g *Generator) runPostRegenerationLint(ctx context.Context, writtenHashes map[string]string) {
	if _, ok := g.props.FS.(*afero.OsFs); !ok {
		return
	}

	g.props.Logger.Info("Running golangci-lint run --fix...")

	cmd := exec.CommandContext(ctx, "golangci-lint", "run", "--fix")
	cmd.Dir = g.config.Path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		g.props.Logger.Warn("Failed to run golangci-lint", "error", err)
	}

	// Post-processing may have modified tracked skeleton files. Refresh
	// their hashes so the next run does not flag those changes as user customisations.
	if err := g.refreshProjectFileHashes(g.config.Path, writtenHashes); err != nil {
		g.props.Logger.Warn("Failed to refresh project file hashes after post-processing", "error", err)
	}
}

func (g *Generator) regenerateCommandRecursive(ctx context.Context, cmd ManifestCommand, parentPath []string) error {
	g.props.Logger.Debugf("Building command context for %q (parent=%v)", cmd.Name, parentPath)

	// Build an immutable CommandContext for this command — no shared-state mutation.
	cmdCtx := buildCommandContext(g.config.Path, g.config.DryRun, g.config.Force, g.config.UpdateDocs, cmd, parentPath)

	// Swap g.config to the context-derived config for the duration of this
	// call. Downstream methods (getCommandPath, prepareGenerationData, etc.)
	// all read g.config, so this is the minimal-change bridge until they are
	// individually refactored to accept CommandContext directly.
	savedConfig := g.config
	g.config = cmdCtx.ToConfig()

	defer func() { g.config = savedConfig }()

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

	// Recurse for subcommands — each gets its own CommandContext via the
	// recursive call, so sibling state can never leak.
	childPath := append(append([]string{}, parentPath...), cmd.Name)

	if len(cmd.Commands) > 0 {
		g.props.Logger.Debugf("Recursing into %d subcommands of %q", len(cmd.Commands), cmd.Name)
	}

	for _, subCmd := range cmd.Commands {
		if err := g.regenerateCommandRecursive(ctx, subCmd, childPath); err != nil {
			return err
		}
	}

	return nil
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
		Name:                  m.Properties.Name,
		Description:           string(m.Properties.Description),
		ReleaseProvider:       releaseProvider,
		Host:                  m.ReleaseSource.Host,
		Org:                   org,
		RepoName:              repoName,
		Private:               m.ReleaseSource.Private,
		DisabledFeatures:      calculateDisabledFeatures(m.Properties.Features),
		EnabledFeatures:       calculateEnabledFeatures(m.Properties.Features),
		HelpType:              m.Properties.Help.Type,
		SlackChannel:          m.Properties.Help.SlackChannel,
		SlackTeam:             m.Properties.Help.SlackTeam,
		TeamsChannel:          m.Properties.Help.TeamsChannel,
		TeamsTeam:             m.Properties.Help.TeamsTeam,
		TelemetryEndpoint:     m.Properties.Telemetry.Endpoint,
		TelemetryOTelEndpoint: m.Properties.Telemetry.OTelEndpoint,
		Subcommands:           subcommands,
	}
}

func (g *Generator) regenerateRootCommand(m Manifest) error {
	g.props.Logger.Info("Regenerating root command...")
	g.props.Logger.Debugf("Building skeleton subcommands for %d top-level commands", len(m.Commands))

	subcommands, err := g.buildSkeletonSubcommands(m.Commands)
	if err != nil {
		return err
	}

	data := buildSkeletonRootData(m, subcommands)

	f := templates.SkeletonRoot(data)

	rootCmdPath := filepath.Join(g.config.Path, "pkg", "cmd", "root", "cmd.go")

	g.props.Logger.Debugf("Writing root command to %s", rootCmdPath)

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
	g.props.Logger.Debugf("Existing hashes: %d entries", len(m.Hashes))

	_, org, repoName := m.GetReleaseSource()

	// Reconstruct template data from the manifest. GoVersion is not persisted
	// so we fall back to the current runtime version.
	data := struct {
		Name                  string
		Repo                  string
		Host                  string
		ModulePath            string
		Description           string
		Org                   string
		RepoName              string
		ReleaseProvider       string
		GoToolBaseVersion     string
		GoVersion             string
		DisabledFeatures      []string
		EnabledFeatures       []string
		Private               bool
		HelpType              string
		SlackChannel          string
		SlackTeam             string
		TeamsChannel          string
		TeamsTeam             string
		TelemetryEndpoint     string
		TelemetryOTelEndpoint string
	}{
		Name:                  m.Properties.Name,
		Repo:                  org + "/" + repoName,
		Host:                  m.ReleaseSource.Host,
		ModulePath:            m.ReleaseSource.Host + "/" + org + "/" + repoName,
		Description:           string(m.Properties.Description),
		Org:                   org,
		RepoName:              repoName,
		ReleaseProvider:       m.ReleaseSource.Type,
		GoToolBaseVersion:     g.currentVersion(),
		GoVersion:             resolveGoVersion(""),
		DisabledFeatures:      calculateDisabledFeatures(m.Properties.Features),
		EnabledFeatures:       calculateEnabledFeatures(m.Properties.Features),
		Private:               m.ReleaseSource.Private,
		HelpType:              m.Properties.Help.Type,
		SlackChannel:          m.Properties.Help.SlackChannel,
		SlackTeam:             m.Properties.Help.SlackTeam,
		TeamsChannel:          m.Properties.Help.TeamsChannel,
		TeamsTeam:             m.Properties.Help.TeamsTeam,
		TelemetryEndpoint:     m.Properties.Telemetry.Endpoint,
		TelemetryOTelEndpoint: m.Properties.Telemetry.OTelEndpoint,
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

	g.props.Logger.Debugf("Skeleton regeneration complete: %d files written, %d total hashes", len(writtenHashes), len(finalHashes))

	return writtenHashes, g.persistProjectHashes(finalHashes)
}

// persistProjectHashes reads the current manifest, updates the top-level
// Hashes field, and writes it back to disk.
func (g *Generator) persistProjectHashes(hashes map[string]string) error {
	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")

	g.props.Logger.Debugf("Persisting %d project hashes to manifest", len(hashes))

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

// updateWrapSubcommandsRecursive sets the WrapSubcommandsWithMiddleware flag
// for all commands in the tree.
func updateWrapSubcommandsRecursive(commands *[]ManifestCommand, value bool) {
	for i := range *commands {
		(*commands)[i].WrapSubcommandsWithMiddleware = value
		updateWrapSubcommandsRecursive(&(*commands)[i].Commands, value)
	}
}
