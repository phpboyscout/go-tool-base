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
// only Host is set on the release source. A provider that does not implement
// KeyManager yields [forgeapi.ErrNotSupported], which the caller treats as
// "skip automated upload and tell the user to add the key manually".
func defaultKeyManager(profile Profile) func(context.Context, config.Reader) (forgeapi.KeyManager, error) {
	return func(ctx context.Context, cfg config.Reader) (forgeapi.KeyManager, error) {
		factory, err := forgeapi.Lookup(profile.Provider)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		provider, err := factory(ctx, forgeapi.ReleaseSourceConfig{Host: profile.Host}, vcs.ConfigFromReader(cfg))
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

type configureSSHKeyConfig struct {
	sshKeySelectFormCreator func(*string, []huh.Option[string]) *huh.Form
	sshKeyPathFormCreator   func(*string) *huh.Form
	generateKeyOpts         []GenerateKeyOption
}

// ConfigureSSHKeyOption is a functional option for ConfigureSSHKey.
type ConfigureSSHKeyOption func(*configureSSHKeyConfig)

// WithSSHKeySelectForm overrides the SSH key selection form (for testing).
func WithSSHKeySelectForm(creator func(*string, []huh.Option[string]) *huh.Form) ConfigureSSHKeyOption {
	return func(c *configureSSHKeyConfig) {
		c.sshKeySelectFormCreator = creator
	}
}

// WithSSHKeyPathForm overrides the SSH key path input form (for testing).
func WithSSHKeyPathForm(creator func(*string) *huh.Form) ConfigureSSHKeyOption {
	return func(c *configureSSHKeyConfig) {
		c.sshKeyPathFormCreator = creator
	}
}

// WithGenerateKeyOptions passes options through to the key generation step.
func WithGenerateKeyOptions(opts ...GenerateKeyOption) ConfigureSSHKeyOption {
	return func(c *configureSSHKeyConfig) {
		c.generateKeyOpts = opts
	}
}

func defaultSSHKeySelectFormCreator(targetKey *string, options []huh.Option[string]) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select SSH key").
				Description("pick a private key from the list, enter a path to a key manually or generate a new key").
				Options(options...).
				Value(targetKey),
		),
	)
}

func defaultSSHKeyPathFormCreator(targetKey *string) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewText().
				Title("Enter path to SSH key").
				Value(targetKey),
		),
	)
}

// ConfigureSSHKey runs the interactive SSH key configuration flow.
func ConfigureSSHKey(profile Profile, props *props.Props, cfg config.Reader, opts ...ConfigureSSHKeyOption) (string, string, error) {
	props.Logger.Info("Configuring SSH key", "provider", profile.Label)

	optsConfig := &configureSSHKeyConfig{
		sshKeySelectFormCreator: defaultSSHKeySelectFormCreator,
		sshKeyPathFormCreator:   defaultSSHKeyPathFormCreator,
	}

	for _, opt := range opts {
		opt(optsConfig)
	}

	potentialKeys, err := discoverSSHKeys(props)
	if err != nil {
		return "", "", err
	}

	// Add additional options
	potentialKeys = append(potentialKeys, huh.NewOption("Generate a new SSH key", "generate"))
	potentialKeys = append(potentialKeys, huh.NewOption("I use ssh-agent to handle my keys", "agent"))
	potentialKeys = append(potentialKeys, huh.NewOption("Enter path to key manually", "other"))

	var targetKey string
	if cfg.IsSet(profile.sshKeyPathKey()) {
		targetKey = cfg.GetString(profile.sshKeyPathKey())
	}

	form := optsConfig.sshKeySelectFormCreator(&targetKey, potentialKeys)
	if form != nil {
		if err := form.Run(); err != nil {
			return "", "", err
		}
	}

	return handleSSHKeySelection(profile, props, cfg, targetKey, optsConfig)
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

func handleSSHKeySelection(profile Profile, props *props.Props, cfg config.Reader, targetKey string, optsConfig *configureSSHKeyConfig) (string, string, error) {
	keyType := "file"

	switch targetKey {
	case "generate":
		key, err := generateKey(profile, props, cfg, optsConfig.generateKeyOpts...)
		if err != nil {
			return "", "", errors.Wrap(err, "failed to generate SSH key")
		}

		return keyType, key, nil

	case "agent":
		return "agent", "", nil

	case "other":
		key, err := promptAndValidateSSHKey(props, optsConfig)
		if err != nil {
			return "", "", err
		}

		return keyType, key, nil

	default:
		return keyType, targetKey, nil
	}
}

func promptAndValidateSSHKey(props *props.Props, optsConfig *configureSSHKeyConfig) (string, error) {
	var targetKey string

	form := optsConfig.sshKeyPathFormCreator(&targetKey)
	if form != nil {
		if err := form.Run(); err != nil {
			return "", err
		}
	}

	contents, err := afero.ReadFile(props.FS, targetKey)
	if err != nil {
		return "", errors.Newf("could not read file: %w", err)
	}

	if err := validateSSHKey(contents, props); err != nil {
		return "", err
	}

	return targetKey, nil
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

type generateKeyConfig struct {
	passphraseFormCreator    func(*string) *huh.Form
	uploadConfirmFormCreator func(*bool) *huh.Form
	keyManagerFactory        func(context.Context, config.Reader) (forgeapi.KeyManager, error)
}

// GenerateKeyOption is a functional option for SSH key generation.
type GenerateKeyOption func(*generateKeyConfig)

// WithPassphraseForm overrides the passphrase input form (for testing).
func WithPassphraseForm(creator func(*string) *huh.Form) GenerateKeyOption {
	return func(c *generateKeyConfig) {
		c.passphraseFormCreator = creator
	}
}

// WithUploadConfirmForm overrides the upload confirmation form (for testing).
func WithUploadConfirmForm(creator func(*bool) *huh.Form) GenerateKeyOption {
	return func(c *generateKeyConfig) {
		c.uploadConfirmFormCreator = creator
	}
}

// WithKeyManager overrides the [forgeapi.KeyManager] constructor used when
// uploading SSH keys. Tests pass a factory returning a fake; production callers
// omit it to get the registered provider's key-upload capability.
func WithKeyManager(factory func(context.Context, config.Reader) (forgeapi.KeyManager, error)) GenerateKeyOption {
	return func(c *generateKeyConfig) {
		c.keyManagerFactory = factory
	}
}

func defaultPassphraseFormCreator(passphrase *string) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Enter passphrase for new SSH key").
				Description(fmt.Sprintf("should be a minimum of %d characters long", minPassphraseLength)).
				EchoMode(huh.EchoModePassword).
				Validate(func(s string) error {
					if len(s) < minPassphraseLength {
						return errors.Newf("passphrase must be at least %d characters long", minPassphraseLength)
					}

					return nil
				}).
				Value(passphrase),
		),
	)
}

