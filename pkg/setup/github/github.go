package github

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/credentials"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
	"github.com/phpboyscout/go-tool-base/pkg/vcs"
	githubvcs "github.com/phpboyscout/go-tool-base/pkg/vcs/github"
)

// keychainOpTimeout bounds any single credentials-backend operation
// initiated by the GitHub wizard (Probe, Store). Mirrors pkg/setup/ai
// — the wizard is interactive and synchronous, so a remote-store
// backend that hangs (Vault, SSM) must not stall the user
// indefinitely.
const keychainOpTimeout = 5 * time.Second

// githubKeychainAccount is the account portion of the
// "<service>/<account>" reference used by keychain storage mode.
// The service portion is the tool name so entries are clearly
// labelled in the OS keychain UI.
const githubKeychainAccount = "github.auth"

// envVarNameRe is the permitted shape for the env var name form —
// conservative `^[A-Z][A-Z0-9_]{0,63}$` so the value is a valid POSIX
// env var and fits shell/YAML contexts without quoting.
var envVarNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

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

// GitHubAuthConfig captures the wizard's output from each stage. All
// fields are populated incrementally so test-injected form creators
// can override any subset and the runner still produces a coherent
// config.
type GitHubAuthConfig struct {
	// StorageMode is set by the storage-mode selector. Defaults to
	// [credentials.ModeEnvVar] when the form presents the choice.
	StorageMode credentials.Mode

	// EnvVarName is the env var NAME recorded under github.auth.env
	// in env-var mode. Ignored in keychain/literal modes.
	EnvVarName string

	// FetchToken is true when the user wants the wizard to run OAuth
	// (or the manual fallback) on their behalf. Only relevant in
	// env-var mode — keychain/literal always need a token.
	FetchToken bool

	// Token is the captured token from OAuth / manual entry. Cleared
	// after it has been written (or displayed for env-var mode) so
	// it does not linger in the AuthConfig longer than necessary.
	Token string
}

// AuthFormOption configures form creators for the GitHub auth
// wizard. Used by tests to inject deterministic form-answering
// creators without driving a real TTY.
type AuthFormOption func(*authFormConfig)

type authFormConfig struct {
	storageModeFormCreator func(*GitHubAuthConfig) *huh.Form
	envVarNameFormCreator  func(*GitHubAuthConfig) *huh.Form
	fetchTokenFormCreator  func(*GitHubAuthConfig) *huh.Form
	displayOnceFormCreator func(envVarName, token string) *huh.Form
}

// Form-slot indices used by [WithAuthForm] for the slice-returning
// creator. The display-once form takes a different signature and is
// not indexed here.
const (
	authFormSlotStorageMode = 0
	authFormSlotEnvVarName  = 1
	authFormSlotFetchToken  = 2
)

// WithAuthForm injects custom form creators into [configureAuth] for
// testability. The creator returns forms in order:
//
//	[0] storage-mode selector
//	[1] env-var name input
//	[2] "fetch token now?" confirm
//	[3] display-once token view (takes envVarName, token)
//
// Returning fewer forms is allowed — the runner skips stages whose
// slot is nil or absent. The display-once creator has a different
// signature because it needs the captured token passed in.
func WithAuthForm(
	creator func(*GitHubAuthConfig) []*huh.Form,
	displayOnceCreator func(envVarName, token string) *huh.Form,
) AuthFormOption {
	return func(c *authFormConfig) {
		c.storageModeFormCreator = authFormAtIndex(creator, authFormSlotStorageMode)
		c.envVarNameFormCreator = authFormAtIndex(creator, authFormSlotEnvVarName)
		c.fetchTokenFormCreator = authFormAtIndex(creator, authFormSlotFetchToken)
		c.displayOnceFormCreator = displayOnceCreator
	}
}

func authFormAtIndex(creator func(*GitHubAuthConfig) []*huh.Form, i int) func(*GitHubAuthConfig) *huh.Form {
	return func(cfg *GitHubAuthConfig) *huh.Form {
		forms := creator(cfg)
		if i < len(forms) {
			return forms[i]
		}

		return nil
	}
}

// GitHubInitialiser handles both GitHub authentication and SSH key configuration.
type GitHubInitialiser struct {
	SkipLogin bool
	SkipKey   bool
	loginFunc func(string) (string, error)
	authOpts  []AuthFormOption
}

// InitialiserOption configures a GitHubInitialiser.
type InitialiserOption func(*GitHubInitialiser)

// WithGHLogin overrides the GitHub CLI login function used for authentication.
// Tests pass a fake; production callers omit to get the default.
func WithGHLogin(fn func(string) (string, error)) InitialiserOption {
	return func(i *GitHubInitialiser) { i.loginFunc = fn }
}

