package github

import (
	"bytes"
	"embed"
	"os"
	"path/filepath"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
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
	// also surfaces prefixed env like MYTOOL_GITHUB_AUTH_VALUE here),
	// or the unprefixed GITHUB_TOKEN ecosystem fallback — don't
	// overwrite it with a fresh OAuth-issued literal token.
	//
	// Inlined rather than using vcs.ResolveToken because that helper
	// expects callers to pass cfg.Sub("github") and Viper's Sub()
	// drops AutomaticEnv, hiding prefixed env binding. Top-level
	// cfg.GetString keeps the prefix-aware resolution intact. A
	// follow-up PR should refactor ResolveToken to accept a full
	// path prefix so every caller benefits.
	if hasExistingGitHubCredential(cfg) {
		p.Logger.Info("GitHub credential already configured; skipping OAuth token capture",
			"env_ref", cfg.GetString("github.auth.env"))

		return nil
	}

	p.Logger.Info("Logging into Github", "host", GitHubHost)

	ghtoken, err := g.loginFunc(GitHubHost)
	if err != nil {
		return err
	}

	// Phase 1 minimum: preserve existing literal-write behaviour for
	// users who do not yet have env-var setup. Phase 2 adds the
	// "display once, write env-var reference" flow per the spec.
	cfg.Set("github.auth.value", ghtoken)

	return nil
}

// hasExistingGitHubCredential checks every layer of the credential
// resolution chain for a populated GitHub token. The order mirrors
// [vcs.tokenFromConfig] but uses top-level config keys so Viper's
// AutomaticEnv fires at every step:
//
//  1. github.auth.env → env var whose name is recorded in config
//  2. github.auth.value → literal in YAML, or <PREFIX>_GITHUB_AUTH_VALUE
//     via Viper's prefix-aware env binding
//  3. GITHUB_TOKEN → unprefixed ecosystem fallback
func hasExistingGitHubCredential(cfg config.Containable) bool {
	if name := cfg.GetString("github.auth.env"); name != "" {
		if os.Getenv(name) != "" {
			return true
		}
	}

	if cfg.GetString("github.auth.value") != "" {
		return true
	}

	return os.Getenv("GITHUB_TOKEN") != ""
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
