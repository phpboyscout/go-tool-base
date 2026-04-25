package chat

// This file implements [ConversationStore] as a JSON-on-disk snapshot
// store with optional AES-256-GCM encryption.
//
// # Snapshot ID contract
//
// Every snapshot identifier that reaches [FileStore.Save],
// [FileStore.Load], or [FileStore.Delete] MUST be a canonical
// google/uuid string — 36 lowercase-hex characters in 8-4-4-4-12
// hyphenated form. This is enforced by two independent defence layers:
//
//  1. A shape check against [uuidCanonicalPattern] performed by
//     [ValidateSnapshotID]. This forecloses the entire path-traversal
//     class at the input layer — no "..", no "/", no "\", no NUL
//     bytes, no Unicode lookalikes.
//  2. A path-containment check in [fileStore.resolveStorePath] that
//     verifies the cleaned absolute target lies beneath the store
//     directory via filepath.Rel. Defence-in-depth against
//     platform-specific path quirks and future regex relaxation.
//
// Both layers are applied to every exported method that touches the
// filesystem. [fileStore.List] is intentionally robust rather than
// strict: files with non-canonical names in the store directory are
// logged at DEBUG and skipped, so one corrupt file cannot break
// enumeration for the user.
//
// See docs/development/specs/2026-04-17-snapshot-id-validation.md
// (H-1 from the 2026-04-17 security audit) for the full threat model
// and design rationale.

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

const (
	// filePermissions is the permission mode for snapshot files.
	filePermissions = 0o600
	// dirPermissions is the permission mode for the snapshot directory.
	dirPermissions = 0o700
	// aesKeySize is the required key length for AES-256.
	aesKeySize = 32
	// truncatedIDMaxLen is the longest ID snippet echoed in error
	// messages; prevents log amplification from attacker-controlled input.
	truncatedIDMaxLen = 32
)

// uuidCanonicalPattern matches the canonical 8-4-4-4-12 hex form produced
// by google/uuid.New(). Any snapshot identifier that does not match this
// pattern is rejected — see [ValidateSnapshotID] and the H-1 spec at
// docs/development/specs/2026-04-17-snapshot-id-validation.md.
var uuidCanonicalPattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`,
)

// ErrInvalidSnapshotID is returned when a snapshot identifier fails
// validation — not a canonical UUID, contains path separators, or
// produces a filesystem path outside the store directory.
//
// Callers can distinguish validation failures from I/O failures via
// errors.Is(err, ErrInvalidSnapshotID).
var ErrInvalidSnapshotID = errors.New("invalid snapshot identifier")

// ValidateSnapshotID returns nil if id is a canonical UUID that will be
// accepted by the [FileStore] methods, or an error wrapping
// [ErrInvalidSnapshotID] otherwise.
//
// Use this at the boundary of your own system — e.g. in a CLI flag or
// HTTP handler that accepts a snapshot identifier from an external
// source — so validation happens before the value reaches Save, Load,
// or Delete.
func ValidateSnapshotID(id string) error {
	if id == "" {
		return errors.WithHint(ErrInvalidSnapshotID, "snapshot ID is empty")
	}

	if !uuidCanonicalPattern.MatchString(id) {
		return errors.WithHintf(ErrInvalidSnapshotID,
			"snapshot ID %q is not a canonical UUID", truncate(id, truncatedIDMaxLen))
	}

	return nil
}

// truncate clips s to at most n characters, appending … when clipped.
// Used exclusively in error hints to prevent log amplification from
// attacker-controlled identifiers.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n] + "…"
}

// FileStoreOption configures a FileStore.
type FileStoreOption func(*fileStoreConfig)

type fileStoreConfig struct {
	key []byte
	log logger.Logger
}

// WithEncryption enables AES-256-GCM encryption for stored snapshots.
// The key must be exactly 32 bytes and must come from a cryptographically
// secure source. Use [GenerateEncryptionKey] to generate one.
func WithEncryption(key []byte) FileStoreOption {
	return func(c *fileStoreConfig) { c.key = key }
}

// WithLogger attaches a logger used for diagnostic DEBUG-level events
// (e.g. when [FileStore.List] skips a file whose name is not a canonical
// snapshot identifier). Defaults to a noop logger.
func WithLogger(log logger.Logger) FileStoreOption {
	return func(c *fileStoreConfig) { c.log = log }
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
	log logger.Logger
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

	log := cfg.log
	if log == nil {
		log = logger.NewNoop()
	}

	return &fileStore{fs: fs, dir: dir, key: cfg.key, log: log}, nil
}

// resolveStorePath validates id and returns the absolute-clean path
// inside s.dir. Two layers of defence:
//
//  1. ValidateSnapshotID rejects anything that is not a canonical UUID —
//     forecloses the entire traversal class (no "..", no "/", no "\",
//     no NUL, no Unicode tricks) at the shape level.
//  2. filepath.Rel verifies the cleaned target still lies below
//     s.dir — belt-and-braces against Unicode normalisation quirks,
//     platform-specific path weirdness, and any future relaxation of
//     the regex.
//
// Returns ErrInvalidSnapshotID (wrapped) for any validation failure.
func (s *fileStore) resolveStorePath(id string) (string, error) {
	if err := ValidateSnapshotID(id); err != nil {
		return "", err
	}

	baseAbs, err := filepath.Abs(s.dir)
	if err != nil {
		return "", errors.Wrap(err, "resolving store directory")
	}

	baseAbs = filepath.Clean(baseAbs)
	target := filepath.Clean(filepath.Join(baseAbs, id+".json"))

	rel, err := filepath.Rel(baseAbs, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.WithHint(ErrInvalidSnapshotID,
			"snapshot path escapes the store directory")
	}

	return target, nil
}

func (s *fileStore) Save(_ context.Context, snapshot *Snapshot) error {
	path, err := s.resolveStorePath(snapshot.ID)
	if err != nil {
		return err
	}

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

	return errors.Wrap(afero.WriteFile(s.fs, path, data, filePermissions), "writing snapshot file")
}

func (s *fileStore) Load(_ context.Context, id string) (*Snapshot, error) {
	path, err := s.resolveStorePath(id)
	if err != nil {
		return nil, err
	}

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

		id := strings.TrimSuffix(entry.Name(), ".json")
		// Skip with DEBUG log rather than error — a file manually placed
		// in the store directory with a non-canonical name should not
		// break snapshot enumeration for the user.
		if err := ValidateSnapshotID(id); err != nil {
			s.log.Debug("skipping snapshot file with non-canonical name",
				"filename", truncate(entry.Name(), truncatedIDMaxLen+len(".json")),
				"reason", err.Error())

			continue
		}

		summary, err := s.loadSummary(id)
		if err != nil {
			continue // skip corrupt files
		}

		summaries = append(summaries, summary)
	}

	return summaries, nil
}

func (s *fileStore) loadSummary(id string) (SnapshotSummary, error) {
	// id is a validated, canonical UUID — safe to pass through
	// resolveStorePath again, which is cheap.
	path, err := s.resolveStorePath(id)
	if err != nil {
		return SnapshotSummary{}, err
	}

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
	path, err := s.resolveStorePath(id)
	if err != nil {
		return err
	}

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