// WithGitHubAuthForms propagates [AuthFormOption]s down into
// configureAuth. Tests use this to inject deterministic form
// creators via [WithAuthForm].
func WithGitHubAuthForms(opts ...AuthFormOption) InitialiserOption {
	return func(i *GitHubInitialiser) { i.authOpts = append(i.authOpts, opts...) }
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
		cfg.GetString("github.auth.keychain") != "" ||
		(authEnv != "" && os.Getenv(authEnv) != "")

	sshConfigured := g.SkipKey ||
		cfg.GetString("github.ssh.key.path") != "" ||
		cfg.GetString("github.ssh.key.type") == "agent"

	return loginConfigured && sshConfigured
}

// Configure runs the interactive login and/or SSH configuration.
func (g *GitHubInitialiser) Configure(props *props.Props, cfg config.Containable) error {
	if !g.SkipLogin && !hasAnyGitHubCredential(cfg) {
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

// hasAnyGitHubCredential reports whether the config already records a
// credential under any of the three storage modes. Used to
// short-circuit the auth wizard when the user re-runs `init`.
func hasAnyGitHubCredential(cfg config.Containable) bool {
	return cfg.GetString("github.auth.value") != "" ||
		cfg.GetString("github.auth.keychain") != "" ||
		cfg.GetString("github.auth.env") != ""
}

func (g *GitHubInitialiser) configureAuth(p *props.Props, cfg config.Containable) error {
	// CI defence: literal-mode writes in a CI environment almost
	// certainly leak the token to build artefacts or logs. The
	// --skip-login default already suppresses this path under CI,
	// but belt-and-braces the invariant here. See R5 in the spec.
	if isCI() {
		return errors.WithHint(
			errors.New("GitHub literal-token storage is refused under CI"),
			"Set GITHUB_TOKEN via your CI platform's secret injection and add `github.auth.env: GITHUB_TOKEN` to the tool's config.")
	}

	// If the user already has any GitHub credential configured —
	// env-var reference, literal config value (Viper's AutomaticEnv
	// surfaces prefixed env like <TOOL>_GITHUB_AUTH_VALUE through
	// pkg/config's env-aware Sub), or the unprefixed GITHUB_TOKEN
	// ecosystem fallback — don't overwrite with a fresh OAuth token.
	ctx, cancel := context.WithTimeout(context.Background(), keychainOpTimeout)
	defer cancel()

	if token := vcs.ResolveTokenContext(ctx, cfg.Sub("github"), "GITHUB_TOKEN"); token != "" {
		p.Logger.Info("GitHub credential already configured; skipping OAuth token capture",
			"env_ref", cfg.GetString("github.auth.env"))

		return nil
	}

	authCfg := &GitHubAuthConfig{}
	fCfg := newAuthFormConfig(g.authOpts...)

	if err := runAuthFormStage(fCfg.storageModeFormCreator, authCfg); err != nil {
		return err
	}

	// CI belt-and-braces: refuse literal even if a test-injected form
	// creator bypassed the selector.
	if authCfg.StorageMode == credentials.ModeLiteral && isCI() {
		return errors.WithHint(
			errors.New("literal credential storage is refused under CI"),
			"CI environments must use platform-injected secrets referenced via env-var mode.")
	}

	return g.runAuthCredentialStage(ctx, p, cfg, fCfg, authCfg)
}

// runAuthCredentialStage dispatches to the per-mode branch and
// writes the resulting config. Split from [configureAuth] so the
// outer function stays under the cyclomatic-complexity budget.
func (g *GitHubInitialiser) runAuthCredentialStage(
	ctx context.Context,
	p *props.Props,
	cfg config.Containable,
	fCfg *authFormConfig,
	authCfg *GitHubAuthConfig,
) error {
	var err error

	switch authCfg.StorageMode {
	case credentials.ModeEnvVar:
		err = g.runEnvVarAuth(p, fCfg, authCfg)
	case credentials.ModeKeychain, credentials.ModeLiteral, "":
		authCfg.Token, err = g.captureToken(p)
	default:
		err = errors.Newf("unsupported credential storage mode %q", authCfg.StorageMode)
	}

	if err != nil {
		return err
	}

	if writeErr := writeGitHubCredential(ctx, cfg, p.Tool.Name, authCfg); writeErr != nil {
		return writeErr
	}

	// Best-effort zero. Go strings are immutable so this only clears
	// the last reference in the AuthConfig — the GC reclaims the
	// backing memory when it's ready. See docs/development/
	// security-decisions.md § M-4.
	authCfg.Token = ""

	return nil
}

// runEnvVarAuth drives the env-var branch: prompts for the env-var
// name, then (optionally) runs OAuth and displays the token once for
// the user to copy into their shell profile.
func (g *GitHubInitialiser) runEnvVarAuth(p *props.Props, fCfg *authFormConfig, authCfg *GitHubAuthConfig) error {
	if err := runAuthFormStage(fCfg.envVarNameFormCreator, authCfg); err != nil {
		return err
	}

	if err := runAuthFormStage(fCfg.fetchTokenFormCreator, authCfg); err != nil {
		return err
	}

	if !authCfg.FetchToken {
		// User already has a token via other means (shell profile,
		// 1Password CLI, etc.). We write only the env-var reference.
		return nil
	}

	token, err := g.captureToken(p)
	if err != nil {
		return err
	}

	displayForm := fCfg.displayOnceFormCreator(authCfg.EnvVarName, token)
	if displayForm != nil {
		if err := displayForm.Run(); err != nil {
			return errors.Wrap(err, "GitHub token display cancelled")
		}
	}

	// Token is not written to config in env-var mode. The user has
	// seen it and acknowledged saving it. We clear our local copy
	// best-effort (string immutability; see M-4).
	token = ""
	_ = token

	return nil
}

// captureToken runs OAuth, falling back to manual entry if the OAuth
// device flow can't launch a browser.
func (g *GitHubInitialiser) captureToken(p *props.Props) (string, error) {
	p.Logger.Info("Logging into Github", "host", GitHubHost)

	token, err := g.loginFunc(GitHubHost)
	if err == nil {
		return token, nil
	}

	p.Logger.Warn("GitHub OAuth flow unavailable, falling back to manual token entry",
		"reason", err)

	return promptManualGitHubToken(GitHubHost)
}

// writeGitHubCredential routes to the correct config-write path for
// the captured storage mode. Token in authCfg is the captured OAuth
// or manual value (populated for keychain / literal modes; cleared
// for env-var mode after the display-once flow).
func writeGitHubCredential(ctx context.Context, cfg config.Containable, toolName string, authCfg *GitHubAuthConfig) error {
	switch authCfg.StorageMode {
	case credentials.ModeEnvVar:
		if authCfg.EnvVarName != "" {
			cfg.Set("github.auth.env", authCfg.EnvVarName)
		}

		return nil

	case credentials.ModeKeychain:
		if authCfg.Token == "" {
			return errors.New("no GitHub token captured for keychain mode")
		}

		if toolName == "" {
			return errors.New("cannot write keychain entry without a tool name")
		}

		if err := credentials.Store(ctx, toolName, githubKeychainAccount, authCfg.Token); err != nil {
			return errors.WithHint(
				errors.Wrap(err, "storing GitHub token in OS keychain"),
				"If the keychain is locked, unlock it and re-run; otherwise pick env-var or literal mode instead.")
		}

		cfg.Set("github.auth.keychain", toolName+"/"+githubKeychainAccount)

		return nil

	case credentials.ModeLiteral, "":
		if authCfg.Token != "" {
			cfg.Set("github.auth.value", authCfg.Token)
		}

		return nil

	default:
		return errors.Newf("unsupported credential storage mode %q", authCfg.StorageMode)
	}
}

// defaultStorageModeForm presents the three-mode selector. Literal
// mode is hidden when the process runs under CI=true; keychain is
// hidden unless [credentials.Probe] succeeds against the registered
// backend.
func defaultStorageModeForm(cfg *GitHubAuthConfig) *huh.Form {
	ctx, cancel := context.WithTimeout(context.Background(), keychainOpTimeout)
	defer cancel()

	options := githubStorageModeOptions(isCI(), credentials.Probe(ctx))

	if cfg.StorageMode == "" {
		cfg.StorageMode = credentials.ModeEnvVar
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[credentials.Mode]().
				Title("GitHub Credential Storage").
				Description(githubStorageModeDescription(isCI())).
				Options(options...).
				Value(&cfg.StorageMode),
		),
	)
}

// githubStorageModeOptions filters the mode selector by CI state and
// keychain probe result. Parallel to pkg/setup/ai; kept local to
// avoid a circular dependency.
func githubStorageModeOptions(ci, keychainUsable bool) []huh.Option[credentials.Mode] {
	opts := []huh.Option[credentials.Mode]{
		huh.NewOption("Environment variable reference (recommended)", credentials.ModeEnvVar),
	}

	if keychainUsable {
		opts = append(opts, huh.NewOption("OS keychain", credentials.ModeKeychain))
	}

	if !ci {
		opts = append(opts, huh.NewOption("Literal value in config file (plaintext)", credentials.ModeLiteral))
	}

	return opts
}

func githubStorageModeDescription(ci bool) string {
	if ci {
		return "CI environment detected — only environment variable references are permitted."
	}

	return "Env-var reference is recommended; keychain keeps the token out of config; literal writes the token to config as plaintext."
}

// defaultEnvVarNameForm prompts for the env var name, defaulting to
// `GITHUB_TOKEN` (the ecosystem standard the `gh` CLI and most CI
// platforms already use).
func defaultEnvVarNameForm(cfg *GitHubAuthConfig) *huh.Form {
	if cfg.EnvVarName == "" {
		cfg.EnvVarName = "GITHUB_TOKEN"
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Environment Variable Name").
				Description("Name of the env var that will hold your GitHub token. " +
					"`GITHUB_TOKEN` is the upstream standard; override if you run " +
					"multiple tools with conflicting tokens.").
				Placeholder("GITHUB_TOKEN").
				Value(&cfg.EnvVarName).
				Validate(validateEnvVarName),
		),
	)
}

