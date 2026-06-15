package enable

import (
	"slices"
	"strings"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	icmd "gitlab.com/phpboyscout/go-tool-base/internal/cmd"
	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/signing"
	"gitlab.com/phpboyscout/go-tool-base/pkg/utils"
)

// ErrInvalidKeySource is returned when --key-source is not one of the
// accepted values.
var ErrInvalidKeySource = errors.New("invalid --key-source: must be embedded, external, or both")

// ErrInvalidBackend is returned when --backend is not a registered signing
// backend.
var ErrInvalidBackend = errors.New("invalid --backend: not a registered signing backend")

// validKeySources lists the accepted --key-source values.
var validKeySources = map[string]bool{"embedded": true, "external": true, "both": true}

type signingOptions struct {
	Path                      string
	Email                     string
	KeySource                 string
	RequireSignature          bool
	RequireExternalCrosscheck bool
	// Release-pipeline fields. Recording a KeyID is what turns the
	// generated GoReleaser signs block on. Backend/KMSRegion/PublicKey
	// default in the generator when a key id is set.
	Backend   string
	KeyID     string
	KMSRegion string
	PublicKey string
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
	cmd.Flags().StringVar(&opts.KeyID, "key-id", "", "Signing key id/ARN/alias (or PEM path for the local backend) the release pipeline signs with; recording it wires the GoReleaser signs block")
	cmd.Flags().StringVar(&opts.Backend, "backend", "", "gtb sign backend for the release pipeline (default aws-kms when --key-id is set)")
	cmd.Flags().StringVar(&opts.KMSRegion, "kms-region", "", "AWS region for the aws-kms backend (default eu-west-2)")
	cmd.Flags().StringVar(&opts.PublicKey, "public-key", "", "Path to the embedded public key the signature identifies (default internal/trustkeys/keys/signing-key-v1.asc)")

	return setup.Wrap("", cmd)
}

// signingFlagSet records which signing fields an `enable signing` invocation
// actually provided — via an explicit flag or the interactive prompt. Fields
// not provided keep their existing manifest value, so changing one setting on
// a re-run never resets the others.
type signingFlagSet struct {
	email             bool
	keySource         bool
	requireSignature  bool
	requireCrosscheck bool
	backend           bool
	keyID             bool
	kmsRegion         bool
	publicKey         bool
}

// validateSigningFlags checks the flag values this invocation actually
// provided: the key source must be one of the accepted set, and the backend
// must be a registered signing backend. Unprovided fields aren't validated —
// they keep their existing manifest value.
func validateSigningFlags(opts *signingOptions, set signingFlagSet) error {
	if set.keySource && !validKeySources[opts.KeySource] {
		return errors.Wrapf(ErrInvalidKeySource, "%q", opts.KeySource)
	}

	if set.backend && opts.Backend != "" && !slices.Contains(signing.Names(), opts.Backend) {
		return errors.Wrapf(ErrInvalidBackend, "%q (available: %s)", opts.Backend, strings.Join(signing.Names(), ", "))
	}

	return nil
}

// mergeSigning overlays the provided option values onto the current manifest
// signing block, leaving unprovided fields untouched.
func mergeSigning(current generator.ManifestSigning, opts *signingOptions, set signingFlagSet) generator.ManifestSigning {
	m := current

	if set.email {
		m.ExternalKeyEmail = opts.Email
	}

	if set.keySource {
		m.KeySource = normaliseKeySource(opts.KeySource)
	}

	if set.requireSignature {
		m.RequireSignature = opts.RequireSignature
	}

	if set.requireCrosscheck {
		m.RequireExternalCrosscheck = opts.RequireExternalCrosscheck
	}

	if set.backend {
		m.Backend = opts.Backend
	}

	if set.keyID {
		m.KeyID = opts.KeyID
	}

	if set.kmsRegion {
		m.KMSRegion = opts.KMSRegion
	}

	if set.publicKey {
		m.PublicKey = opts.PublicKey
	}

	return m
}

// maybePromptEmail asks for the WKD email only on a genuine first enable: none
// passed, none already in the manifest, and an interactive non-CI session. A
// re-run that already has an email (e.g. adding --key-id later) never re-asks.
// When the prompt runs, the email and key source it collects count as provided.
func maybePromptEmail(cmd *cobra.Command, p props.ConfigProvider, opts *signingOptions, current generator.ManifestSigning, set *signingFlagSet) error {
	if opts.Email != "" || current.ExternalKeyEmail != "" || !utils.IsInteractive() || isCI(cmd, p) {
		return nil
	}

	if err := opts.promptInteractive(); err != nil {
		return err
	}

	set.email = true
	set.keySource = true

	return nil
}

func runEnableSigning(cmd *cobra.Command, p *props.Props, opts *signingOptions) error {
	opts.Path = icmd.ResolveProjectPath(p, opts.Path)

	// Track which fields this invocation actually provides, so re-running to
	// change one setting (the N+1 → N+2 → N+3 rollout) leaves the others as
	// they are rather than resetting them to defaults.
	set := signingFlagSet{
		email:             cmd.Flags().Changed("email"),
		keySource:         cmd.Flags().Changed("key-source"),
		requireSignature:  cmd.Flags().Changed("require-signature"),
		requireCrosscheck: cmd.Flags().Changed("require-external-crosscheck"),
		backend:           cmd.Flags().Changed("backend"),
		keyID:             cmd.Flags().Changed("key-id"),
		kmsRegion:         cmd.Flags().Changed("kms-region"),
		publicKey:         cmd.Flags().Changed("public-key"),
	}

	gen := generator.New(p, &generator.Config{Path: opts.Path, Overwrite: "allow"})

	// Load the existing signing block first, so flag overrides merge onto it
	// (changing one field on a re-run never drops the others) and the email
	// prompt only fires when there genuinely isn't one yet.
	current, err := gen.CurrentSigning()
	if err != nil {
		return err
	}

	if err := maybePromptEmail(cmd, p, opts, current, &set); err != nil {
		return err
	}

	if err := validateSigningFlags(opts, set); err != nil {
		return err
	}

	merged := mergeSigning(current, opts, set)

	if err := gen.EnableSigning(cmd.Context(), merged); err != nil {
		return err
	}

	p.Logger.Info("Signing enabled.")
	p.Logger.Info("Drop your minted public key(s) into internal/trustkeys/keys/*.asc, then commit.")

	if !merged.RequireSignature {
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
func isCI(cmd *cobra.Command, p props.ConfigProvider) bool {
	if ci, err := cmd.Flags().GetBool("ci"); err == nil && ci {
		return true
	}

	if cfg := p.GetConfig(); cfg != nil {
		return cfg.GetBool("ci")
	}

	return false
}
