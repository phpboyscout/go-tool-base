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

	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/forms"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/signing"
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

	// CIComponentSource overrides the phpboyscout/cicd include base in the
	// scaffolded GitLab pipeline (GitLab backend only). Empty uses the
	// framework default, gitlab.com/phpboyscout/cicd.
	CIComponentSource string

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
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.ValidateOrPrompt(p); err != nil {
				return err
			}

			return opts.Run(cmd.Context(), p)
		},
	}

	cmd.Flags().StringVarP(&opts.Name, "name", "n", "", "Project name (e.g. als)")
	cmd.Flags().StringVarP(&opts.Repo, "repo", "r", "", "Repository in org/repo format")
	cmd.Flags().StringVar(&opts.GitBackend, "git-backend", "github", "Git backend (github or gitlab)")
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
	cmd.Flags().StringVar(&opts.CIComponentSource, "ci-component-source", "", "Override the phpboyscout/cicd component include base in the scaffolded GitLab pipeline (default gitlab.com/phpboyscout/cicd)")
	cmd.Flags().BoolVar(&opts.Signing, "signing", false, "Enable consumer-side release-signing verification (scaffolds internal/trustkeys and wires props.Signing)")
	cmd.Flags().StringVar(&opts.SigningEmail, "signing-email", "", "Release WKD email for signing (external_key_email); implies --signing")
	cmd.Flags().StringVar(&opts.SigningKeySource, "signing-key-source", "both", "Signing trust-anchor source: embedded, external, or both")
	cmd.Flags().BoolVar(&opts.SigningRequireExternalCrosscheck, "signing-require-external-crosscheck", false, "Fail signing closed when the external (WKD) resolver is unreachable")
	cmd.Flags().StringVar(&opts.SigningKeyID, "signing-key-id", "", "Signing key id/ARN/alias (or PEM path for local) the release pipeline signs with; wires the GoReleaser signs block")
	cmd.Flags().StringVar(&opts.SigningBackend, "signing-backend", "", "gtb sign backend for the release pipeline (default aws-kms when --signing-key-id is set)")
	cmd.Flags().StringVar(&opts.SigningKMSRegion, "signing-kms-region", "", "AWS region for the aws-kms backend (default eu-west-2)")
	cmd.Flags().StringVar(&opts.SigningPublicKey, "signing-public-key", "", "Path to the embedded public key the signature identifies (default internal/trustkeys/keys/signing-key-v1.asc)")

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

	return o.validateFields()
}

// validateFields applies the structural validation rules from
// internal/generator/validate.go to every user-influenced field.
// Runs after both wizard and flag-driven flows so neither path
// can smuggle adversarial input into template rendering.
// See docs/development/specs/2026-04-02-generator-template-escaping.md
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

	if err := generator.ValidateEnvPrefix(o.EnvPrefix); err != nil {
		return err
	}

	return generator.ValidateCIComponentSource(o.CIComponentSource)
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
	const (
		minRepoSegments    = 2
		hostQualifiedCount = 3
	)

	segments := strings.Split(repo, "/")
	if len(segments) < minRepoSegments {
		return "", errors.Newf("repo %q must be org/name or host/org/name", repo)
	}

	if len(segments) < hostQualifiedCount {
		// Two segments: org/name, with no host prefix to strip.
		return segments[0], nil
	}

	return strings.Join(segments[1:len(segments)-1], "/"), nil
}

func (o *SkeletonOptions) defaultHost() string {
	if o.GitBackend == "gitlab" {
		return "gitlab.com"
	}

	return "github.com"
}

