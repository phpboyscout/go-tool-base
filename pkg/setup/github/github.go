package github

import (
	"bytes"
	"embed"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/phpboyscout/gtb/pkg/setup"
)

var (
	skipLogin bool
	skipKey   bool
)

func init() {
	setup.Register("github",
		[]setup.InitialiserProvider{
			func(p *props.Props) setup.Initialiser {
				if skipLogin && skipKey {
					return nil
				}

				return NewGitHubInitialiser(p, skipLogin, skipKey)
			},
		},
		[]setup.SubcommandProvider{
			func(p *props.Props) []*cobra.Command {
				return []*cobra.Command{NewCmdInitGitHub(p)}
			},
		},
		[]setup.FeatureFlag{
			func(cmd *cobra.Command) {
				is_ci := (os.Getenv("CI") == "true")
				cmd.Flags().BoolVarP(&skipLogin, "skip-login", "l", is_ci, "skip the login to github")
				cmd.Flags().BoolVarP(&skipKey, "skip-key", "k", is_ci, "skip configuring ssh key")
			},
		},
	)
}

//go:embed assets/*
var assets embed.FS

// GitHubInitialiser handles both GitHub authentication and SSH key configuration.
type GitHubInitialiser struct {
	SkipLogin bool
	SkipKey   bool
}

// NewGitHubInitialiser creates a new GitHubInitialiser and mounts its assets.
func NewGitHubInitialiser(p *props.Props, skipLogin, skipKey bool) *GitHubInitialiser {
	if p.Assets != nil {
		p.Assets.Mount(assets, "pkg/setup/github")
	}

	return &GitHubInitialiser{
		SkipLogin: skipLogin,
		SkipKey:   skipKey,
	}
}

func (g *GitHubInitialiser) Name() string {
	return "GitHub integration"
}

// IsConfigured returns true if unskipped components are already present in the config.
func (g *GitHubInitialiser) IsConfigured(cfg config.Containable) bool {
	authEnv := cfg.GetString("github.auth.env")
	loginConfigured := g.SkipLogin ||
		cfg.GetString("github.auth.value") != "" ||
		(authEnv != "" && os.Getenv(authEnv) != "")

	sshConfigured := g.SkipKey ||
		cfg.GetString("github.ssh.key.path") != "" ||
		cfg.GetString("github.ssh.key.type") == "agent"

	return loginConfigured && sshConfigured
}

// Configure runs the interactive login and/or SSH configuration.
func (g *GitHubInitialiser) Configure(props *props.Props, cfg config.Containable) error {
	if !g.SkipLogin && cfg.GetString("github.auth.value") == "" {
		if err := g.configureAuth(props, cfg); err != nil {
			return err
		}
	}

	if !g.SkipKey && cfg.GetString("github.ssh.key.path") == "" && cfg.GetString("github.ssh.key.type") != "agent" {
		if err := g.configureSSH(props, cfg); err != nil {
			return err
		}
	}

	return nil
}

func (g *GitHubInitialiser) configureAuth(p *props.Props, cfg config.Containable) error {
	p.Logger.Info("Logging into Github", "host", GitHubHost)

	ghtoken, err := ghLoginFunc(GitHubHost)
	if err != nil {
		return err
	}

	cfg.Set("github.auth.value", ghtoken)

	return nil
}

func (g *GitHubInitialiser) configureSSH(p *props.Props, cfg config.Containable) error {
	keyType, keyPath, err := ConfigureSSHKey(p, cfg)
	if err != nil {
		return err
	}

	cfg.Set("github.ssh.key.type", keyType)
	cfg.Set("github.ssh.key.path", keyPath)

	return nil
}

// RunGitHubInit forcibly runs both login and SSH configuration regardless of current state.
// This is used by the explicit `init github` command.
func RunGitHubInit(p *props.Props, cfg config.Containable) error {
	g := &GitHubInitialiser{}

	if err := g.configureAuth(p, cfg); err != nil {
		return err
	}

	return g.configureSSH(p, cfg)
}

// NewCmdInitGitHub creates the `init github` subcommand.
func NewCmdInitGitHub(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github",
		Short: "Configure GitHub authentication and SSH keys",
		Long:  `Configures the classic token for GitHub API access and generates or selects an SSH key for Git operations.`,
		Run: func(cmd *cobra.Command, _ []string) {
			dir, _ := cmd.Flags().GetString("dir")

			if err := RunInitCmd(p, dir); err != nil {
				p.Logger.Fatalf("Failed to configure GitHub: %s", err)
			}

			p.Logger.Info("GitHub configuration saved successfully")
		},
	}

	cmd.Flags().String("dir", setup.GetDefaultConfigDir(p.FS, p.Tool.Name), "directory containing the config file")

	return cmd
}

// RunInitCmd executes the GitHub configuration and writes the results to the config file.
func RunInitCmd(p *props.Props, dir string) error {
	targetFile := filepath.Join(dir, setup.DefaultConfigFilename)

	c, err := config.LoadFilesContainer(nil, p.FS, targetFile)
	if err != nil {
		// If it doesn't exist, start with defaults
		v := viper.New()
		if err := v.ReadConfig(bytes.NewReader(setup.DefaultConfig)); err != nil {
			return err
		}

		c = config.NewContainerFromViper(nil, v)
	}

	if err := RunGitHubInit(p, c); err != nil {
		return err
	}

	// Ensure directory exists
	const dirPerm = 0o755
	if err := p.FS.MkdirAll(dir, dirPerm); err != nil {
		return err
	}

	return c.WriteConfigAs(targetFile)
}
