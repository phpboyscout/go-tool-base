package chat

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

// Canonical UUID literals used across FileStore tests. Picking
// deterministic values (rather than uuid.New()) keeps the tests
// reproducible and makes failures easier to read in CI logs.
const (
	testSnapshotID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	testListID1    = "11111111-1111-4111-8111-111111111111"
	testListID2    = "22222222-2222-4222-8222-222222222222"
	testListID3    = "33333333-3333-4333-8333-333333333333"
	testMissingID  = "99999999-9999-4999-8999-999999999999"
)

func TestGenerateEncryptionKey(t *testing.T) {
	t.Parallel()

	key1, err := GenerateEncryptionKey()
	require.NoError(t, err)
	assert.Len(t, key1, 32, "key must be 32 bytes for AES-256")

	key2, err := GenerateEncryptionKey()
	require.NoError(t, err)
	assert.Len(t, key2, 32)

	// Two calls must produce different keys — they come from crypto/rand.
	// A 256-bit collision is astronomically unlikely; a failing test here
	// indicates a broken random source, not bad luck.
	assert.NotEqual(t, key1, key2, "two generated keys must differ")
}

func testSnapshot(t *testing.T) *Snapshot {
	t.Helper()

	return &Snapshot{
		ID:           testSnapshotID,
		Provider:     ProviderClaude,
		Model:        "claude-3-5-sonnet",
		SystemPrompt: "You are a helpful assistant.",
		Messages:     json.RawMessage(`[{"role":"user","content":"hello"}]`),
		Tools: []ToolSnapshot{
			{Name: "search", Description: "Search the web"},
		},
		Metadata:  map[string]string{"env": "test"},
		CreatedAt: time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC),
		Version:   SnapshotVersion,
	}
}

func testKey(t *testing.T) []byte {
	t.Helper()

	key := make([]byte, aesKeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	return key
}

func TestFileStore_SaveLoad(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	store, err := NewFileStore(fs, "/snapshots")
	require.NoError(t, err)

	snap := testSnapshot(t)
	ctx := context.Background()

	require.NoError(t, store.Save(ctx, snap))

	loaded, err := store.Load(ctx, snap.ID)
	require.NoError(t, err)

	assert.Equal(t, snap.ID, loaded.ID)
	assert.Equal(t, snap.Provider, loaded.Provider)
	assert.Equal(t, snap.Model, loaded.Model)
	assert.Equal(t, snap.SystemPrompt, loaded.SystemPrompt)
	assert.JSONEq(t, string(snap.Messages), string(loaded.Messages))
	assert.Len(t, loaded.Tools, len(snap.Tools))
	assert.Equal(t, snap.Metadata["env"], loaded.Metadata["env"])
	assert.Equal(t, snap.Version, loaded.Version)
}

func TestFileStore_List(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	store, err := NewFileStore(fs, "/snapshots")
	require.NoError(t, err)

	ctx := context.Background()

	for _, id := range []string{testListID1, testListID2, testListID3} {
		snap := testSnapshot(t)
		snap.ID = id
		require.NoError(t, store.Save(ctx, snap))
	}

	summaries, err := store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, summaries, 3)

	for _, s := range summaries {
		assert.NotEmpty(t, s.ID)
		assert.Equal(t, ProviderClaude, s.Provider)
		assert.Equal(t, "claude-3-5-sonnet", s.Model)
		assert.Equal(t, 1, s.MessageCount)
	}
}

func TestFileStore_Delete(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	store, err := NewFileStore(fs, "/snapshots")
	require.NoError(t, err)

	ctx := context.Background()
	snap := testSnapshot(t)

	require.NoError(t, store.Save(ctx, snap))
	require.NoError(t, store.Delete(ctx, snap.ID))

	_, err = store.Load(ctx, snap.ID)
	require.Error(t, err)
}

func TestFileStore_WithEncryption(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	key := testKey(t)

	store, err := NewFileStore(fs, "/snapshots", WithEncryption(key))
	require.NoError(t, err)

	ctx := context.Background()
	snap := testSnapshot(t)

	require.NoError(t, store.Save(ctx, snap))

	// Raw file should not contain plaintext
	raw, err := afero.ReadFile(fs, "/snapshots/"+snap.ID+".json")
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "helpful assistant")

	// Load should decrypt successfully
	loaded, err := store.Load(ctx, snap.ID)
	require.NoError(t, err)
	assert.Equal(t, snap.SystemPrompt, loaded.SystemPrompt)
}

func TestFileStore_EncryptionKeyMismatch(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	key1 := testKey(t)
	key2 := testKey(t)

	store1, err := NewFileStore(fs, "/snapshots", WithEncryption(key1))
	require.NoError(t, err)

	ctx := context.Background()
	snap := testSnapshot(t)

	require.NoError(t, store1.Save(ctx, snap))

	store2, err := NewFileStore(fs, "/snapshots", WithEncryption(key2))
	require.NoError(t, err)

	_, err = store2.Load(ctx, snap.ID)
	require.Error(t, err, "loading with wrong key should fail")
}

