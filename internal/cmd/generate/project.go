package generate

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/signing"

	icmd "gitlab.com/phpboyscout/go-tool-base/internal/cmd"
	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
	"gitlab.com/phpboyscout/go-tool-base/pkg/utils"
)

type SkeletonOptions struct {
	Name         string
	GitBackend   string
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

	// UpdateCheckInterval is the generated tool's baseline self-update-check
	// throttle as a Go duration string (e.g. "24h"). Empty leaves it unset so
	// the framework default (24h) applies.
	UpdateCheckInterval string

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

	// Templates carries the custom template-overlay specs (<src>@<ref>)
	// supplied via --template (repeatable). Each is parsed into a manifest
	// TemplateSource and layered over the embedded skeleton.
	Templates []string
}

func NewCmdSkeleton(p *props.Props) *cobra.Command {
	opts := SkeletonOptions{
		GitBackend: "github",
		HelpType:   "none",
	}

	cmd := &cobra.Command{
		Use:     "project",
		Aliases: []string{"cli", "skeleton"},
		Short:   "Generate a new project skeleton",
		Long: `Scaffold a complete new GTB-based CLI project.

Generates the full module layout: go.mod, the root command and main entry
point, the .gtb manifest, embedded default config and assets, selected feature
commands (init, update, mcp, docs, doctor, changelog, keychain, and optionally
ai/config/telemetry), and the CI pipeline for the chosen Git backend (GitHub or
GitLab). Optional release-signing and custom template overlays can be layered
in. By default the new project is git-initialised with an initial commit; pass
--no-git to skip that or --push to also add the remote and push.

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
	cmd.Flags().StringVar(&opts.GitBackend, "git-backend", defaultGitBackend,
		"Git backend ("+strings.Join(gitBackendNames(), ", ")+")")
	cmd.Flags().StringVar(&opts.Host, "host", "", "Git host (defaults to backend's canonical host)")
	cmd.Flags().BoolVar(&opts.Private, "private", false, "Mark the repository as private (requires a token for updates)")
	cmd.Flags().StringVarP(&opts.Description, "description", "d", "A tool built with gtb", "Project description")
	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Destination path")
	cmd.Flags().StringSliceVarP(&opts.Features, "features", "f", []string{"init", "update", "mcp", "docs", "doctor", "changelog", "keychain"}, "Features to enable (init, update, mcp, docs, doctor, changelog, keychain, ai, config, telemetry)")
	cmd.Flags().StringVar(&opts.GoVersion, "go-version", "", "Go version for go.mod (defaults to the running toolchain version)")
	cmd.Flags().StringVar(&opts.HelpType, "help-type", "none", "Help channel type (slack, teams, or none)")
	cmd.Flags().StringVar(&opts.Overwrite, "overwrite", "ask", "How to handle file conflicts: allow, deny, or ask")
	cmd.Flags().StringVar(&opts.SlackChannel, "slack-channel", "", "Slack channel for help (e.g. #my-team-help)")
	cmd.Flags().StringVar(&opts.SlackTeam, "slack-team", "", "Slack team name (e.g. My Team)")
	cmd.Flags().StringVar(&opts.TeamsChannel, "teams-channel", "", "Microsoft Teams channel for help")
	cmd.Flags().StringVar(&opts.TeamsTeam, "teams-team", "", "Microsoft Teams team name")
	cmd.Flags().StringVar(&opts.EnvPrefix, "env-prefix", "", "Environment variable prefix for config overrides (e.g. MY_APP)")
	cmd.Flags().StringVar(&opts.UpdatePolicy, "update-policy", "", "Self-update posture for the generated tool: disabled, prompt, or enabled (empty = framework default disabled)")
	cmd.Flags().StringVar(&opts.UpdateCheckInterval, "update-check-interval", "", "Baseline interval between self-update checks as a Go duration, e.g. 24h or 168h (empty = framework default 24h)")
	cmd.Flags().StringVar(&opts.CIComponentSource, "ci-component-source", "", "Override the phpboyscout/cicd component include base in the scaffolded GitLab pipeline (default gitlab.com/phpboyscout/cicd)")
	cmd.Flags().BoolVar(&opts.Signing, "signing", false, "Enable consumer-side release-signing verification (scaffolds internal/trustkeys and wires props.Signing)")
	cmd.Flags().StringVar(&opts.SigningEmail, "signing-email", "", "Release WKD email for signing (external_key_email); implies --signing")
	cmd.Flags().StringVar(&opts.SigningKeySource, "signing-key-source", "both", "Signing trust-anchor source: embedded, external, or both")
	cmd.Flags().BoolVar(&opts.SigningRequireExternalCrosscheck, "signing-require-external-crosscheck", false, "Fail signing closed when the external (WKD) resolver is unreachable")
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
	if o.Name == "" || o.Repo == "" {
		if !utils.IsInteractive() {
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

	return o.validateSigningFields()
}

// validateSigningFields checks the signing key-source value when signing
// is requested (explicitly or implied by a signing email).
func (o *SkeletonOptions) validateSigningFields() error {
	if !o.Signing && o.SigningEmail == "" {
		return nil
	}

	switch o.SigningKeySource {
	case "", "embedded", "external", "both":
	default:
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

	if err := generator.ValidateDescription(o.Description); err != nil {
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
		if verr := generator.ValidateOrg(org, o.GitBackend); verr != nil {
			return verr
		}
	}

	if err := o.validateUpdateFields(); err != nil {
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

	return generator.ValidateUpdateCheckInterval(o.UpdateCheckInterval)
}

// validateHelpFields groups the Slack/Teams help-channel checks.
func (o *SkeletonOptions) validateHelpFields() error {
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

// envPrefixRe validates the environment-variable prefix (upper-case, digits and
// underscores). It is a build-time literal, so MustCompile is safe.
var envPrefixRe = regexp.MustCompile(`^[A-Z0-9_]+$`)

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
	// Only a scaffoldable backend resolves to itself. A registered-but-not-
	// scaffoldable forge (Gitea, Bitbucket) falls back like any other unknown
	// value, which is what the hand-written branches did — each was
	// `if backend == "gitlab" { … } else { github }`, so everything that was
	// not GitLab rendered GitHub.
	if id := props.FeatureID(backend); scaffoldableBackends[id] {
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

// repoDescription is the repository-field help text for a git backend.
func repoDescription(backend string) string { return backendDisplay(backend).RepoDescription }

// repoPlaceholder is the repository-field placeholder for a git backend.
func repoPlaceholder(backend string) string { return backendDisplay(backend).RepoPlaceholder }

// scaffoldableBackends are the forges the generator has a skeleton asset set
// for (internal/generator/assets/skeleton-<backend>), and therefore the ones a
// generated project's CI, release automation and repository conventions can
// actually be written for.
//
// This is a narrower axis than the forge registry, and deliberately so. A forge
// feature gates a *credential wizard*; a git backend selects a *scaffolding
// asset set*. Gitea and Bitbucket have the first and not the second — which is
// also why generator.ValidateReleaseSourceType still rejects them. Offering
// them here would scaffold a project the generator cannot complete.
//
// Adding skeleton-<forge> is what widens this set.
var scaffoldableBackends = map[props.FeatureID]bool{
	forge.GithubFeature: true,
	forge.GitlabFeature: true,
}

// gitBackendOptions is the wizard's backend chooser: the registered forges that
// are also scaffoldable, so the options, the flag help and the credential
// wizards cannot drift apart the way they had.
func gitBackendOptions() []huh.Option[string] {
	displays := scaffoldableDisplays()
	opts := make([]huh.Option[string], 0, len(displays))

	for _, d := range displays {
		opts = append(opts, huh.NewOption(d.Label, string(d.ID)))
	}

	return opts
}

// scaffoldableDisplays is the forge registry filtered to what can be
// scaffolded, in the registry's deterministic order.
func scaffoldableDisplays() []forge.Display {
	all := forge.Displays()
	out := make([]forge.Display, 0, len(all))

	for _, d := range all {
		if scaffoldableBackends[d.ID] {
			out = append(out, d)
		}
	}

	return out
}

// gitBackendNames lists the accepted --git-backend values, for the flag's help
// text and its validation. Derived from the same table as the wizard, so the
// flag cannot document a set the wizard does not offer.
func gitBackendNames() []string {
	displays := scaffoldableDisplays()
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
	return o.wizardForm().Run()
}

// basicsGroup is the entry group: project basics plus the backend and help-type
// selections that drive later groups. The name field seeds the env-prefix
// default and the backend field seeds the host default (both once, if unset), so
// the later groups pick those values up pre-filled.
func (o *SkeletonOptions) basicsGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Project Name").
			Value(&o.Name).
			Validate(func(s string) error {
				if s == "" {
					return ErrNameRequired
				}

				if o.EnvPrefix == "" {
					o.EnvPrefix = deriveEnvPrefix(s)
				}

				return nil
			}),
		huh.NewInput().
			Title("Description").
			Placeholder("A new tool").
			Value(&o.Description),
		huh.NewInput().
			Title("Destination Path").
			Value(&o.Path),
		huh.NewMultiSelect[string]().
			Title("Features").
			Options(
				huh.NewOption("Initialization", "init").Selected(true),
				huh.NewOption("Self-Update", "update").Selected(true),
				huh.NewOption("MCP Server", "mcp").Selected(true),
				huh.NewOption("Documentation", "docs").Selected(true),
				huh.NewOption("Doctor", "doctor").Selected(true),
				huh.NewOption("Changelog", "changelog").Selected(true),
				huh.NewOption("OS Keychain (credentials via go-keyring)", "keychain").Selected(true),
				huh.NewOption("AI Chat", "ai"),
				huh.NewOption("Config Management", "config"),
				huh.NewOption("Telemetry", "telemetry"),
			).
			Value(&o.Features),
		huh.NewSelect[string]().
			Title("Git Backend").
			Description("Where the repository will be hosted.").
			Options(gitBackendOptions()...).
			Value(&o.GitBackend).
			Validate(func(s string) error {
				if o.Host == "" {
					o.Host = hostForBackend(s)
				}

				return nil
			}),
		huh.NewSelect[string]().
			Title("Help Channel").
			Description("Where users should ask for help — shown in error messages.").
			Options(
				huh.NewOption("None", "none"),
				huh.NewOption("Slack", "slack"),
				huh.NewOption("Microsoft Teams", "teams"),
			).
			Value(&o.HelpType),
	).
		Title("New CLI Project").
		Description("Configure your new CLI tool. The next steps will collect repository and help channel details.\n")
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

	return newForm(
		o.basicsGroup(),
		o.envPrefixGroup(),
		o.updatePolicyGroup(),
		o.updateCheckIntervalGroup(),
		o.gitGroup(),
		o.slackGroup(),
		o.teamsGroup(),
		o.signingEnableGroup(),
		o.signingDetailGroup(),
	)
}

// envPrefixGroup collects the config env-var prefix. Its value is pre-seeded
// from the project name (see the name field's validator); the placeholder
// reactively shows that suggestion if the user clears the field.
func (o *SkeletonOptions) envPrefixGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Environment Variable Prefix").
			Description("Prefix for config env var overrides (e.g. MY_APP → MY_APP_LOG_LEVEL). Leave empty to disable.").
			PlaceholderFunc(func() string {
				return deriveEnvPrefix(o.Name)
			}, &o.Name).
			Value(&o.EnvPrefix).
			Validate(func(s string) error {
				if s == "" {
					return nil // opt-out
				}

				if !envPrefixRe.MatchString(s) {
					return ErrEnvPrefixInvalid
				}

				return nil
			}),
	).
		Title("Environment Variable Prefix").
		Description("Scopes config env var lookups so only variables starting with this prefix are considered.\n")
}

// updatePolicyGroup selects the self-update posture. The default (and "Disabled")
// leaves the policy empty so the framework default applies.
func (o *SkeletonOptions) updatePolicyGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewSelect[string]().
			Title("Self-Update Policy").
			Description("How the generated tool behaves when a newer release is found. Users can override via the update.policy config key.").
			Options(
				huh.NewOption("Disabled — log that an update is available, then continue (default)", "").Selected(o.UpdatePolicy == "" || o.UpdatePolicy == "disabled"),
				huh.NewOption("Prompt — ask to update; declining continues the command", "prompt").Selected(o.UpdatePolicy == "prompt"),
				huh.NewOption("Enabled — block every command until the tool is updated", "enabled").Selected(o.UpdatePolicy == "enabled"),
			).
			Value(&o.UpdatePolicy),
	).
		Title("Self-Update Policy").
		Description("Sets props.Tool.UpdatePolicy in the generated tool.\n")
}

// updateCheckIntervalGroup collects the baseline self-update-check throttle as a
// Go duration. Empty leaves it unset so the framework default (24h) applies.
func (o *SkeletonOptions) updateCheckIntervalGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Update Check Interval").
			Description("How often the generated tool checks for updates, as a Go duration (e.g. 24h, 168h). Leave empty for the framework default (24h). Users can override via update.check_interval.").
			Placeholder("24h").
			Value(&o.UpdateCheckInterval).
			Validate(generator.ValidateUpdateCheckInterval),
	).
		Title("Update Check Interval").
		Description("Sets props.Tool.UpdateCheckInterval in the generated tool.\n")
}

// gitGroup collects repository details. The host is pre-seeded from the chosen
// backend (see the backend field's validator); the host/repository help text and
// the repository placeholder react to the backend via *Func binders.
func (o *SkeletonOptions) gitGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Git Host").
			DescriptionFunc(func() string {
				return fmt.Sprintf("The %s host. Change this only if you use a self-hosted instance.", backendLabel(o.GitBackend))
			}, &o.GitBackend).
			Value(&o.Host).
			Validate(func(s string) error {
				if s == "" {
					return ErrHostRequired
				}

				return nil
			}),
		huh.NewInput().
			Title("Repository").
			DescriptionFunc(func() string { return repoDescription(o.GitBackend) }, &o.GitBackend).
			PlaceholderFunc(func() string { return repoPlaceholder(o.GitBackend) }, &o.GitBackend).
			Value(&o.Repo).
			Validate(func(s string) error {
				if s == "" {
					return ErrRepositoryRequired
				}

				if !strings.Contains(s, "/") {
					return ErrRepositoryInvalidFormat
				}

				return nil
			}),
		huh.NewConfirm().
			Title("Private Repository").
			Description("Does this repository require authentication to access releases? Enable for private repos; leave off for public ones.").
			Affirmative("Private").
			Negative("Public").
			Value(&o.Private),
	).
		Title("Repository").
		Description("Configure the repository that will host your new tool. The host and repository help below reflect the backend you chose.\n")
}

// slackGroup collects Slack help-channel details. Shown only when the help
// channel is Slack.
func (o *SkeletonOptions) slackGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Slack Channel").
			Description("The channel where users should ask for help (e.g. #platform-help).").
			Placeholder("#my-team-help").
			Value(&o.SlackChannel),
		huh.NewInput().
			Title("Slack Team").
			Description("The team or squad name owning this tool.").
			Placeholder("My Team").
			Value(&o.SlackTeam),
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
			Value(&o.TeamsChannel),
		huh.NewInput().
			Title("Teams Team").
			Description("The team name owning this tool.").
			Placeholder("Engineering").
			Value(&o.TeamsTeam),
	).
		Title("Microsoft Teams Help Configuration").
		Description("These values appear in error messages to direct users to support.\n").
		WithHideFunc(func() bool { return o.HelpType != "teams" })
}

// signingEnableGroup asks whether to enable release signing (default No).
func (o *SkeletonOptions) signingEnableGroup() *huh.Group {
	return huh.NewGroup(
		huh.NewConfirm().
			Title("Enable release signing?").
			Description("Sets up consumer-side self-update signature verification. Needs a signing key and a published WKD endpoint — leave off unless you have them.").
			Affirmative("Yes").
			Negative("No").
			Value(&o.Signing),
	).
		Title("Release Signing").
		Description("Verify self-update downloads against an embedded release key.\n")
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
	).
		Title("Signing Configuration").
		Description("These values are written to the manifest signing block.\n").
		WithHideFunc(func() bool { return !o.Signing })
}

// resolveFeatures builds the full feature list from the selected set,
// marking unselected defaults as explicitly disabled.
func resolveFeatures(selected []string) []generator.ManifestFeature {
	defaultFeatures := []string{"init", "update", "mcp", "docs", "doctor", "changelog", "keychain"}

	selectedMap := make(map[string]bool, len(selected))
	for _, f := range selected {
		selectedMap[f] = true
	}

	features := make([]generator.ManifestFeature, 0, len(defaultFeatures))
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

func (o *SkeletonOptions) Run(ctx context.Context, p *props.Props) error {
	if o.Overwrite == "" {
		o.Overwrite = "ask"
	}

	if o.Overwrite != "allow" && o.Overwrite != "deny" && o.Overwrite != "ask" {
		return errors.Wrapf(ErrInvalidOverwriteValue, "%q", o.Overwrite)
	}

	// --push implies a commit to push; --no-git removes it. The two are
	// contradictory, so reject the combination rather than silently dropping one.
	if o.NoGit && o.Push {
		return ErrGitFlagsConflict
	}

	templates, err := o.resolveTemplateSources(p)
	if err != nil {
		return err
	}

	gen := generator.New(p, &generator.Config{
		DryRun:    dryRun,
		Path:      o.Path,
		Overwrite: o.Overwrite,
		GitInit:   !o.NoGit,
		GitPush:   o.Push,
		GitBranch: o.GitBranch,
	}).EnableRealTemplateClone()

	features := resolveFeatures(o.Features)

	host := o.Host
	if host == "" {
		host = hostForBackend(o.GitBackend)
	}

	helpType := o.HelpType
	if helpType == "none" {
		helpType = ""
	}

	return gen.GenerateSkeleton(ctx, generator.SkeletonConfig{
		Name:                o.Name,
		Repo:                o.Repo,
		Host:                host,
		Private:             o.Private,
		Description:         o.Description,
		Path:                o.Path,
		GoVersion:           o.GoVersion,
		Features:            features,
		HelpType:            helpType,
		SlackChannel:        o.SlackChannel,
		SlackTeam:           o.SlackTeam,
		TeamsChannel:        o.TeamsChannel,
		TeamsTeam:           o.TeamsTeam,
		EnvPrefix:           o.EnvPrefix,
		UpdatePolicy:        o.UpdatePolicy,
		UpdateCheckInterval: o.UpdateCheckInterval,
		CIComponentSource:   o.CIComponentSource,
		Signing:             o.resolveSigning(),
		Templates:           templates,
	})
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
		// RequireSignature is never set at generate time: it stays false
		// until a signed release has shipped (flip via `gtb enable signing`).
	})
}
