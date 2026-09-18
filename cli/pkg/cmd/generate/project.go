package generate

import (
	"context"
	"fmt"
	"slices"
	"strings"

	gochat "gitlab.com/phpboyscout/go/chat"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/signing"

	icmd "gitlab.com/phpboyscout/go-tool-base/cli/pkg/cmd"
	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

type SkeletonOptions struct {
	// shared carries `generate`'s persistent flags, injected by the
	// constructor rather than read from package state.
	shared *SharedFlags

	Name         string
	ForgeBackend string
	Repo         string
	Host         string
	Private      bool
	Description  string
	Path         string
	GoVersion    string
	Features     []string
	HelpType     string
	Overwrite    string
	SlackChannel string
	SlackTeam    string
	TeamsChannel string
	TeamsTeam    string
	EnvPrefix    string

	// UpdatePolicy is the generated tool's self-update posture baseline
	// (disabled / prompt / enabled). Empty leaves it unset so the framework
	// default (disabled) applies.
	UpdatePolicy string
	// MCPMode is the MCP publication mode (spec 0201 D3).
	MCPMode string
	// MCPExposed are the command paths ticked on the revisit wizard's surface
	// page; mcpCommands is what the page offers, loaded from the manifest
	// (spec 0202 D9).
	MCPExposed  []string
	mcpCommands []mcpCommandChoice

	// UpdateCheckInterval is the generated tool's baseline self-update-check
	// throttle as a Go duration string (e.g. "24h"). Empty leaves it unset so
	// the framework default (24h) applies.
	UpdateCheckInterval string

	// NoForge marks a project that is not hosted on a forge (spec 0195 D3):
	// no backend, no repository, and Module names the Go module path.
	NoForge bool
	// hosted is the wizard's confirm for NoForge, bound positively so the
	// question reads "hosted on a forge?" and defaults to yes.
	hosted bool
	// Module is the Go module path; required with NoForge, an override of
	// <host>/<org>/<repo> otherwise (spec 0195 D5, OQ6).
	Module string
	// ForgeCredentials are further forges enabled for their credential
	// wizard and adapter only (spec 0195 D6).
	ForgeCredentials []string
	// ReleaseChannel is forge or direct when update is enabled (spec 0195
	// D7); empty resolves to forge for a hosted project.
	ReleaseChannel string
	// Direct carries the direct source's settings for ReleaseChannel direct.
	Direct generator.ManifestDirectSource

	// CIComponentSource overrides the phpboyscout/cicd include base in the
	// scaffolded GitLab pipeline (GitLab backend only). Empty uses the
	// framework default, gitlab.com/phpboyscout/cicd.
	CIComponentSource string

	// Git post-generation step. NoGit opts out of the default init + initial
	// commit; Push (opt-in) additionally adds the derived remote and pushes;
	// GitBranch overrides the default branch the initial commit lands on.
	NoGit     bool
	Push      bool
	GitBranch string

	// Signing (off by default). When Signing is true the generated tool
	// scaffolds internal/trustkeys and wires props.Signing. require_signature
	// is intentionally not collectable here — it stays false until a signed
	// release has shipped and is only ever flipped via `gtb enable signing`.
	Signing                          bool
	SigningEmail                     string
	SigningKeySource                 string
	SigningRequireExternalCrosscheck bool
	// Release-pipeline fields. Recording a key id wires the generated
	// GoReleaser signs block; backend/region/public-key default in the
	// generator when a key id is set.
	SigningBackend   string
	SigningKeyID     string
	SigningKMSRegion string
	SigningPublicKey string

	// ChatProviders is what cmd/<name>/chat.go links, as chat.Provider names.
	// Defaults to every provider the framework can configure; an empty list
	// with the ai feature selected is refused (spec 0194 D5, OQ4, OQ5).
	ChatProviders []string

	// TelemetryEndpoint and TelemetryOTelEndpoint are where the telemetry
	// feature sends; recorded under properties.telemetry (spec 0197 D4).
	TelemetryEndpoint     string
	TelemetryOTelEndpoint string
	// Bootstrap is the config-bootstrap posture (auto-initialise, the commands
	// that skip the config check, the auxiliary fast-path commands).
	Bootstrap generator.ManifestBootstrap
	// ConfigLayers declares which config-stack layers the tool wires; empty
	// inherits the framework default.
	ConfigLayers []string
	// SigningRequireSignature and SigningRequireChecksum are the enforcement
	// baselines. Only the checksum one is asked on a first run; the signature
	// one is a footgun before a signed release has shipped (0071), so the
	// wizard asks it only on a revisit (spec 0197 D5).
	SigningRequireSignature bool
	SigningRequireChecksum  bool

	// NoVerify skips go mod tidy and golangci-lint after generation; the run
	// then exits 0 having emitted files it did not verify (spec 0197 D10).
	NoVerify bool

	// revisit marks a wizard run over an existing project (gtb wizard, spec
	// 0197 D13), which asks what a first run holds back.
	revisit bool

	// ChatDefault is the author's default provider, model and addressing,
	// recorded in the manifest and shipped as the tool's lowest config layer.
	// Required when several providers are linked (spec 0196 D1, D9).
	ChatDefault generator.ManifestChatDefault

	// Templates carries the custom template-overlay specs (<src>@<ref>)
	// supplied via --template (repeatable). Each is parsed into a manifest
	// TemplateSource and layered over the embedded skeleton.
	Templates []string
}

func NewCmdSkeleton(p *props.Props, shared *SharedFlags) *cobra.Command {
	opts := SkeletonOptions{
		shared:       shared,
		ForgeBackend: "github",
		HelpType:     "none",
	}

	cmd := &cobra.Command{
		Use:     "project",
		Aliases: []string{"cli", "skeleton"},
		Short:   "Generate a new project skeleton",
		Long: `Scaffold a complete new GTB-based CLI project.

Generates the full module layout: go.mod, the root command and main entry
point, the .gtb manifest, embedded default config and assets, the feature
commands selected by --features (see that flag for the full set), and the CI
pipeline for the chosen Git backend (GitHub or GitLab). Optional release-signing
and custom template overlays can be layered in. By default the new project is
git-initialised with an initial commit; pass --no-git to skip that or --push to
also add the remote and push.

Run without --name/--repo in an interactive terminal to launch a guided wizard;
otherwise supply the flags directly.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.ValidateOrPrompt(p); err != nil {
				return err
			}

			return opts.Run(cmd.Context(), p)
		},
	}

	cmd.Flags().StringVarP(&opts.Name, "name", "n", "", "Project name (e.g. als)")
	cmd.Flags().StringVarP(&opts.Repo, "repo", "r", "", "Repository in org/repo format")
	cmd.Flags().StringVar(&opts.ForgeBackend, "forge-backend", defaultGitBackend,
		"Forge the project is hosted on ("+strings.Join(forgeBackendNames(), ", ")+")")
	cmd.Flags().BoolVar(&opts.NoForge, "no-forge", false, "The project is not hosted on a forge; requires --module")
	cmd.Flags().StringVar(&opts.Module, "module", "", "Go module path (required with --no-forge; overrides <host>/<org>/<repo> otherwise)")
	cmd.Flags().StringSliceVar(&opts.ForgeCredentials, "forge-credentials", nil,
		"Further forges to enable for credential capture (their init wizard and adapter), not the release source")
	cmd.Flags().StringVar(&opts.ReleaseChannel, "release-channel", "", "Release channel for self-update: forge (default when hosted) or direct")
	cmd.Flags().StringVar(&opts.Direct.URLTemplate, "release-url-template", "", "direct channel: asset URL template")
	cmd.Flags().StringVar(&opts.Direct.ChecksumURLTemplate, "release-checksum-url-template", "", "direct channel: checksum URL template")
	cmd.Flags().StringVar(&opts.Direct.SignatureURLTemplate, "release-signature-url-template", "", "direct channel: signature URL template")
	cmd.Flags().StringVar(&opts.Direct.VersionURL, "release-version-url", "", "direct channel: URL that reports the latest version")
	cmd.Flags().StringVar(&opts.Direct.VersionFormat, "release-version-format", "", "direct channel: version endpoint format (text, json, yaml, xml)")
	cmd.Flags().StringVar(&opts.Direct.VersionKey, "release-version-key", "", "direct channel: key holding the version in a structured endpoint")
	cmd.Flags().StringVar(&opts.Direct.PinnedVersion, "release-pinned-version", "", "direct channel: pin to one version")
	cmd.Flags().StringVar(&opts.Host, "host", "", "Git host (defaults to backend's canonical host)")
	cmd.Flags().BoolVar(&opts.Private, "private", false, "Mark the repository as private (requires a token for updates)")
	cmd.Flags().StringVarP(&opts.Description, "description", "d", "A tool built with gtb", "Project description")
	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Destination path")
	// Clone the default: the flag value is handed to pflag, and every command
	// instance must get its own backing array rather than sharing the package
	// var's.
	cmd.Flags().StringSliceVarP(&opts.Features, "features", "f", slices.Clone(generator.DefaultSelectedFeatures),
		"Features to enable ("+strings.Join(generator.SelectableFeatures, ", ")+")")
	cmd.Flags().StringSliceVar(&opts.ChatProviders, "chat-providers", generator.DefaultChatProviders(),
		"Chat providers the tool links when the ai feature is enabled ("+strings.Join(generator.DefaultChatProviders(), ", ")+")")
	cmd.Flags().StringVar(&opts.ChatDefault.Provider, "chat-default-provider", "",
		"Default chat provider; required when --chat-providers links more than one")
	cmd.Flags().StringVar(&opts.ChatDefault.Model, "chat-default-model", "", "Default model for the default provider (blank: the module's choice)")
	cmd.Flags().StringVar(&opts.ChatDefault.BaseURL, "chat-base-url", "", "API endpoint; required by openai-compatible and azure-openai")
	cmd.Flags().StringVar(&opts.ChatDefault.APIVersion, "chat-api-version", "", "Dated API version; required by azure-openai")
	cmd.Flags().StringVar(&opts.ChatDefault.Project, "chat-project", "", "Cloud project, for gemini-vertex")
	cmd.Flags().StringVar(&opts.ChatDefault.Location, "chat-location", "", "Region, for gemini-vertex and bedrock")
	cmd.Flags().StringVar(&opts.GoVersion, "go-version", "", "Go version for go.mod (defaults to the running toolchain version)")
	cmd.Flags().StringVar(&opts.TelemetryEndpoint, "telemetry-endpoint", "", "Where the telemetry feature sends usage events (HTTPS)")
	cmd.Flags().StringVar(&opts.TelemetryOTelEndpoint, "telemetry-otel-endpoint", "", "OpenTelemetry collector endpoint for the telemetry feature")
	cmd.Flags().BoolVar(&opts.Bootstrap.AutoInitialise, "auto-initialise", false, "Run the first-run bootstrap automatically when the config is missing")
	cmd.Flags().StringSliceVar(&opts.Bootstrap.SkipConfigCheck, "skip-config-check", nil, "Commands that run without a config file (repeatable)")
	cmd.Flags().StringSliceVar(&opts.Bootstrap.AuxiliaryCommands, "auxiliary-commands", nil, "Commands that take the root pre-run's auxiliary fast path (repeatable)")
	cmd.Flags().StringSliceVar(&opts.ConfigLayers, "config-layers", nil, "Config-stack layers the tool wires, in precedence order (default: the framework's)")
	cmd.Flags().StringVar(&opts.HelpType, "help-type", "none", "Help channel type (slack, teams, or none)")
	cmd.Flags().StringVar(&opts.Overwrite, "overwrite", "ask", "How to handle file conflicts: allow, deny, or ask")
	cmd.Flags().BoolVar(&opts.NoVerify, "no-verify", false, "Skip go mod tidy and golangci-lint after generation (the run exits 0 unverified; without it a failed step exits 3)")
	cmd.Flags().StringVar(&opts.SlackChannel, "slack-channel", "", "Slack channel for help (e.g. #my-team-help)")
	cmd.Flags().StringVar(&opts.SlackTeam, "slack-team", "", "Slack team name (e.g. My Team)")
	cmd.Flags().StringVar(&opts.TeamsChannel, "teams-channel", "", "Microsoft Teams channel for help")
	cmd.Flags().StringVar(&opts.TeamsTeam, "teams-team", "", "Microsoft Teams team name")
	cmd.Flags().StringVar(&opts.EnvPrefix, "env-prefix", "", "Environment variable prefix for config overrides (e.g. MY_APP)")
	cmd.Flags().StringVar(&opts.UpdatePolicy, "update-policy", "", "Self-update posture for the generated tool: disabled, prompt, or enabled (empty = framework default disabled)")
	cmd.Flags().StringVar(&opts.MCPMode, "mcp-mode", "", "MCP publication mode: compact (three discovery tools, the default) or direct (one tool per command)")
	cmd.Flags().StringVar(&opts.UpdateCheckInterval, "update-check-interval", "", "Baseline interval between self-update checks as a Go duration, e.g. 24h or 168h (empty = framework default 24h)")
	cmd.Flags().StringVar(&opts.CIComponentSource, "ci-component-source", "", "Override the phpboyscout/cicd component include base in the scaffolded GitLab pipeline (default gitlab.com/phpboyscout/cicd)")
	cmd.Flags().BoolVar(&opts.Signing, "signing", false, "Enable consumer-side release-signing verification (scaffolds internal/trustkeys and wires props.Signing)")
	cmd.Flags().StringVar(&opts.SigningEmail, "signing-email", "", "Release WKD email for signing (external_key_email); implies --signing")
	cmd.Flags().StringVar(&opts.SigningKeySource, "signing-key-source", "both", "Signing trust-anchor source: embedded, external, or both")
	cmd.Flags().BoolVar(&opts.SigningRequireExternalCrosscheck, "signing-require-external-crosscheck", false, "Fail signing closed when the external (WKD) resolver is unreachable")
	cmd.Flags().BoolVar(&opts.SigningRequireSignature, "signing-require-signature", false, "Fail a self-update closed without a valid signature; not before your first signed release")
	cmd.Flags().BoolVar(&opts.SigningRequireChecksum, "signing-require-checksum", false, "Fail a self-update closed without a verified checksum")
	cmd.Flags().StringVar(&opts.SigningKeyID, "signing-key-id", "", "Signing key id/ARN/alias (or PEM path for local) the release pipeline signs with; wires the GoReleaser signs block")
	cmd.Flags().StringVar(&opts.SigningBackend, "signing-backend", "", "gtb sign backend for the release pipeline (default aws-kms when --signing-key-id is set)")
	cmd.Flags().StringVar(&opts.SigningKMSRegion, "signing-kms-region", "", "AWS region for the aws-kms backend (default eu-west-2)")
	cmd.Flags().StringVar(&opts.SigningPublicKey, "signing-public-key", "", "Path to the embedded public key the signature identifies (default internal/trustkeys/keys/signing-key-v1.asc)")
	cmd.Flags().BoolVar(&opts.NoGit, "no-git", false, "Skip the post-generation git init and initial commit (init+commit is on by default)")
	cmd.Flags().BoolVar(&opts.Push, "push", false, "After the initial commit, add the derived remote as origin and push the default branch (push failures are non-fatal)")
	cmd.Flags().StringVar(&opts.GitBranch, "git-branch", "main", "Default branch the initial commit lands on")
	cmd.Flags().StringArrayVar(&opts.Templates, "template", nil, "Custom template overlay source <src>@<ref> (local path or forge repo); repeatable, layered in order")

	return cmd
}

func (o *SkeletonOptions) ValidateOrPrompt(p *props.Props) error {
	if o.Name == "" || (o.Repo == "" && !o.NoForge) {
		if !p.GetIO().Interactive() {
			return ErrNonInteractive
		}

		if err := o.runWizard(); err != nil {
			return err
		}
	}

	// Strip a leading host segment from --repo (e.g. "github.com/acme/tool")
	// before validation and generation, so the manifest stores a host-less
	// org/repo and the module path is not double-prefixed. A GitLab nested
	// group ("group/sub/tool") has no host-like first segment and is left
	// intact. Runs after both the wizard and flag paths.
	o.Repo, o.Host = normalizeRepoHost(o.Repo, o.Host)

	return o.validateFields()
}

// validateFields applies the structural validation rules from
// internal/generator/validate.go to every user-influenced field.
// Runs after both wizard and flag-driven flows so neither path
// can smuggle adversarial input into template rendering.
// See https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0055-generator-template-escaping
// for the full threat model.
func (o *SkeletonOptions) validateFields() error {
	if err := o.validateCoreFields(); err != nil {
		return err
	}

	if err := o.validateHelpFields(); err != nil {
		return err
	}

	if err := o.validateSigningFields(); err != nil {
		return err
	}

	features := o.resolveFeatures()
	if err := generator.ValidateChatProviders(o.ChatProviders, features); err != nil {
		return err
	}

	return generator.ValidateChatDefault(o.ChatDefault, o.ChatProviders, features)
}

// validatePostureFields checks the operational settings that have flags and
// a manifest home but no wizard page (spec 0197 D4), with the validators
// ValidateManifest applies.
func (o *SkeletonOptions) validatePostureFields() error {
	if err := generator.ValidateConfigLayers(o.ConfigLayers); err != nil {
		return err
	}

	for _, endpoint := range []string{o.TelemetryEndpoint, o.TelemetryOTelEndpoint} {
		if err := generator.ValidateTelemetryEndpoint(endpoint); err != nil {
			return err
		}
	}

	return nil
}

// validateHostingFields checks the forge half (backend, repository, host, org)
// for a hosted project, or the module path for one that is not (spec 0195
// D3, D4, D5).
func (o *SkeletonOptions) validateHostingFields() error {
	if o.NoForge {
		if o.Module == "" {
			return errors.WithStack(ErrModuleRequired)
		}

		return generator.ValidateModulePath(o.Module)
	}

	if err := o.validateForgeSelection(); err != nil {
		return err
	}

	if err := generator.ValidateRepo(o.Repo); err != nil {
		return err
	}

	if o.Host != "" {
		if err := generator.ValidateHost(o.Host); err != nil {
			return err
		}
	}

	// Derive org from repo for validation so a bad org fails early
	// rather than at CODEOWNERS render time.
	if org, err := splitRepoOrgForValidate(o.Repo); err == nil {
		if verr := generator.ValidateOrg(org, o.ForgeBackend); verr != nil {
			return verr
		}
	}

	return nil
}

// validateForgeSelection checks the backend, the credential forges and an
// optional module-path override.
func (o *SkeletonOptions) validateForgeSelection() error {
	if err := generator.ValidateForgeBackend(o.ForgeBackend); err != nil {
		return err
	}

	for _, extra := range o.ForgeCredentials {
		if err := generator.ValidateForgeBackend(extra); err != nil {
			return err
		}
	}

	return generator.ValidateModulePath(o.Module)
}

// validateReleaseChannel enforces spec 0195 D7: a self-updating tool has a
// release channel, and the direct channel has the URLs the source needs.
func (o *SkeletonOptions) validateReleaseChannel() error {
	if err := generator.ValidateReleaseChannel(o.ReleaseChannel); err != nil {
		return err
	}

	if !slices.Contains(o.Features, string(props.UpdateCmd)) {
		return nil
	}

	switch o.resolvedReleaseChannel() {
	case generator.ReleaseChannelDirect:
		if o.Direct.URLTemplate == "" || o.Direct.VersionURL == "" {
			return errors.WithStack(ErrDirectSourceIncomplete)
		}

		return nil
	case generator.ReleaseChannelForge:
		return nil
	default:
		return errors.WithStack(ErrReleaseChannelRequired)
	}
}

// resolvedReleaseChannel is the channel a hosted project defaults to.
func (o *SkeletonOptions) resolvedReleaseChannel() string {
	if o.ReleaseChannel == "" && !o.NoForge {
		return generator.ReleaseChannelForge
	}

	return o.ReleaseChannel
}

// validateSigningFields checks the signing key-source value when signing
// is requested (explicitly or implied by a signing email).
func (o *SkeletonOptions) validateSigningFields() error {
	if !o.Signing && o.SigningEmail == "" {
		// A key id promises the release pipeline's signs block, which only
		// the signing path renders; refuse rather than drop it (#49).
		if o.SigningKeyID != "" {
			return errors.WithStack(ErrSigningKeyWithoutSigning)
		}

		return nil
	}

	if err := generator.ValidateSigningKeySource(o.SigningKeySource); err != nil {
		return errors.Wrapf(ErrInvalidSigningKeySource, "%q", o.SigningKeySource)
	}

	if o.SigningBackend != "" && !slices.Contains(signing.Names(), o.SigningBackend) {
		return errors.Wrapf(ErrInvalidSigningBackend, "%q (available: %s)", o.SigningBackend, strings.Join(signing.Names(), ", "))
	}

	// The remaining release-pipeline fields are rendered into the
	// CI-executed .goreleaser.yaml, so they are validated here at the CLI
	// boundary (the manifest path re-validates via ValidateManifest).
	if err := generator.ValidateSigningKMSRegion(o.SigningKMSRegion); err != nil {
		return err
	}

	if err := generator.ValidateSigningKeyID(o.SigningKeyID); err != nil {
		return err
	}

	return generator.ValidateSigningPublicKey(o.SigningPublicKey)
}

// validateCoreFields groups the core identity checks (name, repo,
// host, description, env prefix, and derived org) so validateFields
// stays under the cyclomatic-complexity budget.
func (o *SkeletonOptions) validateCoreFields() error {
	if err := generator.ValidateName(o.Name); err != nil {
		return err
	}

	// Reject unknown --features names before anything is cloned or written.
	// Previously an unrecognised name was copied verbatim into the manifest and
	// then silently dropped at emission, so `--features bogus` exited 0 having
	// produced a tool that lacked the feature and recorded that it had it. A
	// forge name is refused with the flag that chooses one (spec 0195 D1).
	for _, f := range o.Features {
		if err := generator.ValidateSelectableFeatureName(f); err != nil {
			return err
		}
	}

	if err := generator.ValidateDescription(o.Description); err != nil {
		return err
	}

	if err := o.validateHostingFields(); err != nil {
		return err
	}

	if err := o.validateReleaseChannel(); err != nil {
		return err
	}

	if err := o.validateUpdateFields(); err != nil {
		return err
	}

	if err := o.validatePostureFields(); err != nil {
		return err
	}

	return generator.ValidateCIComponentSource(o.CIComponentSource)
}

// validateUpdateFields groups the env-prefix and self-update posture checks,
// keeping validateCoreFields under the cyclomatic-complexity budget.
func (o *SkeletonOptions) validateUpdateFields() error {
	if err := generator.ValidateEnvPrefix(o.EnvPrefix); err != nil {
		return err
	}

	if err := generator.ValidateUpdatePolicy(o.UpdatePolicy); err != nil {
		return err
	}

	if err := generator.ValidateMCPMode(o.MCPMode); err != nil {
		return err
	}

	return generator.ValidateUpdateCheckInterval(o.UpdateCheckInterval)
}

// validateHelpFields groups the Slack/Teams help-channel checks.
func (o *SkeletonOptions) validateHelpFields() error {
	helpType := o.HelpType
	if helpType == "none" {
		helpType = ""
	}

	if err := generator.ValidateHelpType(helpType); err != nil {
		return err
	}

	// The type is a closed set and each member needs its channel: a Slack
	// help block with no channel renders an empty SupportMessage (#49).
	switch helpType {
	case "slack":
		if o.SlackChannel == "" {
			return errors.WithStack(ErrHelpChannelRequired)
		}
	case "teams":
		if o.TeamsChannel == "" {
			return errors.WithStack(ErrHelpChannelRequired)
		}
	}

	if err := generator.ValidateSlackChannel(o.SlackChannel); err != nil {
		return err
	}

	if err := generator.ValidateSlackTeam(o.SlackTeam); err != nil {
		return err
	}

	if err := generator.ValidateTeamsChannel(o.TeamsChannel); err != nil {
		return err
	}

	return generator.ValidateTeamsTeam(o.TeamsTeam)
}

// splitRepoOrgForValidate extracts the namespace portion of a repo
// path for org validation. Two shapes are supported:
//
//   - host/org/name (3+ segments): the first segment is the host and
//     the last is the repo name; everything between is the
//     org/namespace (e.g. "github.com/myorg/mytool" → "myorg";
//     "gitlab.com/group/sub/mytool" → "group/sub").
//   - org/name (exactly 2 segments, no host prefix): the first segment
//     is the org and the second is the repo name (e.g.
//     "myorg/mytool" → "myorg"). Previously this shape was rejected,
//     which silently skipped ValidateOrg for host-less repos and let a
//     malformed org through to CODEOWNERS render time.
//
// The org returned is validated by [generator.ValidateOrg].
//
// This is distinct from the older splitRepoPath helper in
// internal/generator/skeleton.go which splits on the LAST `/` and
// therefore returns the entire `host/group/subgroup` prefix as
// "org". We avoid that shape because it cannot appear in a real
// GitHub or GitLab mention.
func splitRepoOrgForValidate(repo string) (org string, err error) {
	const minRepoSegments = 2

	// Drop a leading host segment (dot-detected) so a host-qualified repo and
	// a bare org/repo yield the same org, and a GitLab nested group is kept
	// whole.
	repo, _ = normalizeRepoHost(repo, "")

	segments := strings.Split(repo, "/")
	if len(segments) < minRepoSegments {
		return "", errors.Newf("repo %q must be org/name or host/org/name", repo)
	}

	// Everything before the last segment is the org/namespace (the host, if
	// any, was already stripped above).
	return strings.Join(segments[:len(segments)-1], "/"), nil
}

// normalizeRepoHost strips a leading host segment from a host-qualified repo
// path so the stored org/repo and the generated module path never include the
// host: "github.com/acme/tool" → repo "acme/tool", host "github.com". The
// first segment is treated as a host only when it looks like one (contains a
// "."), which preserves GitLab nested groups ("group/sub/tool"). A stripped
// host is adopted only when no host was supplied explicitly, so an explicit
// --host always wins.
//
// Normalising here keeps the two repo splitters (this one and the generator's
// splitRepoPath) and the module-path builder consistent: a host-qualified
// --repo no longer stores an org with the host baked in, which previously
// produced a manifest that `regenerate project` rejected.
func normalizeRepoHost(repo, host string) (string, string) {
	if first, rest, found := strings.Cut(repo, "/"); found && strings.Contains(first, ".") {
		if host == "" {
			host = first
		}

		return rest, host
	}

	return repo, host
}

// defaultGitBackend is the backend chosen when none is given. It is also the
// fallback when a lookup misses, which keeps every accessor below total.
const defaultGitBackend = "github"

// backendDisplay resolves a git backend value to its forge display data,
// falling back to the default backend so the wizard never renders a blank
// label or placeholder for a value that failed validation elsewhere.
//
// Previously each of these accessors was its own `if backend == "gitlab"`
// branch. Four forges would have meant eight branches — and it was exactly
// that duplication that let the wizard offer GitLab while the flag help,
// validation and credential wizard disagreed about what existed.
func backendDisplay(backend string) forge.Display {
	// Every forge the generator can host on resolves to itself (spec 0195 D4);
	// whether it has a CI skeleton is the generator's concern, not the
	// chooser's (D8).
	if id := props.FeatureID(backend); slices.Contains(generator.ForgeBackends(), id) {
		if d, ok := forge.DisplayFor(id); ok {
			return d
		}
	}

	d, _ := forge.DisplayFor(props.FeatureID(defaultGitBackend))

	return d
}

// backendLabel is the human-facing name for a git backend value.
func backendLabel(backend string) string { return backendDisplay(backend).Label }

// hostForBackend is the default host for a git backend value.
func hostForBackend(backend string) string { return backendDisplay(backend).Host }

// resolvedHost is the host the project is generated against: the one the user
// gave, or the backend's canonical host when they left it empty.
func (o *SkeletonOptions) resolvedHost() string {
	if o.Host != "" {
		return o.Host
	}

	return hostForBackend(o.ForgeBackend)
}

// repoDescription is the repository-field help text for a git backend.
func repoDescription(backend string) string { return backendDisplay(backend).RepoDescription }

// repoPlaceholder is the repository-field placeholder for a git backend.
func repoPlaceholder(backend string) string { return backendDisplay(backend).RepoPlaceholder }

// forgeBackendOptions is the wizard's backend chooser: every forge the
// generator can host a project on (spec 0195 D4), so the options, the flag
// help and the credential wizards cannot drift apart the way they had.
// featureLabels are the human-facing names for the features the wizard offers.
// Forge labels are not listed: they come from the forge registry via
// forge.DisplayFor, so a new forge needs no entry here.
var featureLabels = map[string]string{
	"init":      "Initialization",
	"update":    "Self-Update",
	"mcp":       "MCP Server",
	"docs":      "Documentation",
	"doctor":    "Doctor",
	"changelog": "Changelog",
	"ai":        "AI Chat",
	"config":    "Config Management",
	"telemetry": "Telemetry",
	"man":       "Man Pages",

	generator.KeychainFeature: "OS Keychain",
}

// featureGlosses are the one-line explanations shown beside each feature in
// the wizard. huh has no per-option description, so the gloss rides in the
// option label as a second column (optionLabel). Forges get a generated gloss.
var featureGlosses = map[string]string{
	"init":      "first-run setup wizard: config file, credentials, SSH keys",
	"update":    "self-update from the release source, with signature checks",
	"mcp":       "serve the tool's commands to AI agents over MCP",
	"docs":      "built-in documentation browser and `docs ask`",
	"doctor":    "environment and configuration health checks",
	"changelog": "changelog from conventional commits",
	"ai":        "AI chat client; the providers to link are chosen next",
	"config":    "config get/set/list commands",
	"telemetry": "opt-in usage telemetry and OpenTelemetry export",
	"man":       "man page generation",

	generator.KeychainFeature: "store credentials in the OS keychain (go-keyring)",
}

// providerGlosses explain each chat provider the wizard offers, in the same
// second-column form as the features.
var providerGlosses = map[string]string{
	"claude":            "Anthropic API; needs an API key",
	"claude-local":      "the claude CLI on this machine; no API key",
	"openai":            "OpenAI API; needs an API key",
	"openai-compatible": "any OpenAI-shaped endpoint (Ollama, xAI); needs a base URL",
	"codex-local":       "the codex CLI on this machine; no API key",
	"gemini":            "Google Gemini API; needs an API key",
	"gemini-vertex":     "Gemini through Vertex AI; Google application default credentials",
	"agy-local":         "the agy CLI on this machine; no API key, no tools",
	"bedrock":           "AWS Bedrock; the AWS credential chain, links the AWS SDK",
	"azure-openai":      "Azure OpenAI; a deployment endpoint and api-key or Entra token",
}

// newMultiSelect builds a multi-select whose viewport shows every option on
// first paint. huh sizes an auto-height multi-select to its options and then
// subtracts the title and description lines (field_multiselect.go,
// updateViewportSize), so a list built without an explicit Height clips its
// last options: two of five providers, then the sixteenth feature (#43).
func newMultiSelect(title, description string, options []huh.Option[string]) *huh.MultiSelect[string] {
	header := 0
	if title != "" {
		header += strings.Count(title, "\n") + 1
	}

	if description != "" {
		header += strings.Count(description, "\n") + 1
	}

	return huh.NewMultiSelect[string]().
		Title(title).
		Description(description).
		Options(options...).
		Height(len(options) + header)
}

// optionLabel lays a label and its gloss out as two columns, padding the label
// to width so the glosses line up down the list.
func optionLabel(label, gloss string, width int) string {
	if gloss == "" {
		return label
	}

	return fmt.Sprintf("%-*s  %s", width, label, gloss)
}

// featureGloss resolves a feature's explanation: the static table for the
// built-ins, a generated line for a forge.
func featureGloss(name string) string {
	if gloss, ok := featureGlosses[name]; ok {
		return gloss
	}

	if d, ok := forge.DisplayFor(props.FeatureID(name)); ok {
		return d.Label + " credential wizard, release source and config section"
	}

	return ""
}

func labelWidth(names []string, label func(string) string) int {
	width := 0
	for _, n := range names {
		width = max(width, len(label(n)))
	}

	return width
}

// featureOptions builds the wizard's feature checklist from the same set the
// --features flag accepts and defaults to, so the two entry points cannot
// disagree about what is selectable. Ticks come from the current selection
// (the flag's value, or its default), not from the default set: huh's
// accessor adds the bound values to whatever is already ticked and never
// clears, so pre-ticking the defaults widened an explicit --features (#42).
func featureOptions(selected []string) []huh.Option[string] {
	opts := make([]huh.Option[string], 0, len(generator.SelectableFeatures))
	width := labelWidth(generator.SelectableFeatures, featureLabel)

	for _, name := range generator.SelectableFeatures {
		opts = append(opts,
			huh.NewOption(optionLabel(featureLabel(name), featureGloss(name), width), name).
				Selected(slices.Contains(selected, name)))
	}

	return opts
}

// featureLabel resolves a feature's display name, preferring the static table,
// then the forge registry, and finally the raw config name so an unlabelled
// feature is still selectable rather than blank.
func featureLabel(name string) string {
	if label, ok := featureLabels[name]; ok {
		return label
	}

	if d, ok := forge.DisplayFor(props.FeatureID(name)); ok {
		return d.Label
	}

	return name
}

func forgeBackendOptions() []huh.Option[string] {
	displays := forgeBackendDisplays()
	opts := make([]huh.Option[string], 0, len(displays))

	for _, d := range displays {
		opts = append(opts, huh.NewOption(d.Label, string(d.ID)))
	}

	return opts
}

// forgeBackendDisplays is the display data for every backend, in catalogue
// order.
func forgeBackendDisplays() []forge.Display {
	backends := generator.ForgeBackends()
	out := make([]forge.Display, 0, len(backends))

	for _, id := range backends {
		if d, ok := forge.DisplayFor(id); ok {
			out = append(out, d)
		}
	}

	return out
}

// forgeBackendNames lists the accepted --forge-backend values, for the flag's
// help text. Derived from the same table as the wizard, so the flag cannot
// document a set the wizard does not offer.
func forgeBackendNames() []string {
	displays := forgeBackendDisplays()
	names := make([]string, 0, len(displays))

	for _, d := range displays {
		names = append(names, string(d.ID))
	}

	return names
}

// deriveEnvPrefix is the default env-var prefix for a project name: upper-case
// with hyphens turned into underscores (my-app → MY_APP).
func deriveEnvPrefix(name string) string {
	return strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

func (o *SkeletonOptions) runWizard() error {
	if err := o.wizardForm().Run(); err != nil {
		return err
	}

	return o.afterWizard()
}

// afterWizard makes the form's final state the only state that counts. A
// group hidden by WithHideFunc keeps whatever the user typed in it before
// navigating back and changing the answer that hides it, so those values are
// discarded here rather than letting an earlier email re-enable signing that
// a later No switched off (#46). The flag path keeps "email implies signing".
func (o *SkeletonOptions) afterWizard() error {
	o.NoForge = !o.hosted

	if o.NoForge {
		o.ForgeBackend, o.Repo, o.Host = "", "", ""
		o.Private = false
		o.ForgeCredentials = nil
	} else {
		o.Module = ""
		o.ForgeCredentials = slices.DeleteFunc(o.ForgeCredentials, func(f string) bool { return f == o.ForgeBackend })
	}

	if !o.updateSelected() {
		o.ReleaseChannel = ""
		o.Direct = generator.ManifestDirectSource{}
		o.UpdatePolicy, o.UpdateCheckInterval = "", ""
		o.Signing = false
	}

	if o.ReleaseChannel != generator.ReleaseChannelDirect {
		o.Direct = generator.ManifestDirectSource{}
	}

	if !o.Signing {
		o.SigningEmail = ""
		o.SigningKeySource = ""
		o.SigningKeyID = ""
		o.SigningRequireSignature, o.SigningRequireChecksum = false, false
	}

	o.clearFeaturePages()

	if o.HelpType != "slack" {
		o.SlackChannel, o.SlackTeam = "", ""
	}

	if o.HelpType != "teams" {
		o.TeamsChannel, o.TeamsTeam = "", ""
	}

	return nil
}

// clearFeaturePages drops the answers of the feature-gated pages whose
// feature is not selected, and the addressing the chosen chat provider does
// not use (spec 0196 D1, D2; 0197 D5).
func (o *SkeletonOptions) clearFeaturePages() {
	if !o.aiSelected() {
		o.ChatDefault = generator.ManifestChatDefault{}
	} else {
		if !chatDefaultNeedsEndpoint(o.ChatDefault.Provider) {
			o.ChatDefault.BaseURL, o.ChatDefault.APIVersion = "", ""
		}

		if !chatDefaultUsesCloudAddressing(o.ChatDefault.Provider) {
			o.ChatDefault.Project, o.ChatDefault.Location = "", ""
		}
	}

	if !o.mcpSelected() {
		o.MCPMode, o.MCPExposed = "", nil
	}

	if !slices.Contains(o.Features, string(props.TelemetryCmd)) {
		o.TelemetryEndpoint, o.TelemetryOTelEndpoint = "", ""
	}
}

// basicsGroup is the entry group: project basics plus the backend and help-type
// selections that drive later groups. Nothing here writes into a later field:
// huh copies a bound value into its Input once, at construction, so a value
// seeded after the form is built never renders and is overwritten on blur
// (#41). Later fields derive from these through reactive binders instead.
func (o *SkeletonOptions) basicsGroup() *huh.Group {
	// On a revisit (gtb wizard, spec 0197 D13) the name is shown, not asked:
	// renaming moves cmd/<name> and every import path, which is not this
	// wizard's job; and there is no destination to choose.
	fields := make([]huh.Field, 0, 8) //nolint:mnd // the page's field count

	if o.revisit {
		fields = append(fields, huh.NewNote().Title("Project: "+o.Name).
			Description("Settings are pre-filled from .gtb/manifest.yaml; accept a page to keep it."))
	} else {
		fields = append(fields, huh.NewInput().
			Title("Project Name").
			Value(&o.Name).
			Validate(func(s string) error {
				return hintedValidation(generator.ValidateName(s))
			}))
	}

	fields = append(fields, huh.NewInput().
		Title("Description").
		Placeholder("A new tool").
		Value(&o.Description))

	if !o.revisit {
		fields = append(fields, huh.NewInput().
			Title("Destination Path").
			Value(&o.Path))
	}

	return huh.NewGroup(append(fields,
		newMultiSelect("Features", "", featureOptions(o.Features)).
			Value(&o.Features),
		huh.NewConfirm().
			Key("hosted").
			Title("Hosted on a forge?").
			Description("Yes: the next page asks which forge and where. No: the project names its own Go module path and links no forge.").
			Affirmative("Yes").
			Negative("No").
			Value(&o.hosted),
		huh.NewSelect[string]().
			Title("Help Channel").
			Description("Where users should ask for help — shown in error messages.").
			Options(
				huh.NewOption("None", "none"),
				huh.NewOption("Slack", "slack"),
				huh.NewOption("Microsoft Teams", "teams"),
			).
			Value(&o.HelpType),
	)...).
		Title(basicsTitle(o.revisit)).
		Description("Configure your CLI tool. The pages that follow depend on what you choose here.\n")
}

func basicsTitle(revisit bool) string {
	if revisit {
		return "Project Settings"
	}

	return "New CLI Project"
}

// wizardForm assembles the single native form: the entry group plus the
// per-stage groups. Conditional sections (help channel, signing details) use
// WithHideFunc and huh skips the hidden groups; content that depends on the
// chosen backend uses reactive *Func binders. Back-navigation is shift+tab.
// It is a seam for tests, which drive the form directly.
func (o *SkeletonOptions) wizardForm() *huh.Form {
	// Fixed default — not derived from another field, so it is set upfront. The
	// signing detail group is hidden unless signing is enabled, but the key
	// source select still binds to this so it defaults to "Both" when shown.
	if o.SigningKeySource == "" {
		o.SigningKeySource = "both"
	}

	// The confirm is bound positively (spec 0195 D3); the flag is the negative.
	o.hosted = !o.NoForge

	// The mode select needs a value to sit on; compact is the framework's
	// default and what an untouched flag path means.
	if o.MCPMode == "" {
		o.MCPMode = string(props.MCPCompact)
	}

	return newForm(
		o.basicsGroup(),
		o.forgeGroup(),
		o.moduleGroup(),
		o.envPrefixGroup(),
		o.selfUpdateGroup(),
		o.directSourceGroup(),
		o.chatProvidersGroup(),
		o.chatEndpointGroup(),
		o.chatCloudGroup(),
		o.telemetryGroup(),
		o.mcpGroup(),
		o.slackGroup(),
		o.teamsGroup(),
		o.signingEnableGroup(),
		o.signingDetailGroup(),
		o.signingEnforcementGroup(),
	)
}

// telemetryGroup asks where telemetry goes, when the feature is selected
// (spec 0197 D5). Both endpoints are optional: the feature ships with the
// framework's defaults otherwise.
func (o *SkeletonOptions) telemetryGroup() *huh.Group {
	endpoint := func(s string) error { return hintedValidation(generator.ValidateTelemetryEndpoint(s)) }

	return huh.NewGroup(
		huh.NewInput().Key("telemetry-endpoint").Title("Telemetry endpoint (optional)").
			Description("Where usage events are sent; HTTPS.").
			Placeholder("https://telemetry.example.internal").
			Value(&o.TelemetryEndpoint).Validate(endpoint),
		huh.NewInput().Key("telemetry-otel-endpoint").Title("OpenTelemetry endpoint (optional)").
			Description("An OTel collector for traces and metrics.").
			Placeholder("https://otel.example.internal").
			Value(&o.TelemetryOTelEndpoint).Validate(endpoint),
	).
		Title("Telemetry").
		Description("Recorded under properties.telemetry in the manifest.\n").
		WithHideFunc(func() bool { return !slices.Contains(o.Features, string(props.TelemetryCmd)) })
}

// signingEnforcementGroup asks the enforcement a first run holds back:
// require_signature breaks every update until a signed release exists, so
// it is asked only on a revisit (spec 0197 D5, OQ4).
func (o *SkeletonOptions) signingEnforcementGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewConfirm().Key("signing-require-signature").
			Title("Require a valid signature on every update?").
			Description("Not before your first signed release has shipped: an unsigned release would then fail every consumer's update.").
			Affirmative("Yes").Negative("No").
			Value(&o.SigningRequireSignature),
	).
		Title("Signing enforcement").
		WithHideFunc(func() bool { return !o.revisit || !o.updateSelected() || !o.Signing })
}

// updateSelected reports whether the update feature is among the chosen
// features; the self-update, direct-source and signing pages hang off it.
func (o *SkeletonOptions) updateSelected() bool {
	return slices.Contains(o.Features, string(props.UpdateCmd))
}

// moduleGroup asks for the Go module path of a project that is not hosted on
// a forge (spec 0195 D5); a hosted project derives it and never sees this.
func (o *SkeletonOptions) moduleGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Key("module").
			Title("Go module path").
			Description("The module line of go.mod. A tool that is never imported can be a single word.").
			Placeholder("myapp").
			Value(&o.Module).
			Validate(func(s string) error {
				if s == "" {
					return ErrModuleRequired
				}

				return generator.ValidateModulePath(s)
			}),
	).
		Title("Module").
		Description("Without a forge there is no host and repository to derive a module path from.\n").
		WithHideFunc(func() bool { return o.hosted })
}

// selfUpdateGroup is the one page for the update feature (spec 0195 D7): the
// release channel, the policy and the check interval. Shown only when the
// feature is selected; the channel cannot be left empty.
func (o *SkeletonOptions) selfUpdateGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewSelect[string]().
			Key("channel").
			Title("Release channel").
			Description("Where the tool fetches its releases from.").
			Options(
				huh.NewOption("This forge", generator.ReleaseChannelForge),
				huh.NewOption("Direct URL (a server you name)", generator.ReleaseChannelDirect),
			).
			Value(&o.ReleaseChannel).
			Validate(func(s string) error {
				switch {
				case s == "":
					return ErrReleaseChannelRequired
				case s == generator.ReleaseChannelForge && !o.hosted:
					return errors.WithHint(ErrReleaseChannelRequired, "This project is not hosted on a forge; choose the direct channel.")
				default:
					return nil
				}
			}),
		huh.NewSelect[string]().
			Title("Self-update policy").
			Description("What the tool does when a newer release exists. Users can override via update.policy.").
			Options(
				huh.NewOption("Notify only: log that an update exists, then continue (default)", "").Selected(o.UpdatePolicy == "" || o.UpdatePolicy == "disabled"),
				huh.NewOption("Prompt: ask to update; declining continues the command", "prompt").Selected(o.UpdatePolicy == "prompt"),
				huh.NewOption("Enforce: block every command until the tool is updated", "enabled").Selected(o.UpdatePolicy == "enabled"),
			).
			Value(&o.UpdatePolicy),
		huh.NewInput().
			Title("Update check interval").
			Description("How often the tool checks for releases, as a Go duration (24h, 168h). Empty is the framework default (24h); the check runs under every policy.").
			Placeholder("24h").
			Value(&o.UpdateCheckInterval).
			Validate(generator.ValidateUpdateCheckInterval),
	).
		Title("Self-update").
		Description("How the generated tool finds and applies its own releases.\n").
		WithHideFunc(func() bool { return !o.updateSelected() })
}

// directSourceGroup collects the go/forge direct source's settings when the
// direct channel is chosen (spec 0195 D7, OQ7: all of them; the URL template
// and the version URL are required).
func (o *SkeletonOptions) directSourceGroup() *huh.Group {
	required := func(s string) error {
		if s == "" {
			return ErrDirectSourceIncomplete
		}

		return nil
	}

	return huh.NewGroup(
		huh.NewInput().Key("release-url").Title("Asset URL template").
			Description("Where a release asset is fetched from; {{.Version}} and {{.Asset}} are substituted.").
			Placeholder("https://dl.example.com/myapp/{{.Version}}/{{.Asset}}").
			Value(&o.Direct.URLTemplate).Validate(required),
		huh.NewInput().Key("release-version-url").Title("Version URL").
			Description("An endpoint that reports the latest version.").
			Placeholder("https://dl.example.com/myapp/latest").
			Value(&o.Direct.VersionURL).Validate(required),
		huh.NewInput().Title("Checksum URL template (optional)").
			Value(&o.Direct.ChecksumURLTemplate),
		huh.NewInput().Title("Signature URL template (optional)").
			Value(&o.Direct.SignatureURLTemplate),
		huh.NewInput().Title("Version format (optional)").
			Description("text, json, yaml or xml; empty means text.").
			Value(&o.Direct.VersionFormat),
		huh.NewInput().Title("Version key (optional)").
			Description("The key holding the version in a structured endpoint.").
			Value(&o.Direct.VersionKey),
		huh.NewInput().Title("Pinned version (optional)").
			Value(&o.Direct.PinnedVersion),
	).
		Title("Direct release source").
		Description("Recorded under release_source.direct in the manifest.\n").
		WithHideFunc(func() bool {
			return !o.updateSelected() || o.ReleaseChannel != generator.ReleaseChannelDirect
		})
}

// chatProvidersGroup is the AI page (spec 0196 D1): which providers the tool
// links, which is the default, and optionally which model. Shown only when
// the ai feature is selected; every configurable provider is pre-selected,
// because a generated tool is configured by its consumers the way gtb itself
// is (spec 0194 OQ4).
//
// The default select is fed twice. Static options from the current selection
// are what the field shows before huh runs its option command, and what a
// test harness that never runs commands sees; OptionsFunc bound to the
// providers is what the live program shows once the user ticks or unticks a
// provider on the same page. Between several providers the first option is a
// placeholder the validator refuses, so Enter cannot pick a default on the
// author's behalf (OQ5); a single provider is offered alone and accepted.
func (o *SkeletonOptions) chatProvidersGroup() *huh.Group {
	return huh.NewGroup(
		newMultiSelect("Chat providers",
			"Each one is a module linked into the binary; untick what this tool will never use. Linking is per module: codex-local links chat-openai, which registers openai and openai-compatible too.",
			chatProviderOptions(o.ChatProviders)).
			Key("chat-providers").
			Value(&o.ChatProviders).
			Validate(func(selected []string) error {
				// Refused here, at the field, rather than after the wizard
				// with every answer gone (#48).
				return hintedValidation(generator.ValidateChatProviders(selected, o.resolveFeatures()))
			}),
		huh.NewSelect[string]().Key("chat-default").Title("Default provider").
			Description("Shipped as the tool's default; an end user overrides it in their own config.").
			Options(chatDefaultOptions(o.ChatProviders)...).
			OptionsFunc(func() []huh.Option[string] { return chatDefaultOptions(o.ChatProviders) }, &o.ChatProviders).
			Value(&o.ChatDefault.Provider).
			Validate(func(provider string) error {
				return hintedValidation(generator.ValidateChatDefaultProvider(provider, o.ChatProviders, o.resolveFeatures()))
			}),
		huh.NewInput().Key("chat-model").Title("Default model (optional)").
			Description("Blank means the provider module's choice, which favours capability over cost.").
			Value(&o.ChatDefault.Model),
	).
		Title("AI Chat").
		Description("The ai feature needs at least one provider.\n").
		WithHideFunc(func() bool { return !slices.Contains(o.Features, string(props.AiCmd)) })
}

// chatDefaultOptions offers the linked providers as the default. Between
// several, a placeholder leads so that nothing is chosen until the author
// moves; the empty value is what ValidateChatDefault refuses.
func chatDefaultOptions(providers []string) []huh.Option[string] {
	opts := make([]huh.Option[string], 0, len(providers)+1)

	if len(providers) > 1 {
		opts = append(opts, huh.NewOption("Choose the default provider", ""))
	}

	for _, name := range providers {
		opts = append(opts, huh.NewOption(name, name))
	}

	return opts
}

// chatEndpointGroup asks for the endpoint the default provider refuses to
// construct without (spec 0196 D2): a base URL for openai-compatible and
// azure-openai, and the dated API version for azure-openai. huh hides groups,
// not fields, so this is its own page shown only for those providers.
func (o *SkeletonOptions) chatEndpointGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewInput().Key("chat-base-url").Title("API endpoint").
			Description("HTTPS, no credentials in the URL. Ollama: https://host:11434/v1; Azure: the deployment endpoint.").
			Placeholder("https://llm.example.internal/v1").
			Value(&o.ChatDefault.BaseURL).
			Validate(func(string) error { return o.validateChatEndpointField() }),
		huh.NewInput().Key("chat-api-version").Title("API version").
			Description("Required by azure-openai (dated, e.g. 2024-10-21); ignored by other providers.").
			Value(&o.ChatDefault.APIVersion).
			Validate(func(string) error { return o.validateChatEndpointField() }),
	).
		Title("AI endpoint").
		Description("Recorded under chat.default in the manifest.\n").
		WithHideFunc(func() bool { return !o.aiSelected() || !chatDefaultNeedsEndpoint(o.ChatDefault.Provider) })
}

// chatCloudGroup asks for the cloud addressing gemini-vertex and bedrock use
// (spec 0196 D2). Both are optional: the modules fall back to the platform's
// environment for them.
func (o *SkeletonOptions) chatCloudGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewInput().Key("chat-project").Title("Cloud project (optional)").
			Description("gemini-vertex; falls back to GOOGLE_CLOUD_PROJECT.").
			Value(&o.ChatDefault.Project),
		huh.NewInput().Key("chat-location").Title("Region (optional)").
			Description("gemini-vertex falls back to GOOGLE_CLOUD_LOCATION; bedrock to the AWS chain.").
			Value(&o.ChatDefault.Location),
	).
		Title("AI cloud addressing").
		Description("Recorded under chat.default in the manifest.\n").
		WithHideFunc(func() bool { return !o.aiSelected() || !chatDefaultUsesCloudAddressing(o.ChatDefault.Provider) })
}

func (o *SkeletonOptions) aiSelected() bool {
	return slices.Contains(o.Features, string(props.AiCmd))
}

// validateChatEndpointField validates the endpoint page as a whole from
// either field: huh binds an Input's value on Blur, which precedes Validate,
// so the struct already carries what was typed.
func (o *SkeletonOptions) validateChatEndpointField() error {
	return hintedValidation(generator.ValidateChatDefault(o.ChatDefault, o.ChatProviders, o.resolveFeatures()))
}

func chatDefaultNeedsEndpoint(provider string) bool {
	return provider == string(gochat.ProviderOpenAICompatible) || provider == string(gochat.ProviderAzureOpenAI)
}

func chatDefaultUsesCloudAddressing(provider string) bool {
	return provider == string(gochat.ProviderGeminiVertex) || provider == string(gochat.ProviderBedrock)
}

func chatProviderOptions(selected []string) []huh.Option[string] {
	known := generator.DefaultChatProviders()
	opts := make([]huh.Option[string], 0, len(known))
	width := labelWidth(known, func(s string) string { return s })

	for _, name := range known {
		opts = append(opts, huh.NewOption(optionLabel(name, providerGlosses[name], width), name).
			Selected(slices.Contains(selected, name)))
	}

	return opts
}

// envPrefixGroup collects the config env-var prefix. The derived prefix is
// offered as a suggestion (accepted with tab or the right arrow), not written
// into the field, because huh binds an Input's value once (#41); empty means
// no prefix, the same as the flag.
func (o *SkeletonOptions) envPrefixGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Key("env-prefix").
			Title("Environment Variable Prefix").
			DescriptionFunc(func() string {
				return fmt.Sprintf("Prefix for config env var overrides (e.g. %[1]s → %[1]s_LOG_LEVEL). ctrl+e accepts the suggestion; leave empty to disable.", deriveEnvPrefix(o.Name))
			}, &o.Name).
			Placeholder("e.g. MY_APP").
			SuggestionsFunc(func() []string {
				return []string{deriveEnvPrefix(o.Name)}
			}, &o.Name).
			Value(&o.EnvPrefix).
			Validate(func(s string) error {
				return hintedValidation(generator.ValidateEnvPrefix(s))
			}),
	).
		Title("Environment Variable Prefix").
		Description("Scopes config env var lookups so only variables starting with this prefix are considered.\n")
}

// forgeGroup is the forge page (spec 0195 D3, D4, D6): which forge, where,
// under what path, private or not, and the other forges the tool captures
// credentials for. Shown only for a hosted project; the host and repository
// help react to the backend through *Func binders.
func (o *SkeletonOptions) forgeGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewSelect[string]().
			Key("backend").
			Title("Forge Backend").
			Description("The forge the repository lives on. Decides the release source, the credential wizard, the linked adapter and the CI skeleton (GitHub and GitLab have one).").
			Options(forgeBackendOptions()...).
			Value(&o.ForgeBackend).
			Validate(generator.ValidateForgeBackend),
		huh.NewInput().
			Title("Git Host").
			DescriptionFunc(func() string {
				return fmt.Sprintf("The %s host. Leave empty for %s; set it only for a self-hosted instance.",
					backendLabel(o.ForgeBackend), hostForBackend(o.ForgeBackend))
			}, &o.ForgeBackend).
			PlaceholderFunc(func() string { return hostForBackend(o.ForgeBackend) }, &o.ForgeBackend).
			Value(&o.Host),
		huh.NewInput().
			Key("repo").
			Title("Repository").
			DescriptionFunc(func() string { return repoDescription(o.ForgeBackend) }, &o.ForgeBackend).
			PlaceholderFunc(func() string { return repoPlaceholder(o.ForgeBackend) }, &o.ForgeBackend).
			Value(&o.Repo).
			Validate(func(s string) error {
				if s == "" {
					return ErrRepositoryRequired
				}

				return hintedValidation(generator.ValidateRepo(s))
			}),
		huh.NewConfirm().
			Title("Private Repository").
			Description("Does this repository require authentication to access releases? Enable for private repos; leave off for public ones.").
			Affirmative("Private").
			Negative("Public").
			Value(&o.Private),
		newMultiSelect("Other forges to capture credentials for",
			"Each adds that forge's init wizard, config section and adapter; the release source stays the backend's.",
			forgeCredentialOptions(o.ForgeCredentials)).
			Value(&o.ForgeCredentials),
	).
		Title("Forge").
		Description("Where the repository lives and how the tool reaches it.\n").
		WithHideFunc(func() bool { return !o.hosted })
}

// forgeCredentialOptions lists every backend for the credential multi-select,
// ticked from the current selection (#42's rule); the chosen backend is
// dropped from the answer afterwards, since it is already enabled.
func forgeCredentialOptions(selected []string) []huh.Option[string] {
	displays := forgeBackendDisplays()
	opts := make([]huh.Option[string], 0, len(displays))

	for _, d := range displays {
		opts = append(opts, huh.NewOption(d.Label, string(d.ID)).Selected(slices.Contains(selected, string(d.ID))))
	}

	return opts
}

// slackGroup collects Slack help-channel details. Shown only when the help
// channel is Slack.
func (o *SkeletonOptions) slackGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Slack Channel").
			Description("The channel where users should ask for help (e.g. #platform-help).").
			Placeholder("#my-team-help").
			Value(&o.SlackChannel).
			Validate(func(s string) error {
				if s == "" {
					return ErrHelpChannelRequired
				}

				return hintedValidation(generator.ValidateSlackChannel(s))
			}),
		huh.NewInput().
			Title("Slack Team").
			Description("The team or squad name owning this tool.").
			Placeholder("My Team").
			Value(&o.SlackTeam).
			Validate(func(s string) error { return hintedValidation(generator.ValidateSlackTeam(s)) }),
	).
		Title("Slack Help Configuration").
		Description("These values appear in error messages to direct users to support.\n").
		WithHideFunc(func() bool { return o.HelpType != "slack" })
}

// teamsGroup collects Microsoft Teams help-channel details. Shown only when the
// help channel is Teams.
func (o *SkeletonOptions) teamsGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Teams Channel").
			Description("The channel where users should ask for help.").
			Placeholder("Support").
			Value(&o.TeamsChannel).
			Validate(func(s string) error {
				if s == "" {
					return ErrHelpChannelRequired
				}

				return hintedValidation(generator.ValidateTeamsChannel(s))
			}),
		huh.NewInput().
			Title("Teams Team").
			Description("The team name owning this tool.").
			Placeholder("Engineering").
			Value(&o.TeamsTeam).
			Validate(func(s string) error { return hintedValidation(generator.ValidateTeamsTeam(s)) }),
	).
		Title("Microsoft Teams Help Configuration").
		Description("These values appear in error messages to direct users to support.\n").
		WithHideFunc(func() bool { return o.HelpType != "teams" })
}

// signingEnableGroup asks whether to enable release signing (default No).
func (o *SkeletonOptions) signingEnableGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewConfirm().
			Key("signing").
			Title("Enable release signing?").
			Description("Sets up consumer-side self-update signature verification. Needs a signing key and a published WKD endpoint — leave off unless you have them.").
			Affirmative("Yes").
			Negative("No").
			Value(&o.Signing),
	).
		Title("Release Signing").
		Description("Verify self-update downloads against an embedded release key.\n").
		WithHideFunc(func() bool { return !o.updateSelected() })
}

// signingDetailGroup collects the WKD email and key source. Shown only when
// signing is enabled. require_signature is deliberately never prompted — it
// stays false until a signed release has shipped and is flipped later via
// `gtb enable signing`.
func (o *SkeletonOptions) signingDetailGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Release WKD email").
			Description("Derives the WKD URL and enables the external trust-anchor leg. Leave empty for embedded-only.").
			Placeholder("release@example.com").
			Value(&o.SigningEmail),
		huh.NewSelect[string]().
			Title("Key source").
			Description("Where the trust anchor comes from.").
			Options(
				huh.NewOption("Both (embedded + external cross-check)", "both"),
				huh.NewOption("Embedded only", "embedded"),
				huh.NewOption("External (WKD) only", "external"),
			).
			Value(&o.SigningKeySource),
		huh.NewInput().
			Title("Signing key id (optional)").
			Description("KMS key alias/ARN/id the release pipeline signs with. Leave blank to wire the GoReleaser signs block later via `gtb enable signing --key-id`.").
			Placeholder("alias/myapp-release-signing-v1").
			Value(&o.SigningKeyID),
		huh.NewConfirm().Key("signing-require-checksum").
			Title("Require a verified checksum on every update?").
			Description("Safe from day one: every release ships checksums.").
			Affirmative("Yes").Negative("No").
			Value(&o.SigningRequireChecksum),
	).
		Title("Signing Configuration").
		Description("These values are written to the manifest signing block.\n").
		WithHideFunc(func() bool { return !o.updateSelected() || !o.Signing })
}

// resolveFeatures builds the full feature list from the selected set,
// marking unselected defaults as explicitly disabled.
func resolveFeatures(selected []string) []generator.ManifestFeature {
	defaultFeatures := generator.DefaultSelectedFeatures

	selectedMap := make(map[string]bool, len(selected))
	for _, f := range selected {
		selectedMap[f] = true
	}

	features := make([]generator.ManifestFeature, 0, len(selected)+len(defaultFeatures))
	for _, f := range selected {
		features = append(features, generator.ManifestFeature{Name: f, Enabled: true})
	}

	for _, f := range defaultFeatures {
		if !selectedMap[f] {
			features = append(features, generator.ManifestFeature{Name: f, Enabled: false})
		}
	}

	return features
}

// resolveFeatures is the manifest feature list for the options: the selected
// built-ins, the defaults left off as disabled, and the forge features the
// backend and the credential forges imply (spec 0195 D1, D6). A project that
// is not hosted enables no forge.
func (o *SkeletonOptions) resolveFeatures() []generator.ManifestFeature {
	features := resolveFeatures(o.Features)

	if o.NoForge {
		return features
	}

	for _, id := range o.impliedForges() {
		if !slices.ContainsFunc(features, func(f generator.ManifestFeature) bool { return f.Name == string(id) }) {
			features = append(features, generator.ManifestFeature{Name: string(id), Enabled: true})
		}
	}

	return features
}

// impliedForges is the backend followed by the credential forges, deduplicated.
func (o *SkeletonOptions) impliedForges() []props.FeatureID {
	var ids []props.FeatureID

	if o.ForgeBackend != "" {
		ids = append(ids, props.FeatureID(o.ForgeBackend))
	}

	for _, extra := range o.ForgeCredentials {
		if id := props.FeatureID(extra); id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}

	return ids
}

// preflight applies flag defaults and rejects contradictory or unknown values
// before Run touches the filesystem, so a bad invocation fails without leaving a
// half-scaffolded directory behind.
func (o *SkeletonOptions) preflight() error {
	mode, err := generator.ParseOverwriteMode(o.Overwrite)
	if err != nil {
		return errors.Wrapf(ErrInvalidOverwriteValue, "%q", o.Overwrite)
	}

	o.Overwrite = string(mode)

	// --push implies a commit to push; --no-git removes it. The two are
	// contradictory, so reject the combination rather than silently dropping one.
	if o.NoGit && o.Push {
		return ErrGitFlagsConflict
	}

	return nil
}

func (o *SkeletonOptions) Run(ctx context.Context, p *props.Props) error {
	if err := o.preflight(); err != nil {
		return err
	}

	templates, err := o.resolveTemplateSources(p)
	if err != nil {
		return err
	}

	gen := generator.New(p, &generator.Config{
		DryRun:    o.shared.dryRun(),
		Path:      o.Path,
		Overwrite: generator.OverwriteMode(o.Overwrite),
		GitInit:   !o.NoGit,
		GitPush:   o.Push,
		GitBranch: o.GitBranch,
		NoVerify:  o.NoVerify,
	}).EnableRealTemplateClone()

	return gen.GenerateSkeleton(ctx, o.skeletonConfig(templates))
}

// skeletonConfig assembles the generator's input from the resolved options.
func (o *SkeletonOptions) skeletonConfig(templates []generator.TemplateSource) generator.SkeletonConfig {
	features := o.resolveFeatures()

	// The chat list only means something with the ai feature; without it the
	// manifest carries no chat block, and enabling ai later records the
	// default set (spec 0194 D4, D7).
	chat := generator.ManifestChat{Providers: o.ChatProviders, Default: o.ChatDefault}
	if !slices.Contains(o.Features, string(props.AiCmd)) {
		chat = generator.ManifestChat{}
	} else if chat.Default.Provider == "" && len(chat.Providers) == 1 {
		chat.Default.Provider = chat.Providers[0]
	}

	helpType := o.HelpType
	if helpType == "none" {
		helpType = ""
	}

	cfg := generator.SkeletonConfig{
		Name:                  o.Name,
		Description:           o.Description,
		Path:                  o.Path,
		GoVersion:             o.GoVersion,
		Features:              features,
		Chat:                  chat,
		HelpType:              helpType,
		SlackChannel:          o.SlackChannel,
		SlackTeam:             o.SlackTeam,
		TeamsChannel:          o.TeamsChannel,
		TeamsTeam:             o.TeamsTeam,
		EnvPrefix:             o.EnvPrefix,
		TelemetryEndpoint:     o.TelemetryEndpoint,
		TelemetryOTelEndpoint: o.TelemetryOTelEndpoint,
		Bootstrap:             o.Bootstrap,
		ConfigLayers:          o.ConfigLayers,
		UpdatePolicy:          o.UpdatePolicy,
		MCPMode:               o.MCPMode,
		UpdateCheckInterval:   o.UpdateCheckInterval,
		CIComponentSource:     o.CIComponentSource,
		Signing:               o.resolveSigning(),
		Templates:             templates,
		ModulePath:            o.Module,
	}

	if !o.NoForge {
		cfg.Repo = o.Repo
		cfg.Host = o.resolvedHost()
		cfg.Private = o.Private
		cfg.ForgeBackend = props.FeatureID(o.ForgeBackend)

		for _, extra := range o.ForgeCredentials {
			cfg.ForgeCredentials = append(cfg.ForgeCredentials, props.FeatureID(extra))
		}
	}

	if slices.Contains(o.Features, string(props.UpdateCmd)) {
		cfg.ReleaseChannel = o.resolvedReleaseChannel()
		if cfg.ReleaseChannel == generator.ReleaseChannelDirect {
			cfg.Direct = o.Direct
		}
	}

	return cfg
}

// isCIEnv reports whether the tool is running under CI, honouring the `ci`
// config key (set by the global --ci persistent flag). Used to suppress the
// interactive remote-template confirmation in CI.
func isCIEnv(p *props.Props) bool {
	if cfg := p.GetConfig(); cfg != nil {
		return cfg.View().GetBool("ci")
	}

	return false
}

// resolveTemplateSources parses each --template spec into a manifest
// TemplateSource and, for a remote (git) source, confirms the trust decision
// with the operator (suppressible under --ci / non-interactive). Adding a
// source is the trust decision; the pin records exactly what was trusted.
func (o *SkeletonOptions) resolveTemplateSources(p *props.Props) ([]generator.TemplateSource, error) {
	if len(o.Templates) == 0 {
		return nil, nil
	}

	sources := make([]generator.TemplateSource, 0, len(o.Templates))

	for _, spec := range o.Templates {
		ts, err := generator.ParseTemplateSpec(p.FS, spec, "")
		if err != nil {
			return nil, err
		}

		if err := icmd.ConfirmRemoteTemplate(p, isCIEnv(p), ts); err != nil {
			return nil, err
		}

		sources = append(sources, ts)
	}

	return sources, nil
}

// resolveSigning builds the manifest signing block from the options.
// Supplying a signing email implies --signing. The framework-default key
// source ("both") is stored as empty to keep the manifest minimal.
func (o *SkeletonOptions) resolveSigning() generator.ManifestSigning {
	enabled := o.Signing || o.SigningEmail != ""
	if !enabled {
		return generator.ManifestSigning{}
	}

	keySource := o.SigningKeySource
	if keySource == "both" {
		keySource = ""
	}

	// ApplySigningDefaults fills backend/region/public-key when a key id is
	// recorded, so the persisted manifest and the rendered .goreleaser.yaml
	// agree — the same defaulting `gtb enable signing` applies.
	return generator.ApplySigningDefaults(generator.ManifestSigning{
		Enabled:                   true,
		ExternalKeyEmail:          o.SigningEmail,
		KeySource:                 keySource,
		RequireExternalCrosscheck: o.SigningRequireExternalCrosscheck,
		Backend:                   o.SigningBackend,
		KeyID:                     o.SigningKeyID,
		KMSRegion:                 o.SigningKMSRegion,
		PublicKey:                 o.SigningPublicKey,
		// The wizard asks RequireSignature only on a revisit; the flag is the
		// author's explicit call (spec 0197 D5).
		RequireSignature: o.SigningRequireSignature,
		RequireChecksum:  o.SigningRequireChecksum,
	})
}
