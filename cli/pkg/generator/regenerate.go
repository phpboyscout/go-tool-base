package generator

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
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
			{"golangci-lint", "run"},
		},
	})
}

// shouldMigrateDocsToDiataxis reports whether regenerate should migrate the docs
// layout: only under --force, and only when the project is not already on the
// Diátaxis layout. A plain regenerate (no --force) never moves a downstream user's
// docs tree.
func (g *Generator) shouldMigrateDocsToDiataxis() bool {
	if !g.config.Force {
		return false
	}

	m := g.readManifestQuiet()

	return m != nil && m.Properties.ResolvedDocsLayout() != DocsLayoutDiataxis
}

func (g *Generator) regenerateProject(ctx context.Context) error {
	if err := g.verifyProject(); err != nil {
		return err
	}

	g.conflicts.reset()

	// Route the whole regeneration (docs migration, file writes and the
	// manifest/hash persistence) through a staged overlay and commit it to the
	// real filesystem only once every step succeeds; the commit rolls back if
	// it fails part-way. A failure at either stage leaves the tree and manifest
	// mutually consistent, rather than half-written.
	staged, genErr := g.withStagedFS(ctx, g.stagedRegeneration)
	if genErr != nil {
		// Nothing was materialised: base tree and manifest are untouched.
		return genErr
	}

	if err := staged.materialise(); err != nil {
		return errors.Wrap(err, "failed to commit regenerated project")
	}

	// Post-processing (linter, hash refresh) runs against the now-committed tree
	// on the real filesystem only.
	var failed []string

	writtenSkeletonHashes, err := g.collectSkeletonHashes()
	if err != nil {
		g.props.Logger.Warn("skipping post-regeneration processing: failed to load skeleton hashes", "error", err)
	} else {
		failed = g.runPostRegenerationProcessing(ctx, writtenSkeletonHashes)
	}

	g.reportConflicts()

	if err := notVerified(failed); err != nil {
		g.props.Logger.Warn("project regenerated, not verified")

		return err
	}

	g.props.Logger.Info("Project regeneration complete.")

	return nil
}

// withStagedFS runs body with a staged overlay installed as Props.FS and
// hands the overlay back for the caller to commit. The restore is deferred
// so a panic in body cannot leave the process reading through the overlay.
func (g *Generator) withStagedFS(ctx context.Context, body func(context.Context) error) (*stagedFS, error) {
	base := g.props.FS
	staged := newStagedFS(base)
	g.props.FS = staged

	defer func() { g.props.FS = base }()

	return staged, body(ctx)
}

// stagedRegeneration runs the docs migration and core regeneration against the
// currently-installed (staged) filesystem. It is the buffered body that
// regenerateProject commits, with rollback, once it succeeds.
func (g *Generator) stagedRegeneration(ctx context.Context) error {
	// `regenerate project --force` migrates a flat-layout project to the Diátaxis
	// layout before regeneration, so the re-emitted docs and indexes land in the
	// new tree rather than recreating the old one.
	if g.shouldMigrateDocsToDiataxis() {
		if err := g.migrateFlatDocsToDiataxis(); err != nil {
			return err
		}
	}

	return g.regenerateProjectFiles(ctx)
}