// defaultFetchTokenForm asks whether the user wants the wizard to
// run OAuth and display the token once. Default is yes, matching
// the spec's "most first-time users want the convenience of OAuth
// without the storage risk" design.
func defaultFetchTokenForm(cfg *GitHubAuthConfig) *huh.Form {
	cfg.FetchToken = true

	return huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Fetch a GitHub token now?").
				Description("Select Yes to run OAuth and have the wizard display the token once for you to copy into your shell profile. " +
					"Select No if you already have a token (e.g. from 1Password, a password manager, or manual PAT creation).").
				Affirmative("Yes, run OAuth").
				Negative("No, I already have one").
				Value(&cfg.FetchToken),
		),
	)
}

// defaultDisplayOnceForm shows the captured token inside a
// non-editable input, requires the user to submit the form (the
// acknowledgement), and then the caller is responsible for zeroing
// the token. The token is exposed briefly on screen by design — the
// user explicitly opted in, and the prompt instructs them to copy
// the value before confirming.
func defaultDisplayOnceForm(envVarName, token string) *huh.Form {
	title := fmt.Sprintf("Save the GitHub token to %s", envVarName)
	description := fmt.Sprintf(
		"Copy the token below, add it to your shell profile as:\n\n"+
			"    export %s=%s\n\n"+
			"This prompt will not be shown again. The token is NOT written to the config file.",
		envVarName, token)

	var acknowledged bool

	return huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title(title).
				Description(description),
			huh.NewConfirm().
				Title("Have you saved the token?").
				Affirmative("Yes, continue").
				Negative("No, cancel").
				Value(&acknowledged),
		),
	)
}

