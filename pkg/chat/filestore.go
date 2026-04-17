package chat

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
)

const (
	// filePermissions is the permission mode for snapshot files.
	filePermissions = 0o600
	// dirPermissions is the permission mode for the snapshot directory.
	dirPermissions = 0o700
	// aesKeySize is the required key length for AES-256.
	aesKeySize = 32
)

// FileStoreOption configures a FileStore.
type FileStoreOption func(*fileStoreConfig)

type fileStoreConfig struct {
	key []byte
}

// WithEncryption enables AES-256-GCM encryption for stored snapshots.
// The key must be exactly 32 bytes and must come from a cryptographically
// secure source. Use [GenerateEncryptionKey] to generate one.
func WithEncryption(key []byte) FileStoreOption {
	return func(c *fileStoreConfig) { c.key = key }
}

// GenerateEncryptionKey returns a fresh 32-byte AES-256 key from
// crypto/rand, suitable for use with [WithEncryption]. Each snapshot
// store should use a distinct key obtained either from this helper or
// from an operator-controlled source such as a KMS or secret manager.
//
// Closes L-2 from
// docs/development/reports/security-audit-2026-04-17.md — using this
// helper avoids the footgun of deriving keys from human-readable
// passphrases, which have insufficient entropy for the AES-GCM
// threat model.
func GenerateEncryptionKey() ([]byte, error) {
	key := make([]byte, aesKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, errors.Wrap(err, "reading random bytes")
	}

	return key, nil
}

type fileStore struct {
	fs  afero.Fs
	dir string
	key []byte
}

// NewFileStore creates a ConversationStore that persists snapshots as JSON files.
// Files are stored in dir with 0600 permissions. The directory is created with
// 0700 permissions if it doesn't exist.
func NewFileStore(fs afero.Fs, dir string, opts ...FileStoreOption) (ConversationStore, error) {
	cfg := &fileStoreConfig{}
	for _, o := range opts {
		o(cfg)
	}

	if len(cfg.key) > 0 && len(cfg.key) != aesKeySize {
		return nil, errors.Newf("encryption key must be %d bytes, got %d", aesKeySize, len(cfg.key))
	}

	return &fileStore{fs: fs, dir: dir, key: cfg.key}, nil
}

func (s *fileStore) Save(_ context.Context, snapshot *Snapshot) error {
	if err := s.fs.MkdirAll(s.dir, dirPermissions); err != nil {
		return errors.Wrap(err, "creating snapshot directory")
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return errors.Wrap(err, "marshalling snapshot")
	}

	if len(s.key) > 0 {
		data, err = encrypt(s.key, data)
		if err != nil {
			return errors.Wrap(err, "encrypting snapshot")
		}
	}

	path := filepath.Join(s.dir, snapshot.ID+".json")

	return errors.Wrap(afero.WriteFile(s.fs, path, data, filePermissions), "writing snapshot file")
}

func (s *fileStore) Load(_ context.Context, id string) (*Snapshot, error) {
	path := filepath.Join(s.dir, id+".json")

	data, err := afero.ReadFile(s.fs, path)
	if err != nil {
		return nil, errors.Wrap(err, "reading snapshot file")
	}

	if len(s.key) > 0 {
		data, err = decrypt(s.key, data)
		if err != nil {
			return nil, errors.Wrap(err, "decrypting snapshot")
		}
	}

	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, errors.Wrap(err, "unmarshalling snapshot")
	}

	return &snapshot, nil
}

func (s *fileStore) List(_ context.Context) ([]SnapshotSummary, error) {
	entries, err := afero.ReadDir(s.fs, s.dir)
	if err != nil {
		return nil, errors.Wrap(err, "reading snapshot directory")
	}

	var summaries []SnapshotSummary

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		summary, err := s.loadSummary(entry.Name())
		if err != nil {
			continue // skip corrupt files
		}

		summaries = append(summaries, summary)
	}

	return summaries, nil
}

func (s *fileStore) loadSummary(filename string) (SnapshotSummary, error) {
	path := filepath.Join(s.dir, filename)

	data, err := afero.ReadFile(s.fs, path)
	if err != nil {
		return SnapshotSummary{}, err
	}

	if len(s.key) > 0 {
		data, err = decrypt(s.key, data)
		if err != nil {
			return SnapshotSummary{}, err
		}
	}

	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return SnapshotSummary{}, err
	}

	// Count messages without fully parsing them
	var messages []json.RawMessage

	_ = json.Unmarshal(snapshot.Messages, &messages)

	return SnapshotSummary{
		ID:           snapshot.ID,
		Provider:     snapshot.Provider,
		Model:        snapshot.Model,
		CreatedAt:    snapshot.CreatedAt,
		MessageCount: len(messages),
	}, nil
}

func (s *fileStore) Delete(_ context.Context, id string) error {
	path := filepath.Join(s.dir, id+".json")

	return errors.Wrap(s.fs.Remove(path), "deleting snapshot file")
}

// encrypt uses AES-256-GCM to encrypt plaintext. The nonce is prepended to
// the ciphertext so decrypt can extract it.
func encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decrypt extracts the nonce from the front of ciphertext and decrypts
// using AES-256-GCM.
func decrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	return gcm.Open(nil, nonce, ciphertext, nil)
}
