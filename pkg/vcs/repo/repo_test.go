package repo

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

func TestSourceIs(t *testing.T) {
	repo := &Repo{}
	repo.SetSource(SourceLocal)
	assert.True(t, repo.SourceIs(SourceLocal))
	assert.False(t, repo.SourceIs(SourceMemory))
}

func TestSetSource(t *testing.T) {
	repo := &Repo{}
	repo.SetSource(SourceMemory)
	assert.Equal(t, SourceMemory, repo.source)
}

func TestSetRepo(t *testing.T) {
	repo := &Repo{}
	gitRepo := &git.Repository{}
	repo.SetRepo(gitRepo)
	assert.Equal(t, gitRepo, repo.repo)
}

func TestSetKey(t *testing.T) {
	repo := &Repo{}
	key := &ssh.PublicKeys{}
	repo.SetKey(key)
	assert.Equal(t, key, repo.auth)
}

func TestSetTreeAndWithTree(t *testing.T) {
	repo := &Repo{}
	tree := &git.Worktree{}
	repo.SetTree(tree)
	err := repo.WithTree(func(wt *git.Worktree) error {
		assert.Equal(t, tree, wt)

		return nil
	})
	assert.NoError(t, err)
}


func TestGetSSHKey(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Generate RSA key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if assert.NoError(t, err, "unable to generate RSA key") {

		// Encode private key to PEM format
		privateKeyPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
		})

		// Write the PEM-encoded key to the afero filesystem
		err = afero.WriteFile(fs, "id_rsa", privateKeyPEM, 0600)
		assert.NoError(t, err)

		// Read the key using GetSSHKey
		key, err := GetSSHKey("id_rsa", fs)
		assert.NoError(t, err)
		assert.NotNil(t, key)
	}
}