// regenerateProjectFiles performs the core regeneration: root command, recursive
// commands, and skeleton files. It does not run post-processing shell commands.
func (g *Generator) regenerateProjectFiles(ctx context.Context) error {
	manifestPath := ManifestPathFor(g.config.Path)

	g.props.Logger.Debug("reading manifest", "path", manifestPath)

	m, err := g.decodeManifestFile(manifestPath)
	if err != nil {
		return err
	}

	// Skip-not-abort (spec D1/O3): drop invalid commands from the in-memory
	// manifest with an ERROR log, so the valid entries still regenerate while
	// the traversal sink is foreclosed. The signing block is not skipped: for
	// trust configuration "skip" would mean rendering the disabled state and
	// removing enforcement the author declared (#40), so ValidateManifest
	// below treats it as structural and nothing is written.
	g.sanitiseManifest(m)

	if err := ValidateManifest(m); err != nil {
		return errors.Newf("manifest validation failed: %w", err)
	}

	g.props.Logger.Info("Regenerating project from manifest...")
	g.props.Logger.Debug("manifest loaded", "name", m.Properties.Name, "commands", len(m.Commands))

	// The shared sync (derived fields, root, signing files, adapter files)
	// runs first so the commands and skeleton files below render against a
	// manifest whose derived fields are recorded (spec 0197 D7).
	if err := g.syncDerivedFromManifest(m); err != nil {
		return err
	}

	for _, cmd := range m.Commands {
		g.props.Logger.Debug("processing top-level command", "name", cmd.Name)

		if err := g.regenerateCommandRecursive(ctx, cmd, []string{}); err != nil {
			return err
		}
	}

	g.props.Logger.Debug("Regenerating skeleton files...")

	if _, err = g.regenerateSkeletonFiles(*m); err != nil {
		return err
	}

	// The skeleton pass re-renders the commands index from its asset, whose
	// table is empty, and the docs pass above skips a page that exists, so
	// the table is filled from the manifest once everything else has run.
	if len(m.Commands) > 0 {
		if err := g.generateCommandsIndex(); err != nil {
			g.props.Logger.Warn("failed to refresh the commands index", "error", err)
		}
	}

	return nil
}

// sanitiseManifest removes manifest commands that fail validation so the
// remaining valid entries can still regenerate (skip-not-abort, spec D1/O3).
// Each skipped command is surfaced with an ERROR-level log naming the entry
// and the rule it failed and is never acted on, so the filepath.Join /
// RemoveAll traversal sink is foreclosed. The manifest is only modified in
// memory; the on-disk file is left for the user to fix.
func (g *Generator) sanitiseManifest(m *Manifest) {
	m.Commands = g.sanitiseManifestCommands(m.Commands)
}

// sanitiseManifestCommands returns the commands whose names pass
// ValidateCommandName, recursing into subcommands, and logs an ERROR for
// every command it skips.
func (g *Generator) sanitiseManifestCommands(cmds []ManifestCommand) []ManifestCommand {
	valid := make([]ManifestCommand, 0, len(cmds))

	for _, cmd := range cmds {
		if err := ValidateCommandName(cmd.Name); err != nil {
			g.props.Logger.Error("Skipping command with invalid name in manifest",
				"command", truncateInput(cmd.Name, truncatedInputLen),
				"reason", validationReason(err))

			continue
		}

		cmd.Commands = g.sanitiseManifestCommands(cmd.Commands)
		valid = append(valid, cmd)
	}

	return valid
}

// validationReason renders a validator error for logging: the hint carries
// the field, the rule, and the (truncated) offending input.
func validationReason(err error) string {
	if hints := errors.FlattenHints(err); hints != "" {
		return hints
	}

	return err.Error()
}

// collectSkeletonHashes loads the current project file hashes from the manifest.
func (g *Generator) collectSkeletonHashes() (map[string]string, error) {
	m, err := g.loadManifest()
	if err != nil {
		return nil, err
	}

	return m.Hashes, nil
}

// runPostRegenerationProcessing runs go mod tidy then golangci-lint (verify only) and
// refreshes skeleton file hashes on an OS filesystem. It is a no-op for
// in-memory filesystems used in tests.
//
// The tidy is load-bearing and mirrors generate's runSkeletonPostProcessing:
// regenerate rewrites go.mod from the embedded template snapshot, which omits
// the `tool` directives' transitive dependencies. Without a tidy the project is
// left with an under-resolved go.mod (the tool block present but its deps
// stripped) that a plain `go mod tidy` would immediately re-add — so a freshly
// generated, unchanged project would not survive a regenerate unchanged. Tidying
// here keeps regenerate's go.mod byte-identical to generate's, and because the
// hash refresh runs afterwards the manifest records the tidied go.mod's hash.
func (g *Generator) runPostRegenerationProcessing(ctx context.Context, writtenHashes map[string]string) []string {
	if _, ok := g.props.FS.(*afero.OsFs); !ok {
		return nil
	}

	var failed []string

	if g.config.NoVerify {
		g.props.Logger.Warn("verification skipped (--no-verify): the tree was emitted, not verified")
	} else {
		failed = g.verifyTree(ctx, g.config.Path, lintVerify)
	}

	// Post-processing (tidy, lint) may have modified tracked files in either
	// namespace. Refresh both so the next run does not flag those changes as
	// user customisations: project-level files in Manifest.Hashes, and command
	// files in ManifestCommand.Hashes. Refreshing only the first left command
	// files diverged from their own record (issue #14).
	if err := g.refreshProjectFileHashes(g.config.Path, writtenHashes); err != nil {
		g.props.Logger.Warn("Failed to refresh project file hashes after post-processing", "error", err)
	}

	if err := g.refreshCommandFileHashes(g.config.Path); err != nil {
		g.props.Logger.Warn("Failed to refresh command file hashes after post-processing", "error", err)
	}

	return failed
}

