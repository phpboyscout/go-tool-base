package forge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"charm.land/huh/v2"
	"github.com/charmbracelet/keygen"
	"github.com/spf13/afero"
	"golang.org/x/crypto/ssh"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"

	forgeapi "gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
)

const (
	// dirPermSSH is the permission mode for SSH directories (0700).
	dirPermSSH = 0o700
	// dirPermPublic is the permission mode for public files (0644).
	dirPermPublic = 0o644
	// filePermKey is the permission mode for a private key file (0600 — owner
	// read/write only), the conventional mode for SSH private keys.
	filePermKey = 0o600
	// minPassphraseLength is the minimum length required for SSH key passphrases.
	minPassphraseLength = 12
)

// defaultKeyManager builds the registered forge provider and type-asserts it
// for the optional [forgeapi.KeyManager] capability. SSH-key upload targets the
// profile's Host (or the Enterprise host carried in the config's url.api), so
// the endpoint carries only the provider type and the host. A provider that does
// not implement KeyManager yields [forgeapi.ErrNotSupported], which the caller
// treats as "skip automated upload and tell the user to add the key manually".
//
// Type comes from the same profile field that selected the factory, so the two
// cannot drift; see defaultForgeProvider for why an untyped endpoint is worse
// than a failing one.
func defaultKeyManager(profile Profile) func(context.Context, config.Reader) (forgeapi.KeyManager, error) {
	return func(ctx context.Context, cfg config.Reader) (forgeapi.KeyManager, error) {
		factory, err := forgeapi.Lookup(profile.Provider)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		endpoint := forgeapi.Endpoint{Type: profile.Provider, Host: profile.Host}

		provider, err := factory(ctx, endpoint, vcs.ConfigFromReader(cfg))
		if err != nil {
			return nil, errors.WithStack(err)
		}

		// forgeapi.As walks any Provider decorator chain, so a forwarding
		// wrapper cannot silently strip the KeyManager capability.
		var km forgeapi.KeyManager
		if !forgeapi.As(provider, &km) {
			return nil, errors.Wrapf(forgeapi.ErrNotSupported,
				"%s provider does not support SSH-key upload", profile.Provider)
		}

		return km, nil
	}
}

// Choices the key selector adds after the discovered keys.
const (
	sshChoiceGenerate = "generate"
	sshChoiceAgent    = "agent"
	sshChoiceOther    = "other"
)

type configureSSHKeyConfig struct {
	keyManagerFactory func(context.Context, config.Reader) (forgeapi.KeyManager, error)
}

// ConfigureSSHKeyOption is a functional option for ConfigureSSHKey.
type ConfigureSSHKeyOption func(*configureSSHKeyConfig)

// withKeyManager overrides the [forgeapi.KeyManager] constructor used when
// uploading SSH keys. Tests pass a factory returning a fake; production callers
// omit it to get the registered provider's key-upload capability.
func withKeyManager(factory func(context.Context, config.Reader) (forgeapi.KeyManager, error)) ConfigureSSHKeyOption {
	return func(c *configureSSHKeyConfig) {
		c.keyManagerFactory = factory
	}
}

// sshKeyConfig is what the SSH form collects.
type sshKeyConfig struct {
	// Choice is a discovered key's path or one of the sshChoice* sentinels.
	Choice string
	// Path is the key the user names when Choice is sshChoiceOther.
	Path string
	// Passphrase protects the key generated when Choice is sshChoiceGenerate.
	Passphrase string
}

// sshForm is the SSH stage's one form (spec 0198): the key selector, then
// the page the choice needs. The upload question is asked afterwards,
// because whether an upload is possible is only known once the key manager
// resolves (spec 0186 D7).
func sshForm(cfg *sshKeyConfig, options []huh.Option[string]) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("ssh-key").
				Title("Select SSH key").
				Description("pick a private key from the list, enter a path to a key manually or generate a new key").
				Options(options...).
				Value(&cfg.Choice),
		),
		huh.NewGroup(
			huh.NewText().
				Key("ssh-key-path").
				Title("Enter path to SSH key").
				Value(&cfg.Path),
		).WithHideFunc(func() bool { return cfg.Choice != sshChoiceOther }),
		huh.NewGroup(
			huh.NewInput().
				Key("passphrase").
				Title("Enter passphrase for new SSH key").
				Description(fmt.Sprintf("should be a minimum of %d characters long", minPassphraseLength)).
				EchoMode(huh.EchoModePassword).
				Validate(validatePassphrase).
				Value(&cfg.Passphrase),
		).WithHideFunc(func() bool { return cfg.Choice != sshChoiceGenerate }),
	)
}

func validatePassphrase(s string) error {
	if len(s) < minPassphraseLength {
		return errors.Wrapf(ErrPassphraseTooShort, "%d characters", minPassphraseLength)
	}

	return nil
}

