package forge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// --- discoverSSHKeys ---

func TestDiscoverSSHKeys_Coverage(t *testing.T) {
	fs := afero.NewMemMapFs()

	homeDir := "/home/testuser"
	t.Setenv("HOME", homeDir)

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	sshDir := filepath.Join(homeDir, ".ssh")
	require.NoError(t, fs.MkdirAll(sshDir, 0o700))

	keys, err := discoverSSHKeys(p)
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestDiscoverSSHKeys_CreatesMissingDir(t *testing.T) {
	homeDir := "/home/freshuser"
	t.Setenv("HOME", homeDir)

	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	keys, err := discoverSSHKeys(p)
	require.NoError(t, err)
	assert.Empty(t, keys)

	exists, _ := afero.DirExists(fs, filepath.Join(homeDir, ".ssh"))
	assert.True(t, exists, "discoverSSHKeys should create the missing .ssh dir")
}

func TestDiscoverSSHKeys_FindsKey(t *testing.T) {
	homeDir := "/home/keyuser"
	t.Setenv("HOME", homeDir)

	fs := afero.NewMemMapFs()
	sshDir := filepath.Join(homeDir, ".ssh")
	require.NoError(t, fs.MkdirAll(sshDir, 0o700))
	keyPath := filepath.Join(sshDir, "id_ed25519")
	require.NoError(t, afero.WriteFile(fs, keyPath, generateUnencryptedKeyPEM(t), 0o600))

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	keys, err := discoverSSHKeys(p)
	require.NoError(t, err)

	found := false

	for _, k := range keys {
		if k.Value == keyPath {
			found = true
		}
	}

	assert.True(t, found, "the valid key should be discovered")
}

func TestDiscoverSSHKeys_MkdirError(t *testing.T) {
	homeDir := "/home/rohome"
	t.Setenv("HOME", homeDir)

	roFS := afero.NewReadOnlyFs(afero.NewMemMapFs())
	p := &props.Props{
		FS:     roFS,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	_, err := discoverSSHKeys(p)
	require.Error(t, err)
}

// --- generateKey / discovery ---

// noUpload is a key manager that cannot upload: the stage generates the key
// and never asks about uploading it.
func noUpload() *configureSSHKeyConfig {
	return &configureSSHKeyConfig{keyManagerFactory: keyManagerFactory(nil, errors.Wrap(forge.ErrNotSupported, "no key API"))}
}

func TestGenerateAndDiscoverKey(t *testing.T) {
	fs := afero.NewMemMapFs()
	homeDir := "/home/testuser"
	t.Setenv("HOME", homeDir)

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	// The key manager is injected so the test does not depend on a forge
	// adapter being linked; this test is about generation and discovery.
	keyPath, err := generateKey(t.Context(), gitHubProfile, p, testutil.ViewFromYAML(t, ""), testPassphrase, noUpload())
	require.NoError(t, err)
	assert.Contains(t, keyPath, ".ssh/id_testtool_")

	exists, _ := afero.Exists(fs, keyPath)
	assert.True(t, exists, "Private key should exist")
	exists, _ = afero.Exists(fs, keyPath+".pub")
	assert.True(t, exists, "Public key should exist")

	keys, err := discoverSSHKeys(p)
	require.NoError(t, err)
	assert.NotEmpty(t, keys)

	found := false

	for _, k := range keys {
		if k.Value == keyPath {
			found = true

			break
		}
	}

	assert.True(t, found, "Generated key should be discovered")
}

func TestGenerateKey_Upload(t *testing.T) {
	fs := afero.NewMemMapFs()
	homeDir := "/home/testuser"
	t.Setenv("HOME", homeDir)

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
	p.IO, _ = answersIO("y")
	cfg := testutil.ViewFromYAML(t, "github:\n  token: dummy-token\n")

	km := &fakeKeyManager{}

	keyPath, err := generateKey(t.Context(), gitHubProfile, p, cfg, testPassphrase,
		&configureSSHKeyConfig{keyManagerFactory: keyManagerFactory(km, nil)})
	require.NoError(t, err)
	assert.True(t, km.uploaded, "SSH key should be uploaded via the KeyManager")

	exists, _ := afero.Exists(fs, keyPath)
	assert.True(t, exists)
}

func TestGenerateKey_UploadError(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
	p.IO, _ = answersIO("y")
	cfg := testutil.ViewFromYAML(t, "")

	km := &fakeKeyManager{err: assert.AnError}

	_, err := generateKey(t.Context(), gitHubProfile, p, cfg, testPassphrase,
		&configureSSHKeyConfig{keyManagerFactory: keyManagerFactory(km, nil)})
	require.Error(t, err)
}

// A passphrase the form did not collect (accessible mode with no terminal
// leaves it blank) is refused before any key is written.
func TestGenerateKey_RefusesAShortPassphrase(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")

	_, err := generateKey(t.Context(), gitHubProfile, p, cfg, "", noUpload())
	require.ErrorIs(t, err, ErrPassphraseTooShort)

	keys, derr := discoverSSHKeys(p)
	require.NoError(t, derr)
	assert.Empty(t, keys, "no key is written without a passphrase")
}

func TestGenerateKey_UploadFormRefusesANonInteractiveRun(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	p.IO = nonInteractiveIO()
	cfg := testutil.ViewFromYAML(t, "")

	_, err := generateKey(t.Context(), gitHubProfile, p, cfg, testPassphrase,
		&configureSSHKeyConfig{keyManagerFactory: keyManagerFactory(&fakeKeyManager{}, nil)})
	require.ErrorIs(t, err, setup.ErrNonInteractive)
}

// --- options ---

func TestWithKeyManager(t *testing.T) {
	t.Parallel()

	c := &configureSSHKeyConfig{}
	withKeyManager(keyManagerFactory(&fakeKeyManager{}, nil))(c)
	require.NotNil(t, c.keyManagerFactory)
}

// --- sshForm: the selector, then the page the choice needs ---

// Only the TUI honours a hide function, so the pages are proved with keys:
// "other" opens the path page and nothing else.
func TestSSHForm_OtherAsksForThePath(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	p := newTestProps(t)
	keys, err := discoverSSHKeys(p)
	require.NoError(t, err)

	options := keys
	options = append(options,
		huh.NewOption("Generate a new SSH key", sshChoiceGenerate),
		huh.NewOption("I use ssh-agent to handle my keys", sshChoiceAgent),
		huh.NewOption("Enter path to key manually", sshChoiceOther),
	)

	seqs := append(sshChoiceKeys(t, p, sshChoiceOther), "/keys/id_x", formtest.Enter)
	p.IO = formtest.TUI(formtest.Keys(seqs...))

	cfg := &sshKeyConfig{}
	require.NoError(t, setup.RunForm(ctx, p, sshForm(cfg, options)))
	assert.Equal(t, sshChoiceOther, cfg.Choice)
	assert.Equal(t, "/keys/id_x", cfg.Path)
	assert.Empty(t, cfg.Passphrase)
}

func TestSSHForm_AgentAtAccessiblePrompts(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	options := []huh.Option[string]{
		huh.NewOption("Generate a new SSH key", sshChoiceGenerate),
		huh.NewOption("I use ssh-agent to handle my keys", sshChoiceAgent),
		huh.NewOption("Enter path to key manually", sshChoiceOther),
	}

	// Accessible mode asks the hidden pages too; blank answers leave them be.
	io, out := answersIO(sshChoiceNumber(t, p, sshChoiceAgent), "")
	p.IO = io

	cfg := &sshKeyConfig{}
	require.NoError(t, setup.RunForm(t.Context(), p, sshForm(cfg, options)))
	assert.Equal(t, sshChoiceAgent, cfg.Choice)
	assert.Contains(t, out.String(), "Select SSH key")
}

func TestValidatePassphrase(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, validatePassphrase("short"), ErrPassphraseTooShort)
	assert.Contains(t, errors.FlattenHints(validatePassphrase("short")), "at least 12 characters", "the message reads as one sentence (a manual-round niggle: it used to read \"12 characters: passphrase must be at least\")")
	require.NoError(t, validatePassphrase(testPassphrase))
}

// --- defaultKeyManager ---

// TestDefaultKeyManager resolves the key manager through the registry for a
// type this test registers itself: the framework links no forge adapter, so a
// real type would be a miss here (that miss is TestDefaultKeyManager_Unlinked).
func TestDefaultKeyManager(t *testing.T) {
	t.Parallel()

	registerTestForge(t, "km-test", capturingProvider{})

	cfg := testutil.ViewFromYAML(t, "")
	km, err := defaultKeyManager(Profile{Provider: "km-test", Label: "KM Test"})(t.Context(), cfg)
	require.NoError(t, err)
	assert.NotNil(t, km)
}

// TestDefaultKeyManager_Unlinked is the registry miss a hand-wired tool sees
// when it enables a forge whose adapter it did not import.
func TestDefaultKeyManager_Unlinked(t *testing.T) {
	t.Parallel()

	cfg := testutil.ViewFromYAML(t, "")
	_, err := defaultKeyManager(gitHubProfile)(t.Context(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no release provider registered")
}

// --- validateSSHKey ---

func TestValidateSSHKey_Valid(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	keyPEM := generateUnencryptedKeyPEM(t)
	require.NoError(t, validateSSHKey(keyPEM, p))
}

func TestValidateSSHKey_Invalid(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	err := validateSSHKey([]byte("not-a-key"), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid private key")
}

func TestValidateSSHKey_PassphraseProtected(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	require.NoError(t, validateSSHKey(passphraseProtectedKeyPEM(t), p))
}

// --- isValidSSHKey ---

func TestIsValidSSHKey_Valid(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	keyPEM := generateUnencryptedKeyPEM(t)
	require.NoError(t, afero.WriteFile(fs, "/valid.key", keyPEM, 0o600))
	assert.True(t, isValidSSHKey(fs, "/valid.key"))
}

func TestIsValidSSHKey_Invalid(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/bad.key", []byte("garbage"), 0o600))
	assert.False(t, isValidSSHKey(fs, "/bad.key"))
}

func TestIsValidSSHKey_ReadError(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	assert.False(t, isValidSSHKey(fs, "/nonexistent.key"))
}

func TestIsValidSSHKey_PassphraseProtected(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/protected.key", passphraseProtectedKeyPEM(t), 0o600))
	assert.True(t, isValidSSHKey(fs, "/protected.key"))
}

// --- handleSSHKeySelection ---

func TestHandleSSHKeySelection_Agent(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	keyType, keyPath, err := handleSSHKeySelection(t.Context(), gitHubProfile, p, cfg, &sshKeyConfig{Choice: sshChoiceAgent}, &configureSSHKeyConfig{})
	require.NoError(t, err)
	assert.Equal(t, "agent", keyType)
	assert.Empty(t, keyPath)
}

func TestHandleSSHKeySelection_Default(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	keyType, keyPath, err := handleSSHKeySelection(t.Context(), gitHubProfile, p, cfg, &sshKeyConfig{Choice: "/home/user/.ssh/id_ed25519"}, &configureSSHKeyConfig{})
	require.NoError(t, err)
	assert.Equal(t, "file", keyType)
	assert.Equal(t, "/home/user/.ssh/id_ed25519", keyPath)
}

func TestHandleSSHKeySelection_Other_Error(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	_, _, err := handleSSHKeySelection(t.Context(), gitHubProfile, p, cfg, &sshKeyConfig{Choice: sshChoiceOther, Path: "/nonexistent/id_rsa"}, &configureSSHKeyConfig{})
	require.Error(t, err)
}

func TestHandleSSHKeySelection_Other_ValidKey(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	keyPEM := generateUnencryptedKeyPEM(t)
	require.NoError(t, afero.WriteFile(p.FS, "/test.key", keyPEM, 0o600))

	keyType, keyPath, err := handleSSHKeySelection(t.Context(), gitHubProfile, p, cfg, &sshKeyConfig{Choice: sshChoiceOther, Path: "/test.key"}, &configureSSHKeyConfig{})
	require.NoError(t, err)
	assert.Equal(t, "file", keyType)
	assert.Equal(t, "/test.key", keyPath)
}

func TestHandleSSHKeySelection_Other_InvalidKeyContent(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	require.NoError(t, afero.WriteFile(p.FS, "/bad.key", []byte("not a key"), 0o600))

	_, _, err := handleSSHKeySelection(t.Context(), gitHubProfile, p, cfg, &sshKeyConfig{Choice: sshChoiceOther, Path: "/bad.key"}, &configureSSHKeyConfig{})
	require.Error(t, err)
}

func TestHandleSSHKeySelection_Generate(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	p.IO, _ = answersIO("n")
	cfg := testutil.ViewFromYAML(t, "")

	keyType, keyPath, err := handleSSHKeySelection(t.Context(), gitHubProfile, p, cfg,
		&sshKeyConfig{Choice: sshChoiceGenerate, Passphrase: testPassphrase},
		&configureSSHKeyConfig{keyManagerFactory: keyManagerFactory(&fakeKeyManager{}, nil)})
	require.NoError(t, err)
	assert.Equal(t, "file", keyType)
	assert.Contains(t, keyPath, ".ssh/id_testtool_")
}

func TestHandleSSHKeySelection_Generate_Error(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	p.FS = afero.NewReadOnlyFs(afero.NewMemMapFs())
	cfg := testutil.ViewFromYAML(t, "")

	_, _, err := handleSSHKeySelection(t.Context(), gitHubProfile, p, cfg,
		&sshKeyConfig{Choice: sshChoiceGenerate, Passphrase: testPassphrase}, noUpload())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to generate SSH key")
}

// --- ConfigureSSHKey ---

func TestConfigureSSHKey_Agent(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	p.IO, _ = answersIO(sshChoiceNumber(t, p, sshChoiceAgent), "")
	cfg := testutil.ViewFromYAML(t, "")

	keyType, keyPath, err := ConfigureSSHKey(t.Context(), gitHubProfile, p, cfg)
	require.NoError(t, err)
	assert.Equal(t, "agent", keyType)
	assert.Empty(t, keyPath)
}

// A recorded key is the selector's default: a blank answer keeps it.
func TestConfigureSSHKey_ExistingPath(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	keyPEM := generateUnencryptedKeyPEM(t)
	require.NoError(t, p.FS.MkdirAll("/home/testuser/.ssh", 0o700))
	require.NoError(t, afero.WriteFile(p.FS, "/home/testuser/.ssh/id_existing", keyPEM, 0o600))

	p.IO, _ = answersIO("", "")
	cfg := testutil.ViewFromYAML(t, "github:\n  ssh:\n    key:\n      path: /home/testuser/.ssh/id_existing\n")

	keyType, keyPath, err := ConfigureSSHKey(t.Context(), gitHubProfile, p, cfg)
	require.NoError(t, err)
	assert.Equal(t, "file", keyType)
	assert.Equal(t, "/home/testuser/.ssh/id_existing", keyPath)
}

func TestConfigureSSHKey_RefusesANonInteractiveRun(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	p.IO = nonInteractiveIO()
	cfg := testutil.ViewFromYAML(t, "")

	_, _, err := ConfigureSSHKey(t.Context(), gitHubProfile, p, cfg)
	require.ErrorIs(t, err, setup.ErrNonInteractive)
}

func TestConfigureSSHKey_DiscoverError(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	// A read-only FS makes MkdirAll of ~/.ssh fail in discoverSSHKeys.
	p.FS = afero.NewReadOnlyFs(afero.NewMemMapFs())
	cfg := testutil.ViewFromYAML(t, "")

	_, _, err := ConfigureSSHKey(t.Context(), gitHubProfile, p, cfg)
	require.Error(t, err)
}

// --- uploadSSHKey error paths ---

func TestUploadSSHKey_UploadError(t *testing.T) {
	t.Parallel()

	km := &fakeKeyManager{err: assert.AnError}

	p := newTestProps(t)
	err := uploadSSHKey(t.Context(), gitHubProfile, p, km, "keyname", []byte("pubkey"))
	require.Error(t, err)
}

// --- key-manager resolution happens before the upload prompt (0186 D7) ---

// TestGenerateKey_NotSupported_SkipsTheUploadPrompt is the D7 guard. The old
// order asked "upload this key?", then discovered the provider could not upload
// and overruled the answer with an add-it-manually warning. Resolution now
// happens first, so the question is never asked — and the key is still
// generated and saved, because that stands on its own.
func TestGenerateKey_NotSupported_SkipsTheUploadPrompt(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	// Nobody is at the terminal: were the upload prompt shown, RunForm would
	// refuse the run.
	p.IO = nonInteractiveIO()

	keyPath, err := generateKey(t.Context(), gitHubProfile, p, testutil.ViewFromYAML(t, ""), testPassphrase, noUpload())

	require.NoError(t, err, "an unsupported upload is not a hard failure")

	exists, _ := afero.Exists(fs, keyPath)
	assert.True(t, exists, "the key is still generated and saved")
}

// TestGenerateKey_KeyManagerError_IsFatal separates a real resolution failure
// from the ErrNotSupported degradation above: anything else must surface.
func TestGenerateKey_KeyManagerError_IsFatal(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	p.IO = nonInteractiveIO()

	_, err := generateKey(t.Context(), gitHubProfile, p, testutil.ViewFromYAML(t, ""), testPassphrase,
		&configureSSHKeyConfig{keyManagerFactory: keyManagerFactory(nil, assert.AnError)})

	require.Error(t, err)
	require.NotErrorIs(t, err, setup.ErrNonInteractive, "a failed resolution must not reach the prompt either")
}

// TestGenerateKey_ResolvesTheKeyManagerOnce pins the other half of D7: the
// manager resolved for the capability check is the one used for the upload,
// rather than being constructed a second time.
func TestGenerateKey_ResolvesTheKeyManagerOnce(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	km := &fakeKeyManager{}
	calls := 0

	p.IO, _ = answersIO("y")

	_, err := generateKey(t.Context(), gitHubProfile, p, testutil.ViewFromYAML(t, ""), testPassphrase,
		&configureSSHKeyConfig{keyManagerFactory: func(context.Context, config.Reader) (forge.KeyManager, error) {
			calls++

			return km, nil
		}})

	require.NoError(t, err)
	assert.True(t, km.uploaded)
	assert.Equal(t, 1, calls, "the key manager must be constructed once, not once to test and once to upload")
}

// --- generateAndSaveSSHKey ---

// pubWriteFailFs wraps an afero.Fs and fails Create/OpenFile for paths ending in
// ".pub", so the public-key write step fails while the private-key write
// succeeds.
type pubWriteFailFs struct {
	afero.Fs
}

func (f pubWriteFailFs) Create(name string) (afero.File, error) {
	if filepath.Ext(name) == ".pub" {
		return nil, assert.AnError
	}

	return f.Fs.Create(name)
}

func (f pubWriteFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if filepath.Ext(name) == ".pub" {
		return nil, assert.AnError
	}

	return f.Fs.OpenFile(name, flag, perm)
}

func TestGenerateAndSaveSSHKey_PubWriteError(t *testing.T) {
	t.Parallel()

	fs := pubWriteFailFs{Fs: afero.NewMemMapFs()}
	_, err := generateAndSaveSSHKey(fs, "/home/u/.ssh/id_test", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write public key")
}

func TestGenerateAndSaveSSHKey_WriteError(t *testing.T) {
	t.Parallel()

	roFS := afero.NewReadOnlyFs(afero.NewMemMapFs())
	_, err := generateAndSaveSSHKey(roFS, "/tmp/key", "passphrase-1234")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write private key")
}

func TestGenerateAndSaveSSHKey_Success(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	pub, err := generateAndSaveSSHKey(fs, "/home/u/.ssh/id_test", "")
	require.NoError(t, err)
	assert.NotEmpty(t, pub)

	exists, _ := afero.Exists(fs, "/home/u/.ssh/id_test")
	assert.True(t, exists)
	exists, _ = afero.Exists(fs, "/home/u/.ssh/id_test.pub")
	assert.True(t, exists)
}

// --- runForm ---