func TestFileStore_DirectoryCreation(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	store, err := NewFileStore(fs, "/deep/nested/dir")
	require.NoError(t, err)

	snap := testSnapshot(t)
	require.NoError(t, store.Save(context.Background(), snap))

	exists, err := afero.DirExists(fs, "/deep/nested/dir")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestFileStore_InvalidKeySize(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	_, err := NewFileStore(fs, "/snapshots", WithEncryption([]byte("too-short")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")
}

func TestFileStore_LoadNotFound(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	store, err := NewFileStore(fs, "/snapshots")
	require.NoError(t, err)

	_, err = store.Load(context.Background(), testMissingID)
	require.Error(t, err)
	// A valid-but-absent ID reaches the filesystem and surfaces an I/O
	// error — it must NOT be wrapped in ErrInvalidSnapshotID, otherwise
	// callers cannot distinguish "unknown snapshot" from "rejected input".
	require.NotErrorIs(t, err, ErrInvalidSnapshotID)
}

// --- H-1 validation & path-traversal suite -----------------------------
//
// The following tests exercise the validation layer added for the
// 2026-04-17-snapshot-id-validation.md spec. The goal is that no
// filesystem operation ever runs on a non-canonical identifier, and
// that List remains robust in the face of corrupt directory state.

func TestValidateSnapshotID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{name: "canonical v4 uuid accepted", id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", wantErr: false},
		{name: "all-zero uuid accepted", id: "00000000-0000-0000-0000-000000000000", wantErr: false},
		{name: "mixed hex uuid accepted", id: "0123abcd-4567-89ab-cdef-0123456789ab", wantErr: false},

		{name: "empty rejected", id: "", wantErr: true},
		{name: "uppercase hex rejected", id: "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA", wantErr: true},
		{name: "non-hex character rejected", id: "gggggggg-gggg-4ggg-8ggg-gggggggggggg", wantErr: true},
		{name: "too short rejected", id: "aaaa", wantErr: true},
		{name: "one char too long rejected", id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaaa", wantErr: true},
		{name: "wrong hyphen positions rejected", id: "aaaaaaaaa-aaa-4aaa-8aaa-aaaaaaaaaaaa", wantErr: true},
		{name: "relative traversal rejected", id: "../../etc/passwd", wantErr: true},
		{name: "bare dot-dot rejected", id: "..", wantErr: true},
		{name: "absolute path rejected", id: "/etc/passwd", wantErr: true},
		{name: "forward slash rejected", id: "aaaaaaaa/aaaa/4aaa/8aaa/aaaaaaaaaaaa", wantErr: true},
		{name: "backslash rejected", id: "aaaaaaaa\\aaaa\\4aaa\\8aaa\\aaaaaaaaaaaa", wantErr: true},
		{name: "trailing NUL byte rejected", id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa\x00", wantErr: true},
		{name: "leading whitespace rejected", id: " aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", wantErr: true},
		{name: "trailing whitespace rejected", id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa ", wantErr: true},
		{name: "cyrillic lookalike rejected", id: "ааааaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateSnapshotID(tc.id)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, ErrInvalidSnapshotID)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateSnapshotID_UUIDNewAlwaysValidates(t *testing.T) {
	t.Parallel()

	// Invariant: every ID produced by uuid.New() — the sole source of
	// snapshot IDs in NewSnapshot — must pass validation. A failure here
	// indicates drift between the validator and the generator.
	for range 100 {
		id := uuid.New().String()
		require.NoErrorf(t, ValidateSnapshotID(id), "uuid.New() produced %q which failed validation", id)
	}
}

func TestValidateSnapshotID_HintTruncatesLongInput(t *testing.T) {
	t.Parallel()

	// A 200-character non-UUID input. The hint must truncate with an
	// ellipsis rather than echoing the full attacker-controlled value,
	// to prevent log amplification.
	long := strings.Repeat("z", 200)

	err := ValidateSnapshotID(long)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidSnapshotID)

	hint := errors.FlattenHints(err)
	assert.Contains(t, hint, "…", "long inputs must be truncated with an ellipsis in the hint")
	assert.NotContains(t, hint, long, "full input must not appear in the hint")
}

func TestFileStore_SaveRejectsInvalidID(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	store, err := NewFileStore(fs, "/snapshots")
	require.NoError(t, err)

	snap := testSnapshot(t)
	snap.ID = "../../etc/passwd"

	err = store.Save(context.Background(), snap)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidSnapshotID)

	// A rejected Save must not produce any file anywhere on the fs —
	// the rejection happens before MkdirAll and WriteFile.
	traversed, _ := afero.Exists(fs, "/etc/passwd")
	assert.False(t, traversed, "Save must not create a file via traversal")

	dirCreated, _ := afero.DirExists(fs, "/snapshots")
	assert.False(t, dirCreated, "Save must not create the store directory when the ID is rejected")
}

func TestFileStore_LoadRejectsTraversal(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	// Plant a sensitive file that a traversing Load would expose.
	require.NoError(t, afero.WriteFile(fs, "/etc/passwd", []byte("root:x:0:0"), filePermissions))

	store, err := NewFileStore(fs, "/snapshots")
	require.NoError(t, err)

	malicious := []string{
		"../../etc/passwd",
		"..",
		"/etc/passwd",
		"a/b",
		"",
	}

	for _, id := range malicious {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			_, err := store.Load(context.Background(), id)
			require.Errorf(t, err, "Load(%q) should fail", id)
			require.ErrorIsf(t, err, ErrInvalidSnapshotID, "Load(%q) should return ErrInvalidSnapshotID", id)
		})
	}
}

func TestFileStore_DeleteRejectsInvalidID(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	// Plant a file that Delete must leave alone.
	require.NoError(t, afero.WriteFile(fs, "/etc/passwd", []byte("root"), filePermissions))

	store, err := NewFileStore(fs, "/snapshots")
	require.NoError(t, err)

	err = store.Delete(context.Background(), "../../etc/passwd")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidSnapshotID)

	exists, _ := afero.Exists(fs, "/etc/passwd")
	assert.True(t, exists, "Delete must not touch files reached via traversal")
}

func TestFileStore_ListSkipsNonCanonical(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	store, err := NewFileStore(fs, "/snapshots")
	require.NoError(t, err)

	ctx := context.Background()

	// One legitimate snapshot written through the API.
	snap := testSnapshot(t)
	require.NoError(t, store.Save(ctx, snap))

	// Simulate files placed in the store directory with non-canonical
	// names — e.g. a manual copy, an earlier buggy version, or a race.
	// List must ignore them rather than error the whole call.
	require.NoError(t, afero.WriteFile(fs, "/snapshots/not-a-uuid.json", []byte("{}"), filePermissions))
	require.NoError(t, afero.WriteFile(fs, "/snapshots/hostile-name.json", []byte("{}"), filePermissions))
	require.NoError(t, afero.WriteFile(fs, "/snapshots/README.md", []byte("notes"), filePermissions))

	summaries, err := store.List(ctx)
	require.NoError(t, err, "List must be robust to invalid filenames in the store directory")
	require.Len(t, summaries, 1, "only the canonical-UUID file should be listed")
	assert.Equal(t, testSnapshotID, summaries[0].ID)
}

// recordingLogger captures log calls for assertions in tests.
type recordingLogger struct {
	logger.Logger
	debugCalls int
	lastMsg    string
}

func (r *recordingLogger) Debug(msg string, _ ...any) {
	r.debugCalls++
	r.lastMsg = msg
}

func TestFileStore_WithLogger_LogsSkippedFiles(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	rec := &recordingLogger{Logger: logger.NewNoop()}

	store, err := NewFileStore(fs, "/snapshots", WithLogger(rec))
	require.NoError(t, err)

	// Populate with one valid snapshot plus one non-canonical filename.
	snap := testSnapshot(t)
	require.NoError(t, store.Save(context.Background(), snap))
	require.NoError(t, afero.WriteFile(fs, "/snapshots/not-a-uuid.json", []byte("{}"), filePermissions))

	_, err = store.List(context.Background())
	require.NoError(t, err)

	assert.GreaterOrEqual(t, rec.debugCalls, 1, "skipped files should produce a DEBUG log")
	assert.Contains(t, rec.lastMsg, "skipping", "log message should describe the skip")
}

// TestFileStore_ResolveStorePathContainment is a white-box check that
// the second line of defence — the filepath.Rel containment verification
// in resolveStorePath — is wired up. Under normal inputs the regex layer
// already forecloses every traversal attempt, so this test exists to
// keep the containment branch visible to coverage and to future readers.
func TestFileStore_ResolveStorePathContainment(t *testing.T) {
	t.Parallel()

	s, err := NewFileStore(afero.NewMemMapFs(), "/data/snapshots")
	require.NoError(t, err)

	impl, ok := s.(*fileStore)
	require.True(t, ok)

	target, err := impl.resolveStorePath(testSnapshotID)
	require.NoError(t, err)

	baseAbs, err := filepath.Abs("/data/snapshots")
	require.NoError(t, err)

	assert.True(t,
		strings.HasPrefix(target, filepath.Clean(baseAbs)+string(filepath.Separator)),
		"resolved target %q must stay inside base %q", target, baseAbs)
	assert.True(t,
		strings.HasSuffix(target, testSnapshotID+".json"),
		"resolved target %q must end with <id>.json", target)
}