// RegenerateCommand regenerates a single command's cmd.go from its full
// ManifestCommand record and persists the refreshed cmd.go hash back to the
// manifest. It reuses the exact mapping the `regenerate project` path uses
// (prepareRegenerationData → performGeneration → postGenerate), so a command's
// aliases, persistent/required/shorthand flags, and pre-run hooks survive the
// regeneration rather than being dropped by a hand-built partial CommandData.
//
// Unlike regenerateCommandRecursive it does NOT recurse into subcommands: it is
// the entrypoint for the targeted `generate add-flag` regeneration, which must
// only rewrite the command whose flag set changed. Child registrations in the
// regenerated cmd.go are preserved by the pipeline's reRegisterChildCommands
// step.
func (g *Generator) RegenerateCommand(ctx context.Context, cmd ManifestCommand, parentPath []string) error {
	cmdCtx := buildCommandContext(g.config, cmd, parentPath)

	savedConfig := g.config
	g.config = cmdCtx.ToConfig()

	defer func() { g.config = savedConfig }()

	cmdDir, err := g.getCommandPath()
	if err != nil {
		return err
	}

	if err := g.props.FS.MkdirAll(cmdDir, DefaultDirMode); err != nil {
		return errors.Newf("failed to create command directory: %w", err)
	}

	data := g.prepareRegenerationData(cmd)

	if err := g.performGeneration(ctx, cmdDir, &data); err != nil {
		return err
	}

	// Skip documentation generation: add-flag is a flag-set change, not a docs
	// operation, and the docs step requires AI configuration that the add-flag
	// caller does not wire up. The manifest step still runs, persisting the
	// refreshed cmd.go hash and the full flag set.
	_, err = newCommandPipeline(g, PipelineOptions{SkipDocumentation: true}).Run(ctx, data, cmdDir)

	return err
}

func (g *Generator) regenerateCommandRecursive(ctx context.Context, cmd ManifestCommand, parentPath []string) error {
	g.props.Logger.Debug("building command context", "name", cmd.Name, "parent", parentPath)

	// Build an immutable CommandContext for this command — no shared-state mutation.
	cmdCtx := buildCommandContext(g.config, cmd, parentPath)

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

	g.props.Logger.Info("regenerating command", "name", cmd.Name, "path", cmdDir)

	if err := g.props.FS.MkdirAll(cmdDir, DefaultDirMode); err != nil {
		return errors.Newf("failed to create command directory: %w", err)
	}

	data := g.prepareRegenerationData(cmd)

	if err := g.performGeneration(ctx, cmdDir, &data); err != nil {
		return err
	}

	// A regenerate rewrites what the manifest describes; its AI pass over the
	// docs is what --update-docs asks for, never a side effect (#35).
	if err := g.postGenerateWith(ctx, data, cmdDir, PipelineOptions{SkipDocumentation: g.config.DryRun, BoilerplateDocsOnly: !g.config.UpdateDocs}); err != nil {
		return err
	}

	// If this command's cmd.go was kept, queue the end-of-run check for
	// whether keeping it leaves any of its manifest children unregistered.
	g.recordChildCheck(cmdDir, cmd)

	// Recurse for subcommands — each gets its own CommandContext via the
	// recursive call, so sibling state can never leak.
	childPath := append(append([]string{}, parentPath...), cmd.Name)

	if len(cmd.Commands) > 0 {
		g.props.Logger.Debug("recursing into subcommands", "count", len(cmd.Commands), "name", cmd.Name)
	}

	for _, subCmd := range cmd.Commands {
		if err := g.regenerateCommandRecursive(ctx, subCmd, childPath); err != nil {
			return err
		}
	}

	return nil
}

