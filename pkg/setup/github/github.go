package github

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
	"github.com/phpboyscout/go-tool-base/pkg/vcs"
	githubvcs "github.com/phpboyscout/go-tool-base/pkg/vcs/github"
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
	loginFunc func(string) (string, error)
}

// InitialiserOption configures a GitHubInitialiser.
type InitialiserOption func(*GitHubInitialiser)

// WithGHLogin overrides the GitHub CLI login function used for authentication.
// Tests pass a fake; production callers omit to get the default.
func WithGHLogin(fn func(string) (string, error)) InitialiserOption {
	return func(i *GitHubInitialiser) { i.loginFunc = fn }
}

// NewGitHubInitialiser creates a new GitHubInitialiser and mounts its assets.
func NewGitHubInitialiser(p *props.Props, skipLogin, skipKey bool, opts ...InitialiserOption) *GitHubInitialiser {
	if p.Assets != nil {
		p.Assets.Mount(assets, "pkg/setup/github")
	}

	i := &GitHubInitialiser{
		SkipLogin: skipLogin,
		SkipKey:   skipKey,
		loginFunc: githubvcs.GHLogin,
	}

	for _, o := range opts {
		o(i)
	}

	return i
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
	// CI defence: literal-mode writes in a CI environment almost
	// certainly leak the token to build artefacts or logs. The
	// --skip-login default already suppresses this path under CI,
	// but belt-and-braces the invariant here. See R5 in the spec.
	if os.Getenv("CI") == "true" {
		return errors.WithHint(
			errors.New("GitHub literal-token storage is refused under CI"),
			"Set GITHUB_TOKEN via your CI platform's secret injection and add `github.auth.env: GITHUB_TOKEN` to the tool's config.")
	}

	// If the user already has any GitHub credential configured —
	// env-var reference, literal config value (Viper's AutomaticEnv
	// surfaces prefixed env like <TOOL>_GITHUB_AUTH_VALUE through
	// pkg/config's env-aware Sub), or the unprefixed GITHUB_TOKEN
	// ecosystem fallback — don't overwrite with a fresh OAuth token.
	if token := vcs.ResolveToken(cfg.Sub("github"), "GITHUB_TOKEN"); token != "" {
		p.Logger.Info("GitHub credential already configured; skipping OAuth token capture",
			"env_ref", cfg.GetString("github.auth.env"))

		return nil
	}

	p.Logger.Info("Logging into Github", "host", GitHubHost)

	ghtoken, err := g.loginFunc(GitHubHost)
	if err != nil {
		// OAuth commonly fails on headless systems where no browser
		// is available (dev servers, containers, SSH-only hosts).
		// Rather than aborting, print the PAT creation URL and
		// let the user paste a token they've provisioned manually.
		p.Logger.Warn("GitHub OAuth flow unavailable, falling back to manual token entry",
			"reason", err)

		ghtoken, err = promptManualGitHubToken(GitHubHost)
		if err != nil {
			return err
		}
	}

	// Phase 1 minimum: preserve existing literal-write behaviour for
	// users who do not yet have env-var setup. Phase 2 adds the
	// "display once, write env-var reference" flow per the spec.
	cfg.Set("github.auth.value", ghtoken)

	return nil
}

// promptManualGitHubToken is the fallback authentication path used
// when the OAuth device flow cannot complete — typically on headless
// servers where no web browser is available to launch.
//
// The helper prints a URL the user can visit on any device to create
// a personal access token with the scopes required by the OAuth flow
// (repo, read:org, gist), then prompts for the token via a password
// input that does not echo to the terminal. The resulting token is
// indistinguishable from one issued by OAuth and is persisted under
// the same `github.auth.value` config key.
func promptManualGitHubToken(host string) (string, error) {
	url := fmt.Sprintf("https://%s/settings/tokens/new?scopes=repo,read:org,gist&description=gtb-cli", host)

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Open this URL on any device to create a personal access token:")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  "+url)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Required scopes: repo, read:org, gist")
	fmt.Fprintln(os.Stderr)

	var token string

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("GitHub Personal Access Token").
				Description("Paste the token you just generated. Input is hidden.").
				EchoMode(huh.EchoModePassword).
				Value(&token).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("token is required")
					}

					return nil
				}),
		),
	).Run()
	if err != nil {
		return "", errors.Wrap(err, "manual GitHub token prompt cancelled")
	}

	return strings.TrimSpace(token), nil
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
	g := &GitHubInitialiser{loginFunc: githubvcs.GHLogin}

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

	c, err := config.LoadFilesContainer(p.FS, config.WithConfigFiles(targetFile))
	if err != nil {
		// If it doesn't exist, start with defaults
		v := viper.New()
		if err := v.ReadConfig(bytes.NewReader(setup.DefaultConfig)); err != nil {
			return errors.Wrap(err, "failed to read default config")
		}

		c = config.NewContainerFromViper(nil, v)
	}

	if err := RunGitHubInit(p, c); err != nil {
		return err
	}

	// Ensure directory exists
	const dirPerm = 0o755
	if err := p.FS.MkdirAll(dir, dirPerm); err != nil {
		return errors.Wrap(err, "failed to create config directory")
	}

	if err := c.WriteConfigAs(targetFile); err != nil {
		return err
	}

	// Restrict config file permissions — the file may contain auth tokens.
	const configFilePerm = 0o600

	return p.FS.Chmod(targetFile, configFilePerm)
}
