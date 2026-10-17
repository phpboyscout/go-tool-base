// Package sshagenttest serves an in-memory ssh-agent for tests, and pins
// SSH_AUTH_SOCK so a developer's own agent cannot change a result.
package sshagenttest

import (
	"crypto"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/keygen"
	"golang.org/x/crypto/ssh/agent"
)

// keyFileMode is what ssh refuses to load a private key without.
const keyFileMode = 0o600

// Start serves an empty agent keyring on a fresh socket, points SSH_AUTH_SOCK
// at it for the rest of the test, and returns the keyring.
func Start(t *testing.T) agent.Agent {
	t.Helper()

	// A unix socket path is limited to about 104 bytes, which t.TempDir under a
	// long test name exceeds.
	dir, err := os.MkdirTemp("", "agent")
	if err != nil {
		t.Fatalf("sshagenttest: %v", err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	sock := filepath.Join(dir, "s")

	listener, err := new(net.ListenConfig).Listen(t.Context(), "unix", sock)
	if err != nil {
		t.Fatalf("sshagenttest: %v", err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	keyring := agent.NewKeyring()

	go func() {
		for {
			conn, err := listener.Accept()
			if errors.Is(err, net.ErrClosed) {
				return
			}

			if err != nil {
				continue
			}

			go func() {
				defer func() { _ = conn.Close() }()

				_ = agent.ServeAgent(keyring, conn)
			}()
		}
	}()

	t.Setenv("SSH_AUTH_SOCK", sock)

	return keyring
}

// None pins SSH_AUTH_SOCK to empty for the rest of the test.
func None(t *testing.T) {
	t.Helper()

	t.Setenv("SSH_AUTH_SOCK", "")
}

// WriteEncryptedKey writes a passphrase-protected ed25519 key and its .pub
// beside it at path, as the init wizard does, and returns the private key.
func WriteEncryptedKey(t *testing.T, path, passphrase string) crypto.PrivateKey {
	t.Helper()

	pair, err := keygen.New(path, keygen.WithPassphrase(passphrase), keygen.WithKeyType(keygen.Ed25519))
	if err != nil {
		t.Fatalf("sshagenttest: %v", err)
	}

	if err := os.WriteFile(path, pair.RawProtectedPrivateKey(), keyFileMode); err != nil {
		t.Fatalf("sshagenttest: %v", err)
	}

	if err := os.WriteFile(path+".pub", pair.RawAuthorizedKey(), keyFileMode); err != nil {
		t.Fatalf("sshagenttest: %v", err)
	}

	return pair.PrivateKey()
}