// ErrPassphraseTooShort is returned when a generated key's passphrase is
// shorter than minPassphraseLength, including when huh's accessible mode
// could not read one at all (no terminal) and left it blank.
var ErrPassphraseTooShort = errors.NewSentinel("gtb.setup.forge.passphrase_too_short",
	"passphrase must be at least")

// ConfigureSSHKey runs the interactive SSH key configuration flow on p's IO
// and returns the key type ("file" or "agent") and path.
func ConfigureSSHKey(ctx context.Context, profile Profile, p *props.Props, cfg config.Reader, opts ...ConfigureSSHKeyOption) (string, string, error) {
	p.Logger.Info("Configuring SSH key", "provider", profile.Label)

	optsConfig := &configureSSHKeyConfig{keyManagerFactory: defaultKeyManager(profile)}

	for _, opt := range opts {
		opt(optsConfig)
	}

	potentialKeys, err := discoverSSHKeys(p)
	if err != nil {
		return "", "", err
	}

	potentialKeys = append(potentialKeys,
		huh.NewOption("Generate a new SSH key", sshChoiceGenerate),
		huh.NewOption("I use ssh-agent to handle my keys", sshChoiceAgent),
		huh.NewOption("Enter path to key manually", sshChoiceOther),
	)

	keyCfg := &sshKeyConfig{}
	if cfg.IsSet(profile.sshKeyPathKey()) {
		keyCfg.Choice = cfg.GetString(profile.sshKeyPathKey())
	}

	if err := setup.RunForm(ctx, p, sshForm(keyCfg, potentialKeys)); err != nil {
		return "", "", err
	}

	return handleSSHKeySelection(ctx, profile, p, cfg, keyCfg, optsConfig)
}

func discoverSSHKeys(props *props.Props) ([]huh.Option[string], error) {
	potentialKeys := []huh.Option[string]{}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	targetDir := filepath.Join(homeDir, ".ssh")
	props.Logger.Debug("Checking for SSH keys in", "dir", targetDir)

	if _, err := props.FS.Stat(targetDir); err != nil && errors.Is(err, os.ErrNotExist) {
		if err := props.FS.MkdirAll(targetDir, dirPermSSH); err != nil {
			return nil, errors.Newf("could not create directory: %w", err)
		}
	}

	sshDir := targetDir

	err = afero.Walk(props.FS, sshDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if isValidSSHKey(props.FS, path) {
			potentialKeys = append(potentialKeys, huh.NewOption(path, path))
		}

		return nil
	})

	return potentialKeys, err
}

func isValidSSHKey(fs afero.Fs, path string) bool {
	contents, err := afero.ReadFile(fs, path)
	if err != nil {
		return false
	}

	_, err = ssh.ParseRawPrivateKey(contents)
	if err != nil {
		// Accept passphrase-protected keys. Detect them by the typed error, not
		// its message — a wording change upstream must not silently reject
		// otherwise-valid protected keys.
		var missing *ssh.PassphraseMissingError

		return errors.As(err, &missing)
	}

	return true
}

func handleSSHKeySelection(ctx context.Context, profile Profile, p *props.Props, cfg config.Reader, keyCfg *sshKeyConfig, optsConfig *configureSSHKeyConfig) (string, string, error) {
	keyType := "file"

	switch keyCfg.Choice {
	case sshChoiceGenerate:
		key, err := generateKey(ctx, profile, p, cfg, keyCfg.Passphrase, optsConfig)
		if err != nil {
			return "", "", errors.Wrap(err, "failed to generate SSH key")
		}

		return keyType, key, nil

	case sshChoiceAgent:
		return sshChoiceAgent, "", nil

	case sshChoiceOther:
		if err := validateSSHKeyFile(p, keyCfg.Path); err != nil {
			return "", "", err
		}

		return keyType, keyCfg.Path, nil

	default:
		return keyType, keyCfg.Choice, nil
	}
}

func validateSSHKeyFile(p *props.Props, path string) error {
	contents, err := afero.ReadFile(p.FS, path)
	if err != nil {
		return errors.Newf("could not read file: %w", err)
	}

	return validateSSHKey(contents, p)
}

func validateSSHKey(contents []byte, p props.LoggerProvider) error {
	_, err := ssh.ParseRawPrivateKey(contents)
	if err != nil {
		// Detect a passphrase-protected key by the typed error, not its message,
		// so an upstream wording change cannot turn a valid protected key into a
		// hard "invalid key" failure.
		var missing *ssh.PassphraseMissingError
		if !errors.As(err, &missing) {
			return errors.Newf("key is not a valid private key: %w", err)
		}

		return nil
	}

	p.GetLogger().Warn("Key is not protected with passphrase")

	return nil
}

func uploadConfirmForm(upload *bool) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Key("upload").
				Title("Upload SSH key to the forge?").
				Affirmative("Yes!").
				Negative("No.").
				Value(upload),
		),
	)
}

