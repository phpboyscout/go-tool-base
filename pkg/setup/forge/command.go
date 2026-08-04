package forge

import (
	"context"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// RunGitHubInit forcibly runs both login and SSH configuration regardless of
// current state. This is used by the explicit `init github` command. ctx is
// the command context: it carries no deadline of its own so the OAuth device
// flow can run at human pace, while Ctrl-C aborts an in-flight poll.
func RunGitHubInit(ctx context.Context, p *props.Props, cfg setup.Editor) error {
	g := New(p, gitHubProfile)

	if err := g.configureAuth(ctx, p, cfg); err != nil {
		return err
	}

	return g.configureSSH(p, cfg) //nolint:contextcheck // SSH stage deliberately ctx-free; upload bounds itself, plumbing tracked in the forge-repo-setup follow-ups spec
}

// NewCmdInitGitHub creates the `init github` subcommand.
func NewCmdInitGitHub(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github",
		Short: "Configure GitHub authentication and SSH keys",
		Long: `Configure the GitHub token for API access via the three-mode selector
(env-var reference, OS keychain, or literal), and generate or select an SSH key
for Git operations.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, _ := cmd.Flags().GetString("dir")

			if err := RunGitHubInitCmd(cmd.Context(), p, dir); err != nil {
				return errors.Wrap(err, "failed to configure GitHub")
			}

			p.Logger.Info("GitHub configuration saved successfully")

			return nil
		},
	}

	cmd.Flags().String("dir", setup.GetDefaultConfigDir(p.FS, p.Tool.Name), "directory containing the config file")

	return cmd
}

// RunGitHubInitCmd executes the GitHub configuration and writes the results to
// the config file, which is seeded from the merged init template when absent.
func RunGitHubInitCmd(ctx context.Context, p *props.Props, dir string) error {
	editor, _, err := setup.OpenConfigEditor(ctx, p, dir, false)
	if err != nil {
		return err
	}

	return RunGitHubInit(ctx, p, editor)
}

// RunForgeInit runs a single-token forge's wizard against an existing config
// container. ctx is the command context: it carries no deadline of its own so
// a device-flow login can run at human pace, while Ctrl-C aborts an in-flight
// poll.
func RunForgeInit(ctx context.Context, p *props.Props, cfg setup.Editor, profile Profile) error {
	return New(p, profile).Configure(ctx, p, cfg)
}

// NewCmdInitForge creates the `init <forge>` subcommand for a single-token
// forge, deriving its wording from the profile so a new forge contributes a
// command by existing rather than by adding a builder.
func NewCmdInitForge(p *props.Props, profile Profile) *cobra.Command {
	long := "Configure the " + profile.Label + ` token for API access via the three-mode
selector (env-var reference, OS keychain, or literal)`

	if profile.OffersSSH {
		long += ", and generate or select\nan SSH key for Git operations"
	}

	long += "."

	if !profile.OffersLogin {
		long += "\n\n" + profile.Label + ` does not support an interactive browser login, so the token is
entered by hand.`
	}

	cmd := &cobra.Command{
		Use:   profile.ConfigPrefix,
		Short: "Configure " + profile.Label + " authentication" + sshShortSuffix(profile),
		Long:  long,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, _ := cmd.Flags().GetString("dir")

			if err := RunForgeInitCmd(cmd.Context(), p, dir, profile); err != nil {
				return errors.Wrap(err, "failed to configure "+profile.Label)
			}

			p.Logger.Info(profile.Label + " configuration saved successfully")

			return nil
		},
	}

	cmd.Flags().String("dir", setup.GetDefaultConfigDir(p.FS, p.Tool.Name), "directory containing the config file")

	return cmd
}

func sshShortSuffix(profile Profile) string {
	if profile.OffersSSH {
		return " and SSH keys"
	}

	return ""
}

// RunForgeInitCmd executes a single-token forge's configuration and writes the
// results to the config file, which is seeded from the merged init template
// when absent.
func RunForgeInitCmd(ctx context.Context, p *props.Props, dir string, profile Profile) error {
	editor, _, err := setup.OpenConfigEditor(ctx, p, dir, false)
	if err != nil {
		return err
	}

	return RunForgeInit(ctx, p, editor, profile)
}

// RunBitbucketInit executes the wizard against an existing config container,
// typically invoked by [NewCmdInitBitbucket]. Optional [DualFormOption]s are
// propagated into the wizard so tests can inject deterministic form creators,
// mirroring [pkg/setup/ai.RunAIInit].
func RunBitbucketInit(ctx context.Context, p *props.Props, cfg setup.Editor, opts ...DualFormOption) error {
	i := NewBitbucketInitialiser(p, WithDualForms(opts...))

	return i.Configure(ctx, p, cfg)
}

// NewCmdInitBitbucket creates the `init bitbucket` subcommand.
func NewCmdInitBitbucket(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bitbucket",
		Short: "Configure Bitbucket authentication (username + app password)" + sshShortSuffix(bitbucketProfile),
		Long: `Configure Bitbucket credentials via the three-mode selector: environment
variable references (recommended default), OS keychain (single JSON blob), or
literal values in the config file.

Bitbucket's dual-credential model (username + app_password) is handled natively:
env-var mode records two env-var names, keychain mode stores a single JSON blob,
and literal mode writes both fields to config.

An SSH key for Git operations is configured afterwards, and uploaded using the
credentials just captured. Pass --skip-key to leave keys alone.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, _ := cmd.Flags().GetString("dir")

			if err := RunBitbucketInitCmd(cmd.Context(), p, dir); err != nil {
				return errors.Wrap(err, "failed to configure Bitbucket")
			}

			p.Logger.Info("Bitbucket configuration saved successfully")

			return nil
		},
	}

	cmd.Flags().String("dir", setup.GetDefaultConfigDir(p.FS, p.Tool.Name), "directory containing the config file")

	return cmd
}

// RunBitbucketInitCmd materialises the target config (seeded from the merged
// init template when absent) and runs the wizard over it; writes land in the
// file as they are applied, with 0600 permissions. Optional [DualFormOption]s
// are propagated into the wizard so tests can inject deterministic form
// creators, mirroring [pkg/setup/ai.RunAIInit].
func RunBitbucketInitCmd(ctx context.Context, p *props.Props, dir string, opts ...DualFormOption) error {
	editor, _, err := setup.OpenConfigEditor(ctx, p, dir, false)
	if err != nil {
		return err
	}

	return RunBitbucketInit(ctx, p, editor, opts...)
}
