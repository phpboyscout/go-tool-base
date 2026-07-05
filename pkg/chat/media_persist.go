package chat

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/cockroachdb/errors"
)

// Media persistence keeps snapshots small without losing attachments (spec §4.1).
// When a ConversationStore saves a snapshot, large media strings in the opaque
// provider Messages JSON are externalised to a content-addressed cache and
// replaced with a short reference; Load reverses this before the snapshot reaches
// a provider's Restore. It is generic (works on any provider's JSON) and fully
// reversible, so correctness never depends on the media heuristic — only the
// snapshot's size does.

const (
	// mediaRefPrefix marks a snapshot string that was externalised; the remainder
	// is the content hash. Chosen to be vanishingly unlikely in real content.
	mediaRefPrefix = "chatmedia+sha256:"
	// mediaExternalizeMin is the smallest string worth externalising. Media
	// (base64 images/PDF/A-V) dwarfs any text field, so short strings are left
	// inline untouched.
	mediaExternalizeMin = 512
	// mediaHashLen is the hex length of a SHA-256 content hash.
	mediaHashLen = sha256.Size * 2
)

// mediaHash is the content address of an externalised media string.
func mediaHash(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:])
}

// validMediaHash guards a reference read from an (untrusted) snapshot against
// path traversal: a hash must be exactly a lowercase-hex SHA-256.
func validMediaHash(h string) bool {
	if len(h) != mediaHashLen {
		return false
	}

	_, err := hex.DecodeString(h)

	return err == nil
}

// looksLikeMedia reports whether a snapshot string is a large media blob — a
// base64 payload or a base64 data URI. Conservative by design: a false negative
// merely leaves media inline, and a false positive is still perfectly reversible,
// so this only affects snapshot size, never correctness.
func looksLikeMedia(s string) bool {
	if len(s) < mediaExternalizeMin {
		return false
	}

	if strings.HasPrefix(s, "data:") && strings.Contains(s, ";base64,") {
		return true
	}

	_, err := base64.StdEncoding.DecodeString(s)

	return err == nil
}

// externalizeSnapshotMedia rewrites the messages JSON, replacing each large media
// string with a content-addressed reference and handing the original to put. The
// returned JSON is media-free (small); providers never see it (Load reverses it).
func externalizeSnapshotMedia(msgs json.RawMessage, put func(hash, value string) error) (json.RawMessage, error) {
	changed := false

	out, err := transformSnapshotMedia(msgs, func(s string) (string, error) {
		if !looksLikeMedia(s) {
			return s, nil
		}

		h := mediaHash(s)
		if err := put(h, s); err != nil {
			return "", err
		}

		changed = true

		return mediaRefPrefix + h, nil
	})
	if err != nil {
		return nil, err
	}

	if !changed {
		return msgs, nil // no media — keep the original bytes untouched
	}

	return out, nil
}

// internalizeSnapshotMedia reverses externalizeSnapshotMedia: each reference is
// resolved back to its original string via get.
func internalizeSnapshotMedia(msgs json.RawMessage, get func(hash string) (string, error)) (json.RawMessage, error) {
	changed := false

	out, err := transformSnapshotMedia(msgs, func(s string) (string, error) {
		h, ok := strings.CutPrefix(s, mediaRefPrefix)
		if !ok {
			return s, nil
		}

		if !validMediaHash(h) {
			return "", errors.Newf("snapshot media reference has an invalid hash %q", h)
		}

		changed = true

		return get(h)
	})
	if err != nil {
		return nil, err
	}

	if !changed {
		return msgs, nil // no references — keep the original bytes untouched
	}

	return out, nil
}

// transformSnapshotMedia decodes the messages JSON, applies fn to every string
// value (recursing objects and arrays), and re-encodes. An empty message set is a
// no-op.
func transformSnapshotMedia(msgs json.RawMessage, fn func(string) (string, error)) (json.RawMessage, error) {
	if len(msgs) == 0 {
		return msgs, nil
	}

	var doc any
	if err := json.Unmarshal(msgs, &doc); err != nil {
		return nil, errors.Wrap(err, "decoding snapshot messages")
	}

	out, err := walkStrings(doc, fn)
	if err != nil {
		return nil, err
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return nil, errors.Wrap(err, "re-encoding snapshot messages")
	}

	return raw, nil
}

// walkStrings applies fn to every string in a decoded JSON document.
func walkStrings(v any, fn func(string) (string, error)) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			nv, err := walkStrings(val, fn)
			if err != nil {
				return nil, err
			}

			t[k] = nv
		}

		return t, nil
	case []any:
		for i, val := range t {
			nv, err := walkStrings(val, fn)
			if err != nil {
				return nil, err
			}

			t[i] = nv
		}

		return t, nil
	case string:
		return fn(t)
	default:
		return v, nil
	}
}