func (o *SkeletonOptions) runWizard() error {
	// Stage 1: project basics + backend/help type selections
	stage1 := huh.NewGroup(
		huh.NewInput().
			Title("Project Name").
			Value(&o.Name).
			Validate(func(s string) error {
				if s == "" {
					return ErrNameRequired
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
			Options(
				huh.NewOption("GitHub", "github"),
				huh.NewOption("GitLab", "gitlab"),
			).
			Value(&o.GitBackend),
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

	return forms.NewWizard(stage1).
		Step(o.runEnvPrefixStep).
		// Stage 2: git config — built dynamically so the description reflects the chosen backend
		Step(func() error {
			if o.Host == "" {
				o.Host = o.defaultHost()
			}

			backendLabel := "GitHub"
			repoDesc := "The repository path in org/repo format."
			repoPlaceholder := "org/repo"

			if o.GitBackend == "gitlab" {
				backendLabel = "GitLab"
				repoDesc = "The repository path. GitLab supports nested groups — use the full path and the last segment will be treated as the repository name (e.g. group/subgroup/repo)."
				repoPlaceholder = "group/subgroup/repo"
			}

			stage2 := huh.NewGroup(
				huh.NewInput().
					Title("Git Host").
					Description(fmt.Sprintf("The %s host. Change this only if you use a self-hosted instance.", backendLabel)).
					Value(&o.Host).
					Validate(func(s string) error {
						if s == "" {
							return ErrHostRequired
						}

						return nil
					}),
				huh.NewInput().
					Title("Repository").
					Description(repoDesc).
					Placeholder(repoPlaceholder).
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
				Title(fmt.Sprintf("%s Repository", backendLabel)).
				Description(fmt.Sprintf("Configure the %s repository that will host your new tool.\n", backendLabel))

			return forms.NewNavigable(stage2).Run()
		}).
		// Stage 3: help config — built dynamically based on the chosen help type
		Step(func() error {
			switch o.HelpType {
			case "slack":
				stage3 := huh.NewGroup(
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
					Description("These values appear in error messages to direct users to support.\n")

				return forms.NewNavigable(stage3).Run()
			case "teams":
				stage3 := huh.NewGroup(
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
					Description("These values appear in error messages to direct users to support.\n")

				return forms.NewNavigable(stage3).Run()
			default:
				return nil
			}
		}).
		// Stage 4: release signing — off by default. When enabled, collect
		// the WKD email and key source. require_signature is never prompted.
		Step(o.runSigningStep).
		Run()
}

// runSigningStep asks whether to enable release signing (default No) and,
// when enabled, collects the WKD email and key source. require_signature
// is deliberately never prompted — it stays false until a signed release
// has shipped and is only flipped later via `gtb enable signing`.
func (o *SkeletonOptions) runSigningStep() error {
	if o.SigningKeySource == "" {
		o.SigningKeySource = "both"
	}

	enableGroup := huh.NewGroup(
		huh.NewConfirm().
			Title("Enable release signing?").
			Description("Sets up consumer-side self-update signature verification. Needs a signing key and a published WKD endpoint — leave off unless you have them.").
			Affirmative("Yes").
			Negative("No").
			Value(&o.Signing),
	).
		Title("Release Signing").
		Description("Verify self-update downloads against an embedded release key.\n")

	if err := forms.NewNavigable(enableGroup).Run(); err != nil {
		return err
	}

	if !o.Signing {
		return nil
	}

	detailGroup := huh.NewGroup(
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
		Description("These values are written to the manifest signing block.\n")

	return forms.NewNavigable(detailGroup).Run()
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

// runEnvPrefixStep presents the env prefix wizard step, defaulting to the
// upper-cased tool name with hyphens replaced by underscores.
func (o *SkeletonOptions) runEnvPrefixStep() error {
	if o.EnvPrefix == "" {
		o.EnvPrefix = strings.ToUpper(strings.ReplaceAll(o.Name, "-", "_"))
	}

	envPrefixGroup := huh.NewGroup(
		huh.NewInput().
			Title("Environment Variable Prefix").
			Description("Prefix for config env var overrides (e.g. MY_APP → MY_APP_LOG_LEVEL). Leave empty to disable.").
			Placeholder(o.EnvPrefix).
			Value(&o.EnvPrefix).
			Validate(func(s string) error {
				if s == "" {
					return nil // opt-out
				}

				if !regexp.MustCompile(`^[A-Z0-9_]+$`).MatchString(s) {
					return ErrEnvPrefixInvalid
				}

				return nil
			}),
	).
		Title("Environment Variable Prefix").
		Description("Scopes config env var lookups so only variables starting with this prefix are considered.\n")

	return forms.NewNavigable(envPrefixGroup).Run()
}

func (o *SkeletonOptions) Run(ctx context.Context, p *props.Props) error {
	if o.Overwrite == "" {
		o.Overwrite = "ask"
	}

	if o.Overwrite != "allow" && o.Overwrite != "deny" && o.Overwrite != "ask" {
		return errors.Wrapf(ErrInvalidOverwriteValue, "%q", o.Overwrite)
	}

	gen := generator.New(p, &generator.Config{
		DryRun:    dryRun,
		Path:      o.Path,
		Overwrite: o.Overwrite,
	})

	features := resolveFeatures(o.Features)

	host := o.Host
	if host == "" {
		host = o.defaultHost()
	}

	helpType := o.HelpType
	if helpType == "none" {
		helpType = ""
	}

	return gen.GenerateSkeleton(ctx, generator.SkeletonConfig{
		Name:              o.Name,
		Repo:              o.Repo,
		Host:              host,
		Private:           o.Private,
		Description:       o.Description,
		Path:              o.Path,
		GoVersion:         o.GoVersion,
		Features:          features,
		HelpType:          helpType,
		SlackChannel:      o.SlackChannel,
		SlackTeam:         o.SlackTeam,
		TeamsChannel:      o.TeamsChannel,
		TeamsTeam:         o.TeamsTeam,
		EnvPrefix:         o.EnvPrefix,
		CIComponentSource: o.CIComponentSource,
		Signing:           o.resolveSigning(),
	})
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
