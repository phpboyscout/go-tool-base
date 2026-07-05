package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/afero"
)

// bigBase64 is a media-sized base64 string (> mediaExternalizeMin).
func bigBase64() string {
	raw := make([]byte, 1024)
	for i := range raw {
		raw[i] = byte(i * 7)
	}

	return base64.StdEncoding.EncodeToString(raw)
}

// messagesWithMedia mimics a provider's serialized history carrying an inline
// base64 attachment plus small text/id fields that must stay untouched.
func messagesWithMedia(t *testing.T, b64 string) json.RawMessage {
	t.Helper()

	doc := []any{map[string]any{
		"role": "user",
		"parts": []any{
			map[string]any{"text": "describe this"},
			map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": b64}},
		},
	}}

	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	return raw
}

// The pure transform externalises media and restores it byte-identically; small
// fields are left inline.
func TestExternalizeInternalize_roundTrip(t *testing.T) {
	t.Parallel()

	b64 := bigBase64()
	msgs := messagesWithMedia(t, b64)

	cache := map[string]string{}

	ext, err := externalizeSnapshotMedia(msgs, func(h, v string) error { cache[h] = v; return nil })
	if err != nil {
		t.Fatalf("externalise: %v", err)
	}

	s := string(ext)
	if strings.Contains(s, b64) {
		t.Fatal("externalised messages still contain the raw base64")
	}

	if !strings.Contains(s, mediaRefPrefix) {
		t.Fatal("externalised messages carry no media reference")
	}

	if !strings.Contains(s, "describe this") || !strings.Contains(s, "image/png") {
		t.Fatal("small fields (text, mimeType) should stay inline")
	}

	if len(cache) != 1 {
		t.Fatalf("want 1 cached blob, got %d", len(cache))
	}

	back, err := internalizeSnapshotMedia(ext, func(h string) (string, error) { return cache[h], nil })
	if err != nil {
		t.Fatalf("internalise: %v", err)
	}

	if !strings.Contains(string(back), b64) {
		t.Fatal("internalised messages did not restore the base64")
	}
}

func TestLooksLikeMedia(t *testing.T) {
	t.Parallel()

	if looksLikeMedia("short") {
		t.Fatal("short strings are not media")
	}

	if looksLikeMedia(strings.Repeat("not base64 !!! ", 100)) {
		t.Fatal("large non-base64 text is not media")
	}

	if !looksLikeMedia(bigBase64()) {
		t.Fatal("a large base64 string is media")
	}

	if !looksLikeMedia("data:application/pdf;base64," + bigBase64()) {
		t.Fatal("a base64 data URI is media")
	}
}

func TestValidMediaHash(t *testing.T) {
	t.Parallel()

	if !validMediaHash(mediaHash("x")) {
		t.Fatal("a real hash should validate")
	}

	for _, bad := range []string{"", "../etc/passwd", "zz", strings.Repeat("g", mediaHashLen)} {
		if validMediaHash(bad) {
			t.Fatalf("%q should be rejected (path-traversal guard)", bad)
		}
	}
}

// End-to-end through the FileStore: media is externalised to the cache (the
// snapshot file stays small), and Load restores the messages byte-identically —
// with and without encryption.
func TestFileStore_mediaRoundTrip(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		encrypt bool
	}{{name: "plain"}, {name: "encrypted", encrypt: true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()

			var opts []FileStoreOption
			if tc.encrypt {
				key, _ := GenerateEncryptionKey()
				opts = append(opts, WithEncryption(key))
			}

			store, err := NewFileStore(fs, "/snaps", opts...)
			if err != nil {
				t.Fatalf("NewFileStore: %v", err)
			}

			b64 := bigBase64()
			snap := NewSnapshot(ProviderGemini, "gemini-x", "", messagesWithMedia(t, b64), nil, nil)

			if err := store.Save(context.Background(), snap); err != nil {
				t.Fatalf("Save: %v", err)
			}

			// the snapshot file must not carry the raw media.
			raw, _ := afero.ReadFile(fs, "/snaps/"+snap.ID+".json")
			if strings.Contains(string(raw), b64) {
				t.Fatal("snapshot file still contains the raw base64")
			}

			// a cache blob must exist.
			entries, _ := afero.ReadDir(fs, "/snaps/media")
			if len(entries) != 1 {
				t.Fatalf("want 1 cached media blob, got %d", len(entries))
			}

			// Load restores the messages byte-identically.
			got, err := store.Load(context.Background(), snap.ID)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			if !strings.Contains(string(got.Messages), b64) {
				t.Fatal("Load did not restore the media into the messages")
			}
		})
	}
}
