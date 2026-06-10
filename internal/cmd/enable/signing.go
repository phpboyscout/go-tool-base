package enable

import (
	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	icmd "gitlab.com/phpboyscout/go-tool-base/internal/cmd"
	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/utils"
)

// ErrInvalidKeySource is returned when --key-source is not one of the
// accepted values.
var ErrInvalidKeySource = errors.New("invalid --key-source: must be embedded, external, or both")

// validKeySources lists the accepted --key-source values.
var validKeySources = map[string]bool{"embedded": true, "external": true, "both": true}

type signingOptions struct {
	Path                      string
	Email                     string
	KeySource                 string
	RequireSignature          bool
	RequireExternalCrosscheck bool
}

// NewCmdEnableSigning returns the `gtb enable signing` subcommand. It
// flips the manifest signing block on and selectively regenerates only
// the signing-affected files.
func NewCmdEnableSigning(p *props.Props) *setup.Command {
	opts := signingOptions{KeySource: "both"}

	cmd := &cobra.Command{
		Use:   "signing",
		Short: "Enable consumer-side release-signing verification",
		Long: `Turn on self-update signature verification for a generated project.

Sets properties.signing.enabled = true in .gtb/manifest.yaml, scaffolds the
internal/trustkeys embed package (drop your minted *.asc into its keys/
directory), wires Signing: props.SigningConfig{EmbeddedKeys: trustkeys.Keys()}
into the generated root command, and emits a signing.go with the enforcement
defaults from the supplied flags. Only the signing-affected files change.

--require-signature stays off by default and must only be flipped once a
signed release has shipped (the N+1 rollout); re-run this command with the
flag to do so.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runEnableSigning(cmd, p, &opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Path to project root")
	cmd.Flags().StringVar(&opts.Email, "email", "", "Release WKD email (external_key_email); enables the external trust-anchor leg")
	cmd.Flags().StringVar(&opts.KeySource, "key-source", "both", "Trust-anchor source: embedded, external, or both")
	cmd.Flags().BoolVar(&opts.RequireSignature, "require-signature", false, "Fail updates closed when no valid signature is present (only flip once a signed release has shipped)")
	cmd.Flags().BoolVar(&opts.RequireExternalCrosscheck, "require-external-crosscheck", false, "Fail closed when the external (WKD) resolver is unreachable")

	return setup.Wrap("", cmd)
}

func runEnableSigning(cmd *cobra.Command, p *props.Props, opts *signingOptions) error {
	opts.Path = icmd.ResolveProjectPath(p, opts.Path)

	// Prompt for the email when omitted and the session is interactive
	// and not in CI. require_signature is deliberately never prompted.
	if opts.Email == "" && utils.IsInteractive() && !isCI(cmd, p) {
		if err := opts.promptInteractive(); err != nil {
			return err
		}
	}

	if !validKeySources[opts.KeySource] {
		return errors.Wrapf(ErrInvalidKeySource, "%q", opts.KeySource)
	}

	signing := generator.ManifestSigning{
		ExternalKeyEmail:          opts.Email,
		RequireSignature:          opts.RequireSignature,
		KeySource:                 normaliseKeySource(opts.KeySource),
		RequireExternalCrosscheck: opts.RequireExternalCrosscheck,
	}

	gen := generator.New(p, &generator.Config{Path: opts.Path, Overwrite: "allow"})
	if err := gen.EnableSigning(cmd.Context(), signing); err != nil {
		return err
	}

	p.Logger.Info("Signing enabled.")
	p.Logger.Info("Drop your minted public key(s) into internal/trustkeys/keys/*.asc, then commit.")

	if !opts.RequireSignature {
		p.Logger.Info("require_signature stays off until a signed release has shipped — re-run with --require-signature to flip it.")
	}

	return nil
}

// promptInteractive collects the WKD email and key source in a small
// huh form. require_signature is never prompted.
func (o *signingOptions) promptInteractive() error {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Release WKD email").
				Description("Used to derive the WKD URL and enable the external trust-anchor leg. Leave empty for embedded-only.").
				Placeholder("release@example.com").
				Value(&o.Email),
			huh.NewSelect[string]().
				Title("Key source").
				Description("Where the trust anchor comes from.").
				Options(
					huh.NewOption("Both (embedded + external cross-check)", "both"),
					huh.NewOption("Embedded only", "embedded"),
					huh.NewOption("External (WKD) only", "external"),
				).
				Value(&o.KeySource),
		),
	)

	return form.Run()
}

// normaliseKeySource maps the framework default ("both") to an empty
// string so the manifest stays minimal; the framework treats an empty
// key_source as "both".
func normaliseKeySource(src string) string {
	if src == "both" {
		return ""
	}

	return src
}

// isCI reports whether the tool is running in CI, honouring the global
// --ci persistent flag and the ci config key.
func isCI(cmd *cobra.Command, p *props.Props) bool {
	if ci, err := cmd.Flags().GetBool("ci"); err == nil && ci {
		return true
	}

	if p.Config != nil {
		return p.Config.GetBool("ci")
	}

	return false
}
