package forge

import (
	"os"
	"path/filepath"
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"

	"github.com/cockroachdb/errors"
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

func TestGenerateAndDiscoverKey(t *testing.T) {
	fs := afero.NewMemMapFs()
	homeDir := "/home/testuser"
	t.Setenv("HOME", homeDir)

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	mockPassphraseForm := func(s *string) *huh.Form {
		*s = ""

		return nil
	}
	mockUploadForm := func(b *bool) *huh.Form {
		*b = false

		return nil
	}

	keyPath, err := generateKey(gitHubProfile, p, testutil.ViewFromYAML(t, ""),
		WithPassphraseForm(mockPassphraseForm),
		WithUploadConfirmForm(mockUploadForm),
	)
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
	cfg := testutil.ViewFromYAML(t, "github:\n  token: dummy-token\n")

	km := &fakeKeyManager{}

	keyPath, err := generateKey(gitHubProfile, p, cfg,
		WithPassphraseForm(func(s *string) *huh.Form {
			*s = ""

			return nil
		}),
		WithUploadConfirmForm(func(b *bool) *huh.Form {
			*b = true

			return nil
		}),
		WithKeyManager(keyManagerFactory(km, nil)),
	)
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
	cfg := testutil.ViewFromYAML(t, "")

	km := &fakeKeyManager{err: assert.AnError}

	_, err := generateKey(gitHubProfile, p, cfg,
		WithPassphraseForm(func(s *string) *huh.Form { *s = ""; return nil }),
		WithUploadConfirmForm(func(b *bool) *huh.Form { *b = true; return nil }),
		WithKeyManager(keyManagerFactory(km, nil)),
	)
	require.Error(t, err)
}

func TestGenerateKey_PassphraseFormError(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")

	var v string

	_, err := generateKey(gitHubProfile, p, cfg,
		WithPassphraseForm(func(_ *string) *huh.Form {
			return huh.NewForm(huh.NewGroup(huh.NewInput().Value(&v)))
		}),
	)
	require.Error(t, err)
}

func TestGenerateKey_UploadFormError(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")

	var v bool

	_, err := generateKey(gitHubProfile, p, cfg,
		WithPassphraseForm(func(s *string) *huh.Form { *s = ""; return nil }),
		WithUploadConfirmForm(func(_ *bool) *huh.Form {
			return huh.NewForm(huh.NewGroup(huh.NewConfirm().Value(&v)))
		}),
	)
	require.Error(t, err)
}

// --- SSH option funcs ---

func TestWithSSHKeySelectForm(t *testing.T) {
	t.Parallel()

	called := false
	opt := WithSSHKeySelectForm(func(_ *string, _ []huh.Option[string]) *huh.Form {
		called = true

		return nil
	})
	c := &configureSSHKeyConfig{}
	opt(c)
	require.NotNil(t, c.sshKeySelectFormCreator)
	c.sshKeySelectFormCreator(nil, nil)
	assert.True(t, called)
}

func TestWithSSHKeyPathForm(t *testing.T) {
	t.Parallel()

	called := false
	opt := WithSSHKeyPathForm(func(_ *string) *huh.Form {
		called = true

		return nil
	})
	c := &configureSSHKeyConfig{}
	opt(c)
	require.NotNil(t, c.sshKeyPathFormCreator)
	c.sshKeyPathFormCreator(nil)
	assert.True(t, called)
}

func TestWithGenerateKeyOptions(t *testing.T) {
	t.Parallel()

	noop := func(_ *generateKeyConfig) {}
	opt := WithGenerateKeyOptions(noop)
	c := &configureSSHKeyConfig{}
	opt(c)
	assert.Len(t, c.generateKeyOpts, 1)
}

// --- default form creators (construction only) ---

func TestDefaultSSHKeySelectFormCreator(t *testing.T) {
	t.Parallel()

	var target string

	opts := []huh.Option[string]{huh.NewOption("a", "a")}
	form := defaultSSHKeySelectFormCreator(&target, opts)
	assert.NotNil(t, form)
}

func TestDefaultSSHKeyPathFormCreator(t *testing.T) {
	t.Parallel()

	var target string

	form := defaultSSHKeyPathFormCreator(&target)
	assert.NotNil(t, form)
}

func TestDefaultPassphraseFormCreator(t *testing.T) {
	t.Parallel()

	var pass string

	form := defaultPassphraseFormCreator(&pass)
	assert.NotNil(t, form)
}

func TestDefaultUploadConfirmFormCreator(t *testing.T) {
	t.Parallel()

	var upload bool

	form := defaultUploadConfirmFormCreator(&upload)
	assert.NotNil(t, form)
}

// --- defaultKeyManager ---

func TestDefaultKeyManager(t *testing.T) {
	t.Parallel()

	cfg := testutil.ViewFromYAML(t, "")
	km, err := defaultKeyManager(gitHubProfile)(cfg)
	require.NoError(t, err)
	assert.NotNil(t, km)
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
	keyType, keyPath, err := handleSSHKeySelection(gitHubProfile, p, cfg, "agent", &configureSSHKeyConfig{})
	require.NoError(t, err)
	assert.Equal(t, "agent", keyType)
	assert.Empty(t, keyPath)
}

func TestHandleSSHKeySelection_Default(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	keyType, keyPath, err := handleSSHKeySelection(gitHubProfile, p, cfg, "/home/user/.ssh/id_ed25519", &configureSSHKeyConfig{})
	require.NoError(t, err)
	assert.Equal(t, "file", keyType)
	assert.Equal(t, "/home/user/.ssh/id_ed25519", keyPath)
}

func TestHandleSSHKeySelection_Other_Error(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	opts := &configureSSHKeyConfig{
		sshKeyPathFormCreator: func(s *string) *huh.Form {
			*s = "/nonexistent/id_rsa"

			return nil
		},
	}
	_, _, err := handleSSHKeySelection(gitHubProfile, p, cfg, "other", opts)
	require.Error(t, err)
}

func TestHandleSSHKeySelection_Other_ValidKey(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	keyPEM := generateUnencryptedKeyPEM(t)
	require.NoError(t, afero.WriteFile(p.FS, "/test.key", keyPEM, 0o600))

	opts := &configureSSHKeyConfig{
		sshKeyPathFormCreator: func(s *string) *huh.Form {
			*s = "/test.key"

			return nil
		},
	}
	keyType, keyPath, err := handleSSHKeySelection(gitHubProfile, p, cfg, "other", opts)
	require.NoError(t, err)
	assert.Equal(t, "file", keyType)
	assert.Equal(t, "/test.key", keyPath)
}

func TestHandleSSHKeySelection_Other_InvalidKeyContent(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	require.NoError(t, afero.WriteFile(p.FS, "/garbage.key", []byte("not-a-key"), 0o600))

	opts := &configureSSHKeyConfig{
		sshKeyPathFormCreator: func(s *string) *huh.Form {
			*s = "/garbage.key"

			return nil
		},
	}
	_, _, err := handleSSHKeySelection(gitHubProfile, p, cfg, "other", opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid private key")
}

func TestHandleSSHKeySelection_Other_FormError(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")

	var v string

	opts := &configureSSHKeyConfig{
		sshKeyPathFormCreator: func(_ *string) *huh.Form {
			return huh.NewForm(huh.NewGroup(huh.NewText().Value(&v)))
		},
	}
	_, _, err := handleSSHKeySelection(gitHubProfile, p, cfg, "other", opts)
	require.Error(t, err)
}

func TestHandleSSHKeySelection_Generate(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")

	opts := &configureSSHKeyConfig{
		generateKeyOpts: []GenerateKeyOption{
			WithPassphraseForm(func(s *string) *huh.Form {
				*s = ""

				return nil
			}),
			WithUploadConfirmForm(func(b *bool) *huh.Form {
				*b = false

				return nil
			}),
		},
	}
	keyType, keyPath, err := handleSSHKeySelection(gitHubProfile, p, cfg, "generate", opts)
	require.NoError(t, err)
	assert.Equal(t, "file", keyType)
	assert.Contains(t, keyPath, ".ssh/id_testtool_")
}

func TestHandleSSHKeySelection_Generate_Error(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	roFS := afero.NewReadOnlyFs(afero.NewMemMapFs())
	p := &props.Props{
		FS:     roFS,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
	cfg := testutil.ViewFromYAML(t, "")

	opts := &configureSSHKeyConfig{
		generateKeyOpts: []GenerateKeyOption{
			WithPassphraseForm(func(s *string) *huh.Form { *s = ""; return nil }),
			WithUploadConfirmForm(func(b *bool) *huh.Form { *b = false; return nil }),
		},
	}
	_, _, err := handleSSHKeySelection(gitHubProfile, p, cfg, "generate", opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to generate SSH key")
}

// --- ConfigureSSHKey ---

func TestConfigureSSHKey_Agent(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")

	keyType, keyPath, err := ConfigureSSHKey(gitHubProfile, p, cfg,
		WithSSHKeySelectForm(func(s *string, _ []huh.Option[string]) *huh.Form {
			*s = "agent"

			return nil
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, "agent", keyType)
	assert.Empty(t, keyPath)
}

func TestConfigureSSHKey_ExistingPath(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "github:\n  ssh:\n    key:\n      path: /home/testuser/.ssh/existing_key\n")

	keyType, keyPath, err := ConfigureSSHKey(gitHubProfile, p, cfg,
		WithSSHKeySelectForm(func(_ *string, _ []huh.Option[string]) *huh.Form {
			return nil
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, "file", keyType)
	assert.Equal(t, "/home/testuser/.ssh/existing_key", keyPath)
}

func TestConfigureSSHKey_SelectFormError(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")

	var v string

	_, _, err := ConfigureSSHKey(gitHubProfile, p, cfg,
		WithSSHKeySelectForm(func(_ *string, _ []huh.Option[string]) *huh.Form {
			return huh.NewForm(huh.NewGroup(
				huh.NewSelect[string]().Options(huh.NewOption("a", "a")).Value(&v),
			))
		}),
	)
	require.Error(t, err)
}

func TestConfigureSSHKey_DiscoverError(t *testing.T) {
	t.Setenv("HOME", "/home/rohome2")

	roFS := afero.NewReadOnlyFs(afero.NewMemMapFs())
	p := &props.Props{
		FS:     roFS,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
	cfg := testutil.ViewFromYAML(t, "")

	_, _, err := ConfigureSSHKey(gitHubProfile, p, cfg)
	require.Error(t, err)
}

// --- uploadSSHKey error paths ---

func TestUploadSSHKey_UploadError(t *testing.T) {
	t.Parallel()

	km := &fakeKeyManager{err: assert.AnError}

	p := newTestProps(t)
	err := uploadSSHKey(gitHubProfile, p, km, "keyname", []byte("pubkey"))
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

	promptShown := false

	keyPath, err := generateKey(gitHubProfile, p, testutil.ViewFromYAML(t, ""),
		WithPassphraseForm(func(s *string) *huh.Form { *s = ""; return nil }),
		WithUploadConfirmForm(func(b *bool) *huh.Form {
			promptShown = true
			*b = true

			return nil
		}),
		WithKeyManager(keyManagerFactory(nil, errors.Wrap(forge.ErrNotSupported, "no key API"))),
	)

	require.NoError(t, err, "an unsupported upload is not a hard failure")
	assert.False(t, promptShown, "the upload prompt must not be shown when upload cannot work")

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

	promptShown := false

	_, err := generateKey(gitHubProfile, p, testutil.ViewFromYAML(t, ""),
		WithPassphraseForm(func(s *string) *huh.Form { *s = ""; return nil }),
		WithUploadConfirmForm(func(b *bool) *huh.Form {
			promptShown = true

			return nil
		}),
		WithKeyManager(keyManagerFactory(nil, assert.AnError)),
	)

	require.Error(t, err)
	assert.False(t, promptShown, "a failed resolution must not reach the prompt either")
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

	_, err := generateKey(gitHubProfile, p, testutil.ViewFromYAML(t, ""),
		WithPassphraseForm(func(s *string) *huh.Form { *s = ""; return nil }),
		WithUploadConfirmForm(func(b *bool) *huh.Form { *b = true; return nil }),
		WithKeyManager(func(config.Reader) (forge.KeyManager, error) {
			calls++

			return km, nil
		}),
	)

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

func TestRunForm_Nil(t *testing.T) {
	t.Parallel()
	require.NoError(t, runForm(nil))
}

func TestRunForm_NonNilErrors(t *testing.T) {
	t.Parallel()

	var v string

	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Options(huh.NewOption("a", "a")).Value(&v),
	))
	require.Error(t, runForm(form))
}