// newAuthFormConfig constructs the default form creators and applies
// caller-supplied overrides (typically from tests).
func newAuthFormConfig(opts ...AuthFormOption) *authFormConfig {
	c := &authFormConfig{
		storageModeFormCreator: defaultStorageModeForm,
		envVarNameFormCreator:  defaultEnvVarNameForm,
		fetchTokenFormCreator:  defaultFetchTokenForm,
		displayOnceFormCreator: defaultDisplayOnceForm,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// runAuthFormStage runs a single form stage, wrapping any error in a
// cancellation message.
func runAuthFormStage(creator func(*GitHubAuthConfig) *huh.Form, cfg *GitHubAuthConfig) error {
	if creator == nil {
		return nil
	}

	form := creator(cfg)
	if form == nil {
		return nil
	}

	if err := form.Run(); err != nil {
		return errors.Wrap(err, "GitHub auth form cancelled")
	}

	return nil
}

// validateEnvVarName enforces `^[A-Z][A-Z0-9_]{0,63}$` so the name is
// a valid POSIX env var and fits downstream shell/YAML contexts.
func validateEnvVarName(name string) error {
	if name == "" {
		return errors.New("env var name is required")
	}

	if !envVarNameRe.MatchString(name) {
		return errors.New("env var name must match ^[A-Z][A-Z0-9_]{0,63}$")
	}

	return nil
}

// isCI reports whether the process appears to be running under a CI
// system.
func isCI() bool {
	return os.Getenv("CI") == "true"
}

// promptManualGitHubToken is the fallback authentication path used
// when the OAuth device flow cannot complete — typically on headless
// servers where no web browser is available to launch.
//
// The helper prints a URL the user can visit on any device to create
// a personal access token with the scopes required by the OAuth flow
// (repo, read:org, gist), then prompts for the token via a password
// input that does not echo to the terminal. The resulting token is
// indistinguishable from one issued by OAuth and is returned to the
// caller for mode-specific persistence.
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
		Long:  `Configures the GitHub token for API access via the three-mode selector (env-var reference, OS keychain, or literal), and generates or selects an SSH key for Git operations.`,
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
