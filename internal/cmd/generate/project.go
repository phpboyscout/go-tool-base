package generate

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/gtb/internal/generator"
	"github.com/phpboyscout/gtb/pkg/forms"
	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/phpboyscout/gtb/pkg/utils"
)

type SkeletonOptions struct {
	Name         string
	GitBackend   string
	Repo         string
	Host         string
	Description  string
	Path         string
	GoVersion    string
	Features     []string
	HelpType     string
	SlackChannel string
	SlackTeam    string
	TeamsChannel string
	TeamsTeam    string
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
	cmd.Flags().StringVarP(&opts.Description, "description", "d", "A tool built with gtb", "Project description")
	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Destination path")
	cmd.Flags().StringSliceVarP(&opts.Features, "features", "f", []string{"init", "update", "mcp", "docs"}, "Features to enable (init, update, mcp, docs)")
	cmd.Flags().StringVar(&opts.GoVersion, "go-version", "", "Go version for go.mod (defaults to the running toolchain version)")
	cmd.Flags().StringVar(&opts.HelpType, "help-type", "none", "Help channel type (slack, teams, or none)")
	cmd.Flags().StringVar(&opts.SlackChannel, "slack-channel", "", "Slack channel for help (e.g. #my-team-help)")
	cmd.Flags().StringVar(&opts.SlackTeam, "slack-team", "", "Slack team name (e.g. My Team)")
	cmd.Flags().StringVar(&opts.TeamsChannel, "teams-channel", "", "Microsoft Teams channel for help")
	cmd.Flags().StringVar(&opts.TeamsTeam, "teams-team", "", "Microsoft Teams team name")

	return cmd
}

func (o *SkeletonOptions) ValidateOrPrompt(p *props.Props) error {
	if o.Name != "" && o.Repo != "" {
		return nil
	}

	if !utils.IsInteractive() {
		return ErrNonInteractive
	}

	return o.runWizard()
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
				huh.NewOption("Initialization", "init"),
				huh.NewOption("Self-Update", "update"),
				huh.NewOption("MCP Server", "mcp"),
				huh.NewOption("Documentation", "docs"),
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

func (o *SkeletonOptions) Run(ctx context.Context, p *props.Props) error {
	gen := generator.New(p, &generator.Config{
		Path: o.Path,
	})

	features := make([]generator.ManifestFeature, 0, len(o.Features))
	for _, f := range o.Features {
		features = append(features, generator.ManifestFeature{
			Name:    f,
			Enabled: true,
		})
	}

	// Also add explicitly disabled ones if they are default features but not in the list
	defaultFeatures := []string{"init", "update", "mcp", "docs"}

	selectedMap := make(map[string]bool)
	for _, f := range o.Features {
		selectedMap[f] = true
	}

	for _, f := range defaultFeatures {
		if !selectedMap[f] {
			features = append(features, generator.ManifestFeature{
				Name:    f,
				Enabled: false,
			})
		}
	}

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
		Description:  o.Description,
		Path:         o.Path,
		GoVersion:    o.GoVersion,
		Features:     features,
		HelpType:     helpType,
		SlackChannel: o.SlackChannel,
		SlackTeam:    o.SlackTeam,
		TeamsChannel: o.TeamsChannel,
		TeamsTeam:    o.TeamsTeam,
	})
}