func (g *Generator) prepareRegenerationData(cmd ManifestCommand) templates.CommandData {
	flags := g.resolveGenerationFlags()
	data := g.prepareGenerationData(flags)
	data.Aliases = g.config.Aliases
	data.Args = g.config.Args
	data.Hidden = g.config.Hidden
	data.MutuallyExclusive = cmd.MutuallyExclusive
	data.RequiredTogether = cmd.RequiredTogether

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
		ReleaseBaseURL:        m.ReleaseSource.Static.BaseURL,
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
		EnvPrefix:             m.Properties.EnvPrefix,
		ConfigLayers:          m.Properties.Config.Layers,
		UpdatePolicy:          m.Properties.UpdatePolicy,
		UpdateCheckInterval:   m.Properties.UpdateCheckInterval,
		MCPMode:               m.Properties.MCP.Mode,
		SigningEnabled:        m.Properties.Signing.Enabled,
		ModulePath:            manifestModulePath(m),
		AutoInitialise:        m.Properties.Bootstrap.AutoInitialise,
		SkipConfigCheck:       m.Properties.Bootstrap.SkipConfigCheck,
		AuxiliaryCommands:     m.Properties.Bootstrap.AuxiliaryCommands,
		RequireChecksum:       m.Properties.Signing.RequireChecksum,
		Subcommands:           subcommands,
		ExternalCommands:      buildSkeletonExternalCommands(m.Properties.ExternalCommands),
		ExternalAdapter:       m.Properties.ExternalCommandsAdapter,
	}
}

