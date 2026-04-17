package generate

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/internal/generator"
	"github.com/phpboyscout/go-tool-base/pkg/forms"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/utils"
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
	cmd.Flags().StringSliceVarP(&opts.Features, "features", "f", []string{"init", "update", "mcp", "docs", "doctor", "changelog"}, "Features to enable (init, update, mcp, docs, doctor, changelog, ai, config, telemetry)")
	cmd.Flags().StringVar(&opts.GoVersion, "go-version", "", "Go version for go.mod (defaults to the running toolchain version)")
	cmd.Flags().StringVar(&opts.HelpType, "help-type", "none", "Help channel type (slack, teams, or none)")
	cmd.Flags().StringVar(&opts.Overwrite, "overwrite", "ask", "How to handle file conflicts: allow, deny, or ask")
	cmd.Flags().StringVar(&opts.SlackChannel, "slack-channel", "", "Slack channel for help (e.g. #my-team-help)")
	cmd.Flags().StringVar(&opts.SlackTeam, "slack-team", "", "Slack team name (e.g. My Team)")
	cmd.Flags().StringVar(&opts.TeamsChannel, "teams-channel", "", "Microsoft Teams channel for help")
	cmd.Flags().StringVar(&opts.TeamsTeam, "teams-team", "", "Microsoft Teams team name")
	cmd.Flags().StringVar(&opts.EnvPrefix, "env-prefix", "", "Environment variable prefix for config overrides (e.g. MY_APP)")

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

	return o.validateHelpFields()
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
	if org, _, err := splitRepoOrgForValidate(o.Repo); err == nil {
		if verr := generator.ValidateOrg(org, o.GitBackend); verr != nil {
			return verr
		}
	}

	return generator.ValidateEnvPrefix(o.EnvPrefix)
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

// splitRepoOrgForValidate returns the org portion of a repo path
// without requiring the stricter path-containment rules the
// generator applies downstream. Used purely to produce an org
// value for [generator.ValidateOrg].
func splitRepoOrgForValidate(repo string) (org, rest string, err error) {
	i := strings.LastIndex(repo, "/")
	if i <= 0 || i == len(repo)-1 {
		return "", "", errors.Newf("repo %q has no org/name split", repo)
	}

	return repo[:i], repo[i+1:], nil
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
		Run()
}

// resolveFeatures builds the full feature list from the selected set,
// marking unselected defaults as explicitly disabled.
func resolveFeatures(selected []string) []generator.ManifestFeature {
	defaultFeatures := []string{"init", "update", "mcp", "docs", "doctor", "changelog"}

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
		Name:         o.Name,
		Repo:         o.Repo,
		Host:         host,
		Private:      o.Private,
		Description:  o.Description,
		Path:         o.Path,
		GoVersion:    o.GoVersion,
		Features:     features,
		HelpType:     helpType,
		SlackChannel: o.SlackChannel,
		SlackTeam:    o.SlackTeam,
		TeamsChannel: o.TeamsChannel,
		TeamsTeam:    o.TeamsTeam,
		EnvPrefix:    o.EnvPrefix,
	})
}
