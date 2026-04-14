package github

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"charm.land/huh/v2"
	"github.com/charmbracelet/keygen"
	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"golang.org/x/crypto/ssh"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	githubvcs "github.com/phpboyscout/go-tool-base/pkg/vcs/github"
)

const (
	// dirPermUserOnly is the permission mode for user-only directories (0700).
	dirPermUserOnly = 0o700
	// dirPermSSH is the permission mode for SSH directories (0700).
	dirPermSSH = 0o700
	// dirPermPublic is the permission mode for public files (0644).
	dirPermPublic = 0o644
	// minPassphraseLength is the minimum length required for SSH key passphrases.
	minPassphraseLength = 12
)

var (
	GitHubHost = "github.com"

	// Mockable functions.
	ghLoginFunc         = githubvcs.GHLogin
	newGitHubClientFunc = func(cfg config.Containable) (githubvcs.GitHubClient, error) {
		return githubvcs.NewGitHubClient(cfg)
	}
)

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
func ConfigureSSHKey(props *props.Props, cfg config.Containable, opts ...ConfigureSSHKeyOption) (string, string, error) {
	props.Logger.Info("Configuring SSH key for use with Github")

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
	if cfg.IsSet("github.ssh.key.path") {
		targetKey = cfg.GetString("github.ssh.key.path")
	}

	form := optsConfig.sshKeySelectFormCreator(&targetKey, potentialKeys)
	if form != nil {
		if err := form.Run(); err != nil {
			return "", "", err
		}
	}

	return handleSSHKeySelection(props, cfg, targetKey, optsConfig)
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
		// Accept passphrase-protected keys
		return err.Error() == "ssh: this private key is passphrase protected"
	}

	return true
}

func handleSSHKeySelection(props *props.Props, cfg config.Containable, targetKey string, optsConfig *configureSSHKeyConfig) (string, string, error) {
	keyType := "file"

	switch targetKey {
	case "generate":
		key, err := generateKey(props, cfg, optsConfig.generateKeyOpts...)
		if err != nil {
			return "", "", errors.Newf("failed to generate SSH key: %v", err)
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

func validateSSHKey(contents []byte, props *props.Props) error {
	_, err := ssh.ParseRawPrivateKey(contents)
	if err != nil {
		if err.Error() != "ssh: this private key is passphrase protected" {
			return errors.Newf("key is not a valid private key: %w", err)
		}

		return nil
	}

	props.Logger.Warn("Key is not protected with passphrase")

	return nil
}

type generateKeyConfig struct {
	passphraseFormCreator    func(*string) *huh.Form
	uploadConfirmFormCreator func(*bool) *huh.Form
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
				Title("Upload SSH key to Github?").
				Affirmative("Yes!").
				Negative("No.").
				Value(upload),
		),
	)
}

func generateKey(props *props.Props, cfg config.Containable, opts ...GenerateKeyOption) (string, error) {
	optsConfig := &generateKeyConfig{
		passphraseFormCreator:    defaultPassphraseFormCreator,
		uploadConfirmFormCreator: defaultUploadConfirmFormCreator,
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

	var upload bool

	if err := runForm(optsConfig.uploadConfirmFormCreator(&upload)); err != nil {
		return keypath, errors.WithStack(err)
	}

	if upload {
		if err := uploadSSHKeyToGitHub(props, cfg, keyname, publicKeyBytes); err != nil {
			return keypath, err
		}
	} else {
		props.Logger.Warn("You must ensure your SSH key is added to Github")
	}

	return keypath, err
}

func uploadSSHKeyToGitHub(props *props.Props, cfg config.Containable, keyname string, publicKey []byte) error {
	props.Logger.Info("Uploading SSH public key to Github", "key", string(publicKey))

	c, err := newGitHubClientFunc(cfg)
	if err != nil {
		return errors.WithStack(err)
	}

	err = c.UploadKey(context.Background(), keyname, publicKey)
	if err != nil {
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
	if err := afero.WriteFile(fs, keypath, privateKeyBytes, dirPermUserOnly); err != nil {
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