func defaultUploadConfirmFormCreator(upload *bool) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Upload SSH key to the forge?").
				Affirmative("Yes!").
				Negative("No.").
				Value(upload),
		),
	)
}

func generateKey(profile Profile, props *props.Props, cfg config.Reader, opts ...GenerateKeyOption) (string, error) {
	optsConfig := &generateKeyConfig{
		passphraseFormCreator:    defaultPassphraseFormCreator,
		uploadConfirmFormCreator: defaultUploadConfirmFormCreator,
		keyManagerFactory:        defaultKeyManager(profile),
	}

	for _, opt := range opts {
		opt(optsConfig)
	}

	now := time.Now()
	keyname := fmt.Sprintf("id_%s_%s", props.Tool.Name, now.Format("20060102150405"))

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return keyname, errors.WithStack(err)
	}

	keypath := filepath.Join(homeDir, ".ssh", keyname)
	// Ensure .ssh directory exists in props.FS
	sshDir := filepath.Dir(keypath)
	if err := props.FS.MkdirAll(sshDir, dirPermSSH); err != nil {
		return keyname, errors.Newf("failed to create ssh directory: %w", err)
	}

	var passphrase string

	if err := runForm(optsConfig.passphraseFormCreator(&passphrase)); err != nil {
		return keypath, errors.WithStack(err)
	}

	props.Logger.Info("Generating new SSH key with passphrase", "path", keypath)

	publicKeyBytes, err := generateAndSaveSSHKey(props.FS, keypath, passphrase)
	if err != nil {
		return keypath, err
	}

	// The SSH stage is ctx-free by design — its upload bounds itself, and
	// plumbing a context through it is tracked in the forge-repo-setup
	// follow-ups spec. maybeUploadKey takes one so it is ready when that lands;
	// until then this is the same Background the upload already used.
	return keypath, maybeUploadKey(context.Background(), profile, props, cfg, optsConfig, keyname, publicKeyBytes)
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
	optsConfig *generateKeyConfig,
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

	if err := runForm(optsConfig.uploadConfirmFormCreator(&upload)); err != nil {
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

func runForm(form *huh.Form) error {
	if form == nil {
		return nil
	}

	return form.Run()
}

// configureSSH runs the SSH key configuration stage and records the selected
// key type and path under the profile's SSH config keys.
func (i *Initialiser) configureSSH(p *props.Props, cfg setup.Editor) error {
	keyType, keyPath, err := ConfigureSSHKey(i.profile, p, cfg.View(), i.sshOpts...)
	if err != nil {
		return err
	}

	if err := cfg.Set(i.profile.sshKeyTypeKey(), keyType); err != nil {
		return err
	}

	return cfg.Set(i.profile.sshKeyPathKey(), keyPath)
}
