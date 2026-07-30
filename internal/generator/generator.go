package generator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"

	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

const (
	DefaultFileMode = 0o644
	DefaultDirMode  = 0o755
)

// CICD component pins for the scaffolded GitLab pipeline.
//
// These are kept in LOCKSTEP with the framework's own root .gitlab-ci.yml
// (resolved O2 of the 2026-06-15 generator-gitlab-ci-refresh spec): when the
// framework upgrades its phpboyscout/cicd component pins, bump these in the
// same change. The scaffolded renovate config (extending the cicd preset)
// then keeps the pins current for the downstream tool.
//
// The releaser-pleaser component is pinned separately because it lives under
// apricote/, not phpboyscout/cicd, and is referenced $CI_SERVER_FQDN-relative
// (instance-local) per O7.
const (
	// DefaultCICDComponentSource is the default include base for the
	// phpboyscout/cicd components. It is overridable via the manifest
	// `ci.component_source` value (O1) so a mirrored/self-hosted downstream
	// can repoint the include base; the default works out-of-the-box.
	DefaultCICDComponentSource = "gitlab.com/phpboyscout/cicd"

	// CICDComponentVersion is the phpboyscout/cicd component version the
	// scaffold pins (go-lint, go-test, go-security, goreleaser,
	// zensical-pages, renovate-self). Mirrors the framework's own pin; kept
	// current automatically by the Renovate customManager in renovate.json5
	// (do not hand-bump — let Renovate propose it).
	CICDComponentVersion = "v0.34.0"

	// ReleaserPleaserComponentVersion is the apricote/releaser-pleaser/run
	// component version the scaffold pins. The component does not support
	// floating tags, so this is always a full version (O7). Kept current by
	// the Renovate customManager in renovate.json5.
	ReleaserPleaserComponentVersion = "v0.9.0"
)

type Config struct {
	Agentless  bool
	AIModel    string
	AIProvider string
	// MaxSteps bounds the autonomous repair agent's ReAct loop. Zero uses the
	// framework default (gochat.DefaultMaxSteps). Raise it for complex commands
	// the agent cannot finish within the default budget.
	MaxSteps int
	// NonInteractive withholds the repair agent's query_user tool so a
	// generation never blocks for input (CI / --non-interactive).
	NonInteractive bool
	Aliases        []string
	Args           string
	DryRun         bool
	// GitInit controls the post-generation git step on `generate project`
	// (init + stage + initial commit). It is opt-out: the CLI sets it true by
	// default and false under --no-git. Only honoured by GenerateSkeleton.
	GitInit bool
	// GitPush, when true (--push), adds the derived remote as origin and pushes
	// the initial commit. Opt-in; requires GitInit. Push failures are non-fatal.
	GitPush bool
	// GitBranch is the default branch the initial commit lands on (default
	// "main", overridable via --git-branch).
	GitBranch        string
	Flags            []string
	Force            bool
	Hidden           bool
	Overwrite        string // allow, deny, or ask (default ask)
	Long             string
	Name             string
	Parent           string
	Path             string
	PersistentPreRun bool
	PreRun           bool
	Prompt           string
	Protected        *bool
	MCPEnabled       *bool // tri-state MCP exposure; mirrors Protected
	ScriptPath       string
	Short            string
	UpdateDocs       bool
	// PublicAPI marks the module as publicly published (the --public-api flag),
	// so generated package docs defer their API reference to pkg.go.dev. Default
	// false: the reference is stubbed to a local `go doc` hint (no dead links for
	// a private/unpublished module). The manifest module_published property is the
	// persistent equivalent.
	PublicAPI bool
	// NoAIAttribution, when true (the --no-ai-attribution flag on `generate
	// docs`), flips the documentation system prompt so the frontmatter `authors:`
	// field carries the project's human author(s) only — the model is instructed
	// to add no AI/model/assistant identity. Default false: AI attribution is
	// additive (existing human authors preserved, AI model appended as a
	// co-author). See docs.go authorsDirectives and issue #7.
	NoAIAttribution      bool
	WithAssets           bool
	WithConfigValidation bool
	WithInitializer      bool
}

type CommandFlag struct {
	Default       string
	DefaultIsCode bool
	Description   string
	Hidden        bool
	Name          string
	Persistent    bool
	Required      bool
	Shorthand     string
	Type          string
}

type Generator struct {
	props      *props.Props
	config     *Config
	chatClient gochat.ChatClient
	runCommand func(ctx context.Context, dir, name string, args ...string) ([]byte, error)
	// cloneTemplate fetches a git custom-template source into a staging dir
	// and returns the resolved commit SHA. nil means "no clone available"
	// (offline): a cold cache for a git source then errors clearly rather
	// than silently restoring a suppressed embedded scaffold. Tests inject a
	// local-remote/fake; production wires the real provider-aware clone via
	// WithTemplateClone / EnableRealTemplateClone.
	cloneTemplate templateCloneFunc
}

// WithTemplateClone injects the git template-source clone implementation.
// Primarily for tests (a local bare remote or a fake); production code calls
// EnableRealTemplateClone to wire the provider-aware pkg/vcs/repo clone.
func (g *Generator) WithTemplateClone(fn templateCloneFunc) *Generator {
	g.cloneTemplate = fn

	return g
}

func New(p *props.Props, cfg *Config) *Generator {
	return &Generator{
		props:  p,
		config: cfg,
	}
}

