package forge

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"charm.land/huh/v2"
	"github.com/charmbracelet/keygen"
	"github.com/spf13/afero"
	"golang.org/x/crypto/ssh"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/credentials"
	"gitlab.com/phpboyscout/go/errors"

	forgeapi "gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/internal/formpage"
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
		forgeCfg := vcs.ConfigFromReader(cfg)

		provider, err := factory(ctx, endpoint, forgeCfg, vcs.CredentialOptions(endpoint, forgeCfg, profile.FallbackEnv)...)
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
	// Store keeps Passphrase in the OS keychain so gtb can open the key.
	Store bool
}

// SSHKeySelection is what the SSH stage settled on: the key type ("file" or
// "agent"), its path, and the keychain reference to a generated key's
// passphrase when one was stored. The zero value is a declined stage, which
// records nothing.
type SSHKeySelection struct {
	Type, Path, Keychain string
}

// sshOpeners is what could open a generated key for gtb (spec 0210 D4).
type sshOpeners struct {
	keychain, agent bool
}

// agentDialTimeout bounds the reachability check on SSH_AUTH_SOCK.
const agentDialTimeout = time.Second

// discoverSSHOpeners probes the keychain under its own deadline, as the
// storage-mode page does, and dials the agent.
func discoverSSHOpeners(ctx context.Context) sshOpeners {
	probeCtx, cancel := context.WithTimeout(ctx, credentials.KeychainOpTimeout)
	defer cancel()

	return sshOpeners{keychain: credentials.Probe(probeCtx), agent: agentReachable(ctx)}
}

func agentReachable(ctx context.Context) bool {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return false
	}

	conn, err := (&net.Dialer{Timeout: agentDialTimeout}).DialContext(ctx, "unix", sock)
	if err != nil {
		return false
	}

	_ = conn.Close()

	return true
}

// sshForm is the SSH stage's one form (spec 0198): the key selector, then
// the page the choice needs. The upload question is asked afterwards,
// because whether an upload is possible is only known once the key manager
// resolves (spec 0186 D7).
//
// The store question comes before the passphrase so that a key nothing could
// open is declined before anyone types a passphrase for it (spec 0210 D4).
func sshPages(cfg *sshKeyConfig, options []huh.Option[string], secret huh.EchoMode, openers sshOpeners) []formpage.Page {
	generating := func() bool { return cfg.Choice == sshChoiceGenerate }

	return []formpage.Page{
		formpage.Of(huh.NewGroup(
			huh.NewSelect[string]().
				Key("ssh-key").
				Title("Select SSH key").
				Description("pick a private key from the list, enter a path to a key manually or generate a new key").
				Options(options...).
				Value(&cfg.Choice),
		)),
		formpage.Of(huh.NewGroup(
			huh.NewText().
				Key("ssh-key-path").
				Title("Enter path to SSH key").
				Value(&cfg.Path),
		)).HiddenWhen(func() bool { return cfg.Choice != sshChoiceOther }),
		formpage.Of(huh.NewGroup(
			huh.NewConfirm().
				Key("ssh-store-passphrase").
				Title("Store the new key's passphrase in the OS keychain?").
				Description("gtb needs it to open the key for git operations. Without it, the key works only while ssh-agent holds it.").
				Value(&cfg.Store),
		)).HiddenWhen(func() bool { return !generating() || !openers.keychain }),
		formpage.Of(huh.NewGroup(
			huh.NewInput().
				Key("passphrase").
				Title("Enter passphrase for new SSH key").
				Description(fmt.Sprintf("should be a minimum of %d characters long", minPassphraseLength)).
				EchoMode(secret).
				Validate(validatePassphrase).
				Value(&cfg.Passphrase),
		)).HiddenWhen(func() bool { return !generating() || (!cfg.Store && !openers.agent) }),
	}
}

// sshForm is sshPages as the one form the TUI runs.
func sshForm(cfg *sshKeyConfig, options []huh.Option[string], openers sshOpeners) *huh.Form {
	return huh.NewForm(formpage.Groups(sshPages(cfg, options, huh.EchoModePassword, openers)...)...)
}

func validatePassphrase(s string) error {
	if len(s) < minPassphraseLength {
		return errors.WithHintf(ErrPassphraseTooShort, "Use at least %d characters.", minPassphraseLength)
	}

	return nil
}

// ErrPassphraseTooShort is returned when a generated key's passphrase is
// shorter than minPassphraseLength, including when huh's accessible mode
// could not read one at all (no terminal) and left it blank.
var ErrPassphraseTooShort = errors.NewSentinel("gtb.setup.forge.passphrase_too_short",
	"passphrase is too short")

