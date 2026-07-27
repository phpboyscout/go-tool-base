package config

import (
	"fmt"
	"os"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// NewCmdTrust returns the "config trust" subcommand. A project-local
// ".<tool>.yaml" at a repository root can tune workflow settings, but its
// security-sensitive keys (self-update verification, telemetry consent,
// credentials) are IGNORED until the user explicitly trusts the file — so a
// hostile clone cannot downgrade security posture just by shipping one. This
// command records the current content of the discovered (or named) project-local
// file as trusted, direnv-style: editing the file afterwards revokes trust until
// it is re-run.
func NewCmdTrust(props *p.Props) *cobra.Command {
	var (
		list   bool
		forget bool
	)

	cmd := &cobra.Command{
		Use:   "trust [path]",
		Short: "Trust a project-local config file's security-sensitive keys",
		Long: `Trust the project-local ".<tool>.yaml" so its security-sensitive keys
(self-update verification, telemetry consent, credentials) are honoured.

Until a project-local file is trusted, those keys are ignored — a repository you
clone cannot silently weaken update verification or flip telemetry consent.
Workflow-tuning keys (logging, output, feature toggles) are always honoured.

  <tool> config trust           trust the project file discovered from the cwd
  <tool> config trust ./path    trust a specific file
  <tool> config trust --list    list trusted project files
  <tool> config trust --forget  revoke trust for the discovered/named file

Trust is bound to the file's exact content: editing a trusted file revokes trust
until you run this again.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if list {
				return runTrustList(cmd, props)
			}

			target, err := resolveTrustTarget(props, args)
			if err != nil {
				return err
			}

			if forget {
				if err := setup.UntrustProjectConfig(props.FS, props.Tool.Name, target); err != nil {
					return errors.Wrap(err, "revoking trust")
				}

				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "revoked trust for %s\n", target)

				return nil
			}

			if err := setup.TrustProjectConfig(props.FS, props.Tool.Name, target); err != nil {
				return errors.Wrap(err, "trusting project config")
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"trusted %s — its security-sensitive keys now apply\n", target)

			return nil
		},
	}

	cmd.Flags().BoolVar(&list, "list", false, "list trusted project-local config files")
	cmd.Flags().BoolVar(&forget, "forget", false, "revoke trust for the discovered or named file")

	return cmd
}

// resolveTrustTarget resolves which project-local file the command acts on: an
// explicit path argument if given, otherwise the file discovered by walking up
// from the working directory.
func resolveTrustTarget(props *p.Props, args []string) (string, error) {
	if len(args) == 1 && args[0] != "" {
		return args[0], nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", errors.Wrap(err, "resolving working directory")
	}

	target := setup.DiscoverProjectConfig(props.FS, props.Tool.Name, cwd)
	if target == "" {
		return "", errors.WithHintf(
			errors.Newf("no project-local .%s.yaml found from the current directory", props.Tool.Name),
			"Create .%s.yaml at your repository root, or pass an explicit path.", props.Tool.Name)
	}

	return target, nil
}

// runTrustList prints the trusted project-local files for the tool.
func runTrustList(cmd *cobra.Command, props *p.Props) error {
	paths, err := setup.ListTrustedProjects(props.FS, props.Tool.Name)
	if err != nil {
		return errors.Wrap(err, "listing trusted project configs")
	}

	if len(paths) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no trusted project-local config files")

		return nil
	}

	for _, path := range paths {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), path)
	}

	return nil
}