func (g *Generator) regenerateRootCommand(m Manifest) error {
	// The root carries no hash, so it cannot conflict, but a rule still
	// outranks the write (#32): `sealed` is "never written, wiring included",
	// and a plain rule is "leave it alone". Recorded so the summary is true.
	if !g.managedGeneratedFile("pkg/cmd/root/cmd.go") {
		return nil
	}

	g.props.Logger.Info("Regenerating root command...")
	g.props.Logger.Debug("building skeleton subcommands", "commands", len(m.Commands))

	subcommands, err := g.buildSkeletonSubcommands(m.Commands)
	if err != nil {
		return err
	}

	data := buildSkeletonRootData(m, subcommands)

	f := templates.SkeletonRoot(data)

	rootCmdPath := filepath.Join(g.config.Path, "pkg", "cmd", "root", "cmd.go")

	g.props.Logger.Debug("writing root command", "path", rootCmdPath)

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

// skeletonTemplateData is the data passed to the non-Go skeleton asset
// templates (CI configs, justfile, .goreleaser.yaml, ...) when
// reconstructed from a manifest. Shared by the full regenerate and the
// targeted .goreleaser.yaml re-render that enable/disable signing performs.
type skeletonTemplateData struct {
	Name            string
	Repo            string
	Host            string
	ModulePath      string
	Description     string
	Org             string
	RepoName        string
	ReleaseProvider string
	// ReleaseBaseURL is the static channel's location (spec 0203 D1), set
	// only when ReleaseProvider is static.
	ReleaseBaseURL string
	// ForgeBackend chooses the CI skeleton (spec 0195 D8); empty on a
	// manifest that predates the field, when the provider decides.
	ForgeBackend      props.FeatureID
	GoToolBaseVersion string
	GoVersion         string
	// FrameworkReplace, when set, points the generated go.mod at a framework
	// working tree with a replace directive. It is read from
	// GTB_FRAMEWORK_REPLACE at every render (generate and regenerate alike)
	// and recorded nowhere, so a scaffold can be built against unreleased
	// framework API during development and the e2e suite, and the same tree
	// regenerated later without the directive is a normal project.
	FrameworkReplace string
	DisabledFeatures []string
	// Links are the framework links the scaffold carries as cmd/<name>/<id>.go:
	// the manifest's entry decides, the link's default otherwise (spec 0197
	// D8, spec 0202 D6).
	Links           []props.FeatureDescriptor
	EnabledFeatures []string
	// ChatModules and ForgeModules are the blank imports cmd/<name>/chat.go and
	// forge.go carry, derived from the manifest's chat.providers and enabled
	// forge features (spec 0194 D4, D6). ChatProviders and ForgeLinks are the
	// names those files declare as link features (#81).
	ChatModules   []string
	ChatProviders []string
	ForgeLinks    []string
	// ChatFile decides whether cmd/<name>/chat.go exists at all: a linked
	// provider or the ai feature; under ai an empty ChatProviders is still a
	// file (spec 0197 D8, D9).
	ChatFile bool
	// ChatDefault is the author's chat default, rendered as the ai defaults
	// bundle beside chat.go when set (spec 0196 D4).
	ChatDefault           ManifestChatDefault
	ForgeModules          []string
	Private               bool
	HelpType              string
	SlackChannel          string
	SlackTeam             string
	TeamsChannel          string
	TeamsTeam             string
	TelemetryEndpoint     string
	TelemetryOTelEndpoint string
	EnvPrefix             string
	ConfigLayers          []string
	UpdatePolicy          string
	UpdateCheckInterval   string
	MCPMode               string
	Signing               ManifestSigning
	Bootstrap             ManifestBootstrap
	// CIComponentSource is the resolved phpboyscout/cicd include base for
	// the scaffolded GitLab pipeline (defaulted to DefaultCICDComponentSource
	// when the manifest carries no override).
	CIComponentSource string
	// CICDComponentVersion is the pinned phpboyscout/cicd component version
	// sourced from the generator constant (lockstep with the framework),
	// interpolated into the pipeline includes.
	CICDComponentVersion string
	// CIEnableE2E controls the go-test component's enable_e2e input. A freshly
	// generated tool has no E2E suite, so this is false.
	CIEnableE2E bool
}

// GetReleaseProvider satisfies releaseProviderAccessor so
// extractReleaseProvider can read the provider without the reflection
// fallback now that every caller passes the named skeletonTemplateData.
func (d skeletonTemplateData) GetReleaseProvider() string { return d.ReleaseProvider }

// StaticChannel reports whether the tool releases on the static channel, the
// condition the release configuration renders its additions under (spec 0203
// D5, D7).
func (d skeletonTemplateData) StaticChannel() bool {
	return d.ReleaseProvider == props.ReleaseSourceStatic
}

// ForgeTokenType is the forge whose token goreleaser reads: the release
// provider on the forge channel, the backend when the tool releases on the
// static channel but is still hosted.
func (d skeletonTemplateData) ForgeTokenType() string {
	if d.StaticChannel() {
		return string(d.ForgeBackend)
	}

	return d.ReleaseProvider
}

// Hosted reports whether the project lives on a forge; a project that is not
// hosted has no forge release object to create or upload to.
func (d skeletonTemplateData) Hosted() bool { return d.ForgeBackend != "" || d.Host != "" }

// ReleaseBasePath is the base URL's path without its leading slash: the key
// prefix a tag's objects are published under on an S3-style store.
func (d skeletonTemplateData) ReleaseBasePath() string {
	u, err := url.Parse(d.ReleaseBaseURL)
	if err != nil {
		return ""
	}

	return strings.Trim(u.Path, "/")
}

// GetForgeBackend satisfies forgeBackendAccessor for the CI skeleton choice.
func (d skeletonTemplateData) GetForgeBackend() props.FeatureID { return d.ForgeBackend }

// buildSkeletonTemplateData reconstructs the skeleton asset template data
// from a manifest.
func (g *Generator) buildSkeletonTemplateData(m Manifest) skeletonTemplateData {
	data := buildSkeletonTemplateDataFrom(m)
	data.GoToolBaseVersion = g.currentVersion()

	return data
}

// buildSkeletonTemplateDataFrom is the pure part of buildSkeletonTemplateData,
// so a round-trip test can reach it without a Generator.
func buildSkeletonTemplateDataFrom(m Manifest) skeletonTemplateData {
	_, org, repoName := m.GetReleaseSource()

	return skeletonTemplateData{
		Name:                  m.Properties.Name,
		Repo:                  org + "/" + repoName,
		Host:                  m.ReleaseSource.Host,
		ModulePath:            manifestModulePath(m),
		Description:           string(m.Properties.Description),
		Org:                   org,
		RepoName:              repoName,
		ReleaseProvider:       m.ReleaseSource.Type,
		ReleaseBaseURL:        m.ReleaseSource.Static.BaseURL,
		ForgeBackend:          m.ReleaseSource.Backend,
		GoVersion:             resolveGoVersion(m.Version.Go),
		FrameworkReplace:      frameworkReplace(),
		DisabledFeatures:      calculateDisabledFeatures(m.Properties.Features),
		EnabledFeatures:       calculateEnabledFeatures(m.Properties.Features),
		Links:                 enabledLinks(m.Properties.Features),
		ChatModules:           chatModulesFor(m.Properties.Chat.Providers),
		ChatProviders:         chatProvidersFor(m.Properties.Chat.Providers),
		ForgeLinks:            enabledForges(m.Properties.Features),
		ChatDefault:           chatDefaultsFor(m.Properties),
		ForgeModules:          forgeModules(m.Properties.Features),
		Private:               m.ReleaseSource.Private,
		HelpType:              m.Properties.Help.Type,
		SlackChannel:          m.Properties.Help.SlackChannel,
		SlackTeam:             m.Properties.Help.SlackTeam,
		TeamsChannel:          m.Properties.Help.TeamsChannel,
		TeamsTeam:             m.Properties.Help.TeamsTeam,
		TelemetryEndpoint:     m.Properties.Telemetry.Endpoint,
		TelemetryOTelEndpoint: m.Properties.Telemetry.OTelEndpoint,
		EnvPrefix:             m.Properties.EnvPrefix,
		ConfigLayers:          m.Properties.Config.Layers,
		UpdatePolicy:          m.Properties.UpdatePolicy,
		UpdateCheckInterval:   m.Properties.UpdateCheckInterval,
		MCPMode:               m.Properties.MCP.Mode,
		Signing:               m.Properties.Signing,
		Bootstrap:             m.Properties.Bootstrap,
		CIComponentSource:     resolveCIComponentSource(m.Properties.CI.ComponentSource),
		CICDComponentVersion:  CICDComponentVersion,
		CIEnableE2E:           false,
	}
}

// regenerateSkeletonFiles re-applies the project skeleton template files
// (non-Go files such as CI configs, justfile, .goreleaser.yaml, etc.) from
// the current GTB version, protecting any user customisations via hash
// comparison before overwriting. Resulting hashes are persisted back to the
// manifest so subsequent runs can detect further modifications.
func (g *Generator) regenerateSkeletonFiles(m Manifest) (map[string]string, error) {
	g.props.Logger.Info("Regenerating project skeleton files...")
	g.props.Logger.Debug("existing hashes", "entries", len(m.Hashes))

	data := g.buildSkeletonTemplateData(m)

	storedHashes := m.Hashes
	if storedHashes == nil {
		storedHashes = make(map[string]string)
	}

	// go.mod stopped being a rendered, hash-compared file (spec 0200 D1); an
	// older manifest's entry is dropped here rather than merged back and
	// refreshed on every run (#88).
	delete(storedHashes, "go.mod")

	// The same set the conflict resolver uses, so a rule cannot cover a file
	// at one stage of the run and miss it at another.
	ignoreRules := g.ignoreRules()

	writtenHashes, updatedSources, err := g.generateSkeletonTemplateFilesWithSources(g.config.Path, data, storedHashes, ignoreRules, m.Properties.Templates)
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

	g.props.Logger.Debug("skeleton regeneration complete", "written", len(writtenHashes), "hashes", len(finalHashes))

	return writtenHashes, g.persistProjectHashesAndSources(finalHashes, updatedSources)
}

// persistProjectHashesAndSources updates the top-level Hashes field and (when
// provided) the templates source entries with their refreshed pins/hashes,
// then writes the manifest back to disk.
func (g *Generator) persistProjectHashesAndSources(hashes map[string]string, sources []TemplateSource) error {
	manifestPath := ManifestPathFor(g.config.Path)

	g.props.Logger.Debug("persisting project hashes", "hashes", len(hashes))

	m, err := g.decodeManifestFile(manifestPath)
	if err != nil {
		return err
	}

	m.Hashes = hashes

	if sources != nil {
		m.Properties.Templates = sources
	}

	return g.marshalManifestFile(manifestPath, m)
}