// ConfigureSSHKey runs the interactive SSH key configuration flow on p's IO
// and returns the key it settled on.
func ConfigureSSHKey(ctx context.Context, profile Profile, p *props.Props, cfg config.Reader, opts ...ConfigureSSHKeyOption) (SSHKeySelection, error) {
	p.Logger.Info("Configuring SSH key", "provider", profile.Label)

	optsConfig := &configureSSHKeyConfig{keyManagerFactory: defaultKeyManager(profile)}

	for _, opt := range opts {
		opt(optsConfig)
	}

	potentialKeys, err := discoverSSHKeys(p)
	if err != nil {
		return SSHKeySelection{}, err
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

	openers := discoverSSHOpeners(ctx)

	if err := runPages(ctx, p, sshPages(keyCfg, potentialKeys, setup.SecretEchoMode(p.GetIO()), openers)); err != nil {
		return SSHKeySelection{}, err
	}

	return handleSSHKeySelection(ctx, profile, p, cfg, keyCfg, optsConfig, openers)
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

	return err == nil || isPassphraseMissing(err)
}

func handleSSHKeySelection(
	ctx context.Context,
	profile Profile,
	p *props.Props,
	cfg config.Reader,
	keyCfg *sshKeyConfig,
	optsConfig *configureSSHKeyConfig,
	openers sshOpeners,
) (SSHKeySelection, error) {
	switch keyCfg.Choice {
	case sshChoiceGenerate:
		return generateUsableKey(ctx, profile, p, cfg, keyCfg, optsConfig, openers)

	case sshChoiceAgent:
		return SSHKeySelection{Type: sshChoiceAgent}, nil

	case sshChoiceOther:
		if err := validateSSHKeyFile(p, keyCfg.Path); err != nil {
			return SSHKeySelection{}, err
		}

		return SSHKeySelection{Type: "file", Path: keyCfg.Path}, nil

	default:
		if contents, err := afero.ReadFile(p.FS, keyCfg.Choice); err == nil && isEncryptedKey(contents) {
			warnAgentNeeded(p, keyCfg.Choice)
		}

		return SSHKeySelection{Type: "file", Path: keyCfg.Choice}, nil
	}
}

// generateUsableKey generates a key only when gtb will be able to open it:
// its passphrase stored in the keychain, or an agent to add it to.
func generateUsableKey(
	ctx context.Context,
	profile Profile,
	p *props.Props,
	cfg config.Reader,
	keyCfg *sshKeyConfig,
	optsConfig *configureSSHKeyConfig,
	openers sshOpeners,
) (SSHKeySelection, error) {
	if !keyCfg.Store && !openers.agent {
		p.Logger.Warn("Not generating an SSH key: gtb could not open it without its passphrase",
			"reason", "the passphrase is not stored in an OS keychain and no ssh-agent is running",
			"next", "unlock or link an OS keychain, or start ssh-agent, then run `init "+profile.ConfigPrefix+
				"` again; or pick an existing key")

		return SSHKeySelection{}, nil
	}

	var ref string

	if keyCfg.Store {
		if err := validatePassphrase(keyCfg.Passphrase); err != nil {
			return SSHKeySelection{}, errors.Wrap(err, "failed to generate SSH key")
		}

		stored, err := storeSSHPassphrase(ctx, profile, p.Tool.Name, keyCfg.Passphrase)
		if err != nil {
			return SSHKeySelection{}, err
		}

		ref = stored
	}

	key, err := generateKey(ctx, profile, p, cfg, keyCfg.Passphrase, optsConfig)
	if err != nil {
		return SSHKeySelection{}, errors.Wrap(err, "failed to generate SSH key")
	}

	if !keyCfg.Store {
		warnAgentNeeded(p, key)
	}

	return SSHKeySelection{Type: "file", Path: key, Keychain: ref}, nil
}

// storeSSHPassphrase stores a generated key's passphrase and returns the
// "<tool>/<account>" reference config records (spec 0210 D1).
func storeSSHPassphrase(ctx context.Context, profile Profile, toolName, passphrase string) (string, error) {
	if toolName == "" {
		return "", errors.New("cannot write keychain entry without a tool name")
	}

	// A fresh deadline: ctx has been through the human-paced form.
	storeCtx, cancel := context.WithTimeout(ctx, credentials.KeychainOpTimeout)
	defer cancel()

	if err := credentials.Store(storeCtx, toolName, profile.sshPassphraseAccount(), passphrase); err != nil {
		return "", errors.WithHint(
			errors.Wrap(err, "storing the SSH key passphrase in the OS keychain"),
			"If the keychain is locked, unlock it and re-run; otherwise answer No and use ssh-agent.")
	}

	return toolName + "/" + profile.sshPassphraseAccount(), nil
}

// warnAgentNeeded names the ssh-add a passphrase-protected key needs before
// gtb's own git operations can use it (spec 0210 D6).
func warnAgentNeeded(p props.LoggerProvider, path string) {
	p.GetLogger().Warn("gtb can use this passphrase-protected key only while ssh-agent holds it",
		"run", "ssh-add "+path)
}

func validateSSHKeyFile(p *props.Props, path string) error {
	contents, err := afero.ReadFile(p.FS, path)
	if err != nil {
		return errors.Newf("could not read file: %w", err)
	}

	return validateSSHKey(path, contents, p)
}

func validateSSHKey(path string, contents []byte, p props.LoggerProvider) error {
	_, err := ssh.ParseRawPrivateKey(contents)
	if err == nil {
		p.GetLogger().Warn("Key is not protected with passphrase")

		return nil
	}

	if !isPassphraseMissing(err) {
		return errors.Newf("key is not a valid private key: %w", err)
	}

	warnAgentNeeded(p, path)

	return nil
}

func isEncryptedKey(contents []byte) bool {
	_, err := ssh.ParseRawPrivateKey(contents)

	return isPassphraseMissing(err)
}

// isPassphraseMissing detects a passphrase-protected key by the typed error,
// not its message, so an upstream wording change cannot turn a valid
// protected key into an invalid one.
func isPassphraseMissing(err error) bool {
	var missing *ssh.PassphraseMissingError

	return errors.As(err, &missing)
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
	key, err := ConfigureSSHKey(ctx, i.profile, p, cfg.View(), i.sshOpts...)
	if err != nil || key.Type == "" {
		return err
	}

	recorded := map[string]any{i.profile.sshKeyTypeKey(): key.Type}

	if key.Path != "" {
		recorded[i.profile.sshKeyPathKey()] = key.Path
	}

	if key.Keychain != "" {
		recorded[i.profile.sshKeyKeychainKey()] = key.Keychain
	}

	return setup.WriteExclusive(cfg, recorded, i.profile.sshKeyKeys())
}