// generateKey writes a new ed25519 key protected by passphrase under ~/.ssh
// and offers to upload it. The passphrase is checked again here because
// huh's accessible mode cannot read one without a terminal and leaves it
// blank without error.
func generateKey(ctx context.Context, profile Profile, p *props.Props, cfg config.Reader, passphrase string, optsConfig *configureSSHKeyConfig) (string, error) {
	if err := validatePassphrase(passphrase); err != nil {
		return "", err
	}

	now := time.Now()
	keyname := fmt.Sprintf("id_%s_%s", p.Tool.Name, now.Format("20060102150405"))

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return keyname, errors.WithStack(err)
	}

	keypath := filepath.Join(homeDir, ".ssh", keyname)
	// Ensure .ssh directory exists in p.FS
	sshDir := filepath.Dir(keypath)
	if err := p.FS.MkdirAll(sshDir, dirPermSSH); err != nil {
		return keyname, errors.Newf("failed to create ssh directory: %w", err)
	}

	p.Logger.Info("Generating new SSH key with passphrase", "path", keypath)

	publicKeyBytes, err := generateAndSaveSSHKey(p.FS, keypath, passphrase)
	if err != nil {
		return keypath, err
	}

	return keypath, maybeUploadKey(ctx, profile, p, cfg, optsConfig, keyname, publicKeyBytes)
}

// maybeUploadKey resolves the key manager, asks whether to upload only when an
// upload is actually possible, and performs it.
//
// The resolution happens BEFORE the prompt (spec 0186 D7). The capability is
// knowable at that point, and asking a question the program can already answer —
// then overruling the "yes" with an add-it-manually warning — is the shape of
// prompt that teaches people to distrust a wizard. The resolved manager is then
// reused rather than constructed a second time.
//
// The stage is not skipped wholesale when upload is impossible: the key has
// already been generated, saved and recorded, all of which stand on their own.
func maybeUploadKey(
	ctx context.Context,
	profile Profile,
	p *props.Props,
	cfg config.Reader,
	optsConfig *configureSSHKeyConfig,
	keyname string,
	publicKey []byte,
) error {
	km, err := optsConfig.keyManagerFactory(ctx, cfg)
	if err != nil {
		if errors.Is(err, forgeapi.ErrNotSupported) {
			p.Logger.Warn("provider does not support SSH-key upload; add the key manually",
				"provider", profile.Label)

			return nil
		}

		return errors.WithStack(err)
	}

	var upload bool

	if err := setup.RunForm(ctx, p, uploadConfirmForm(&upload)); err != nil {
		return errors.WithStack(err)
	}

	if !upload {
		p.Logger.Warn("You must ensure your SSH key is added to the forge")

		return nil
	}

	return uploadSSHKey(ctx, profile, p, km, keyname, publicKey)
}

// uploadSSHKey posts an already-generated public key using an already-resolved
// KeyManager. Resolution happens in the caller, before the upload prompt, so a
// provider that cannot upload is never asked about — see [generateKey].
func uploadSSHKey(
	ctx context.Context,
	profile Profile,
	p props.LoggerProvider,
	km forgeapi.KeyManager,
	keyname string,
	publicKey []byte,
) error {
	log := p.GetLogger()
	log.Info("Uploading SSH public key", "provider", profile.Label, "key", string(publicKey))

	if err := km.UploadKey(ctx, keyname, publicKey); err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func generateAndSaveSSHKey(fs afero.Fs, keypath, passphrase string) ([]byte, error) {
	kp, err := keygen.New(keypath,
		keygen.WithPassphrase(passphrase),
		keygen.WithKeyType(keygen.Ed25519),
	)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// Manually write keys to props.FS
	privateKeyBytes := kp.RawProtectedPrivateKey()
	if err := afero.WriteFile(fs, keypath, privateKeyBytes, filePermKey); err != nil {
		return nil, errors.Newf("failed to write private key: %w", err)
	}

	publicKeyBytes := kp.RawAuthorizedKey()
	if err := afero.WriteFile(fs, keypath+".pub", publicKeyBytes, dirPermPublic); err != nil {
		return nil, errors.Newf("failed to write public key: %w", err)
	}

	return publicKeyBytes, nil
}

// configureSSH runs the SSH key configuration stage and records the selected
// key type and path under the profile's SSH config keys.
func (i *Initialiser) configureSSH(ctx context.Context, p *props.Props, cfg setup.Editor) error {
	keyType, keyPath, err := ConfigureSSHKey(ctx, i.profile, p, cfg.View(), i.sshOpts...)
	if err != nil {
		return err
	}

	if err := cfg.Set(i.profile.sshKeyTypeKey(), keyType); err != nil {
		return err
	}

	return cfg.Set(i.profile.sshKeyPathKey(), keyPath)
}
