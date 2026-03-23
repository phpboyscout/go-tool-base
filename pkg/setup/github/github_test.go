package github

import (
	"path/filepath"
	"testing"

	"github.com/charmbracelet/huh"
	"github.com/phpboyscout/gtb/pkg/logger"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockVCS "github.com/phpboyscout/gtb/mocks/pkg/vcs/github"
	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/phpboyscout/gtb/pkg/props"
	githubvcs "github.com/phpboyscout/gtb/pkg/vcs/github"
)

func TestDiscoverSSHKeys_Coverage(t *testing.T) {
	// Setup Afero FS
	fs := afero.NewMemMapFs()

	// Mock HOME directory
	homeDir := "/home/testuser"
	t.Setenv("HOME", homeDir)

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	// Create .ssh directory and some keys
	sshDir := filepath.Join(homeDir, ".ssh")
	err := fs.MkdirAll(sshDir, 0700)
	require.NoError(t, err)

	keys, err := discoverSSHKeys(p)
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestGenerateAndDiscoverKey(t *testing.T) {
	// Setup
	fs := afero.NewMemMapFs()
	homeDir := "/home/testuser"
	t.Setenv("HOME", homeDir)

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
	cfg := viper.New()

	// Mock Forms to avoid interaction
	mockPassphraseForm := func(s *string) *huh.Form {
		*s = "" // no passphrase
		return nil
	}
	mockUploadForm := func(b *bool) *huh.Form {
		*b = false // no upload
		return nil
	}

	// 1. Generate Key
	keyPath, err := generateKey(p, config.NewContainerFromViper(nil, cfg),
		WithPassphraseForm(mockPassphraseForm),
		WithUploadConfirmForm(mockUploadForm),
	)
	require.NoError(t, err)
	assert.Contains(t, keyPath, ".ssh/id_testtool_")

	// Verify file exists in FS
	exists, _ := afero.Exists(fs, keyPath)
	assert.True(t, exists, "Private key should exist")
	exists, _ = afero.Exists(fs, keyPath+".pub")
	assert.True(t, exists, "Public key should exist")

	// 2. Discover Keys
	keys, err := discoverSSHKeys(p)
	require.NoError(t, err)
	assert.NotEmpty(t, keys)

	// Verify our generated key is found
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
	// Setup
	fs := afero.NewMemMapFs()
	homeDir := "/home/testuser"
	t.Setenv("HOME", homeDir)

	l := logger.NewNoop()
	p := &props.Props{
		FS:     fs,
		Logger: l,
		Tool:   props.Tool{Name: "testtool"},
	}
	cfg := viper.New()
	cfg.Set("github.token", "dummy-token") // To ensure config is validish

	// Mock newGitHubClientFunc
	mockClient := mockVCS.NewMockGitHubClient(t)
	mockClient.EXPECT().UploadKey(mock.Anything, mock.Anything, mock.Anything).Return(nil)

	originalNewClientFunc := newGitHubClientFunc
	defer func() { newGitHubClientFunc = originalNewClientFunc }()
	newGitHubClientFunc = func(cfg config.Containable) (githubvcs.GitHubClient, error) {
		return mockClient, nil
	}

	// Mock Forms
	mockPassphraseForm := func(s *string) *huh.Form {
		*s = ""
		return nil
	}
	mockUploadForm := func(b *bool) *huh.Form {
		*b = true // YES upload
		return nil
	}

	// Run
	keyPath, err := generateKey(p, config.NewContainerFromViper(nil, cfg),
		WithPassphraseForm(mockPassphraseForm),
		WithUploadConfirmForm(mockUploadForm),
	)
	require.NoError(t, err)

	// Check files
	exists, _ := afero.Exists(fs, keyPath)
	assert.True(t, exists)
}

func TestGitHubInitialiser(t *testing.T) {
	fs := afero.NewMemMapFs()
	homeDir := "/home/testuser"
	t.Setenv("HOME", homeDir)

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	// Mock GH Login
	originalGHLogin := ghLoginFunc
	defer func() { ghLoginFunc = originalGHLogin }()
	ghLoginFunc = func(hostname string) (string, error) {
		return "mock-token", nil
	}

	cfg := viper.New()
	cfg.SetFs(fs)

	init := NewGitHubInitialiser(p, false, true) // SkipKey=true
	err := init.Configure(p, config.NewContainerFromViper(nil, cfg))
	require.NoError(t, err)

	assert.Equal(t, "mock-token", cfg.GetString("github.auth.value"))
}
func TestIsGitHubConfigured(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	tests := []struct {
		name     string
		setup    func(t *testing.T, cfg *viper.Viper)
		expected bool
	}{
		{
			name:     "empty config",
			setup:    func(t *testing.T, cfg *viper.Viper) {},
			expected: false,
		},
		{
			name: "token provided",
			setup: func(t *testing.T, cfg *viper.Viper) {
				cfg.Set("github.auth.value", "some-token")
				cfg.Set("github.ssh.key.type", "agent")
			},
			expected: true,
		},
		{
			name: "env var name provided but not set",
			setup: func(t *testing.T, cfg *viper.Viper) {
				cfg.Set("github.auth.env", "TEST_GH_TOKEN")
				cfg.Set("github.ssh.key.type", "agent")
				t.Setenv("TEST_GH_TOKEN", "")
			},
			expected: false,
		},
		{
			name: "env var name provided and set",
			setup: func(t *testing.T, cfg *viper.Viper) {
				cfg.Set("github.auth.env", "TEST_GH_TOKEN")
				cfg.Set("github.ssh.key.type", "agent")
				t.Setenv("TEST_GH_TOKEN", "secret")
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := viper.New()
			tt.setup(t, cfg)
			init := NewGitHubInitialiser(p, false, false)
			assert.Equal(t, tt.expected, init.IsConfigured(config.NewContainerFromViper(nil, cfg)))
		})
	}
}