func (g *Generator) SetProtection(ctx context.Context, commandName string, protected bool) error {
	pathParts := strings.Split(commandName, "/")
	name := pathParts[len(pathParts)-1]

	// Create a minimal config for finding/updating the command
	g.config.Name = name
	g.config.Protected = &protected

	if len(pathParts) > 1 {
		g.config.Parent = pathParts[len(pathParts)-2]
	}

	// Protection is a manifest-only flag, so this is a targeted find-and-set
	// against the manifest rather than a full updateManifest (which would rewrite
	// description, flags and hashes from g.config defaults).
	return g.setCommandProtection(pathParts, protected)
}

// SetMCPEnabled records a command's MCP-exposure decision in the manifest and
// re-renders that single command's cmd.go so the generated
// setup.ExcludeFromMCP / setup.IncludeInMCP marker matches. It is the engine
// behind `gtb enable mcp` (enabled=true) and `gtb disable mcp` (enabled=false).
//
// Unlike SetProtection (manifest-only), this changes generated code, so it
// drives the targeted RegenerateCommand path. A protected command is refused
// with ErrCommandProtected rather than silently overwritten — the operator must
// unprotect it first.
func (g *Generator) SetMCPEnabled(ctx context.Context, commandPath string, enabled bool) error {
	pathParts := strings.Split(commandPath, "/")
	name := pathParts[len(pathParts)-1]
	parentPath := pathParts[:len(pathParts)-1]

	manifestPath := ManifestPathFor(g.config.Path)

	m, err := g.decodeManifestFile(manifestPath)
	if err != nil {
		return err
	}

	cmd := findCommandAt(m.Commands, parentPath, name)
	if cmd == nil {
		return errors.Newf("command %s not found in manifest", commandPath)
	}

	// Honour protection: never rewrite a protected command's cmd.go here.
	if cmd.Protected != nil && *cmd.Protected {
		return ErrCommandProtected
	}

	cmd.MCPEnabled = &enabled

	// Persist the manifest up front, fatally: the regeneration below also
	// re-writes the manifest (via the pipeline's updateManifest), but that
	// write is advisory. Writing here first guarantees the decision is durable
	// before we re-render cmd.go, so a write failure can't leave the generated
	// code and the manifest disagreeing.
	if err := g.marshalManifestFile(manifestPath, m); err != nil {
		return err
	}

	// Re-render the single command so the marker reflects the new decision.
	return g.RegenerateCommand(ctx, *cmd, parentPath)
}

func PascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}

	return strings.Join(parts, "")
}

func (g *Generator) verifyProject() error {
	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")

	if _, err := g.props.FS.Stat(manifestPath); os.IsNotExist(err) {
		return errors.Wrapf(ErrNotGoToolBaseProject, "at %q; run 'generate skeleton' first", g.config.Path)
	}

	// The manifest file is present (Stat above), so a load failure here is a
	// corrupt/undecodable manifest — surface it rather than silently passing
	// verification and skipping the version-compatibility gate.
	m, err := g.loadManifest()
	if err != nil {
		return err
	}

	return g.checkManifestVersion(m)
}

func (g *Generator) checkManifestVersion(m *Manifest) error {
	cliVer := ""
	if g.props.Version != nil {
		cliVer = g.props.Version.GetVersion()
	}

	// Dev builds bypass checks
	if g.props.Version == nil || g.props.Version.IsDevelopment() || cliVer == "" {
		return nil
	}

	manifestVer := m.Version.GoToolBase

	// A dev-build (or version-less) manifest is refused by a released CLI with a
	// message naming the real condition — "generated by a dev build" — rather
	// than the misleading "your gtb is older than the manifest" wording, which
	// sent operators to upgrade a CLI that is already current.
	if manifestVer == "dev" {
		return errors.Newf("this project's manifest was generated by a development build of gtb; regenerate it with a released gtb (current: %s) before continuing", cliVer)
	}

	if manifestVer == "" {
		return errors.Newf("this project's manifest does not record the gtb version it was generated with; regenerate it with a released gtb (current: %s) before continuing", cliVer)
	}

	if version.CompareVersions(cliVer, manifestVer) < 0 {
		return errors.Newf("current gtb version (%s) is lower than the version specified in the manifest (%s). Please update gtb: go install gitlab.com/phpboyscout/go-tool-base@latest", cliVer, manifestVer)
	}

	if version.CompareVersions(cliVer, manifestVer) > 0 {
		g.checkBreakingChanges(manifestVer, cliVer)
	}

	return nil
}

func (g *Generator) checkBreakingChanges(manifestVer, cliVer string) {
	for ver, msg := range BreakingChanges {
		// If version is newer than manifest AND older than or equal to current CLI
		// using > and <= comparisons
		if version.CompareVersions(ver, manifestVer) > 0 && version.CompareVersions(ver, cliVer) <= 0 {
			g.props.Logger.Warn("project update required", "version", ver, "message", msg)
		}
	}
}

func (g *Generator) getImportPath() (string, error) {
	moduleName, err := g.getModuleName()
	if err != nil {
		return "", err
	}

	cmdSubPath, err := g.getCommandPath()
	if err != nil {
		return "", err
	}

	relCmdPath, err := filepath.Rel(g.config.Path, cmdSubPath)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s/%s", moduleName, relCmdPath), nil
}
