package repo

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepo_OpenLocal(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize a real git repo
	_, err := git.PlainInit(tmpDir, false)
	require.NoError(t, err)

	r := &Repo{} // Manually initialize as NewRepo might entail config deps
	repo, tree, err := r.OpenLocal(tmpDir, "master")
	require.NoError(t, err)
	assert.NotNil(t, repo)
	assert.NotNil(t, tree)
	assert.True(t, r.SourceIs(SourceLocal))
}

func TestRepo_Commit(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := git.PlainInit(tmpDir, false)
	require.NoError(t, err)

	r := &Repo{}
	_, _, err = r.OpenLocal(tmpDir, "master")
	require.NoError(t, err)

	// Create a file
	testFile := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(testFile, []byte("content"), 0644)
	require.NoError(t, err)

	// Add to index
	_, err = r.tree.Add("test.txt")
	require.NoError(t, err)

	opts := &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Now(),
		},
	}

	hash, err := r.Commit("initial commit", opts)
	require.NoError(t, err)
	assert.False(t, hash.IsZero())

	// Verify HEAD matches commit hash
	head, err := r.repo.Head()
	require.NoError(t, err)
	assert.Equal(t, hash, head.Hash())
}

func TestRepo_CreateRemote(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := git.PlainInit(tmpDir, false)
	require.NoError(t, err)

	r := &Repo{}
	_, _, err = r.OpenLocal(tmpDir, "master")
	require.NoError(t, err)

	remoteName := "origin"
	remoteURL := "https://example.com/repo.git"

	remote, err := r.CreateRemote(remoteName, []string{remoteURL})
	require.NoError(t, err)
	assert.NotNil(t, remote)
	assert.Equal(t, remoteName, remote.Config().Name)
	assert.Equal(t, remoteURL, remote.Config().URLs[0])

	// Test Retrieve Remote
	rem, err := r.Remote(remoteName)
	require.NoError(t, err)
	assert.Equal(t, remoteName, rem.Config().Name)
}

func TestRepo_CheckoutCommit(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := git.PlainInit(tmpDir, false)
	require.NoError(t, err)

	r := &Repo{}
	_, _, err = r.OpenLocal(tmpDir, "master")
	require.NoError(t, err)

	// Commit 1
	testFile := filepath.Join(tmpDir, "test1.txt")
	_ = os.WriteFile(testFile, []byte("1"), 0644)
	_, _ = r.tree.Add("test1.txt")
	hash1, err := r.Commit("commit 1", &git.CommitOptions{
		Author: &object.Signature{Name: "T", Email: "e", When: time.Now()},
	})
	require.NoError(t, err)

	// Commit 2
	testFile2 := filepath.Join(tmpDir, "test2.txt")
	_ = os.WriteFile(testFile2, []byte("2"), 0644)
	_, _ = r.tree.Add("test2.txt")
	hash2, err := r.Commit("commit 2", &git.CommitOptions{
		Author: &object.Signature{Name: "T", Email: "e", When: time.Now()},
	})
	require.NoError(t, err)

	// Verify we are at hash2
	head, _ := r.repo.Head()
	assert.Equal(t, hash2, head.Hash())

	// Checkout Commit 1
	err = r.CheckoutCommit(hash1)
	require.NoError(t, err)

	head, _ = r.repo.Head()
	assert.Equal(t, hash1, head.Hash())
}

func TestGetSSHKey_EdgeCases(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Case 1: File not found
	_, err := GetSSHKey("/missing", fs)
	assert.Error(t, err)

	// Case 2: Is Directory
	err = fs.MkdirAll("/dir", 0755)
	require.NoError(t, err)
	_, err = GetSSHKey("/dir", fs)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Could not open SSH key")

	// Case 3: Valid Key (No Passphrase)
	keyPath := "/id_rsa"
	// Generate a real key
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	privKeyBytes := x509.MarshalPKCS1PrivateKey(privKey)
	pemBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privKeyBytes,
	}
	pemData := pem.EncodeToMemory(pemBlock)

	err = afero.WriteFile(fs, keyPath, pemData, 0600)
	require.NoError(t, err)

	pubKeys, err := GetSSHKey(keyPath, fs)
	require.NoError(t, err)
	assert.NotNil(t, pubKeys)
	assert.Equal(t, "git", pubKeys.User)
}
