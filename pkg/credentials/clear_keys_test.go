package credentials_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentials"
)

// fakeKeyWriter records Set calls so tests can assert exactly which
// keys ClearKeysExcept blanked.
type fakeKeyWriter struct {
	set map[string]any
}

func newFakeKeyWriter() *fakeKeyWriter {
	return &fakeKeyWriter{set: map[string]any{}}
}

func (f *fakeKeyWriter) Set(key string, value any) {
	f.set[key] = value
}

func TestClearKeysExcept(t *testing.T) {
	t.Parallel()

	all := []string{"x.env", "x.literal", "x.keychain"}

	t.Run("blanks the keys not in keep", func(t *testing.T) {
		t.Parallel()

		w := newFakeKeyWriter()
		credentials.ClearKeysExcept(w, all, "x.env")

		assert.Equal(t, map[string]any{"x.literal": "", "x.keychain": ""}, w.set,
			"every key except the kept one is blanked to empty string")
		_, kept := w.set["x.env"]
		assert.False(t, kept, "the kept key is never written")
	})

	t.Run("supports multiple kept keys", func(t *testing.T) {
		t.Parallel()

		w := newFakeKeyWriter()
		credentials.ClearKeysExcept(w, all, "x.env", "x.literal")

		assert.Equal(t, map[string]any{"x.keychain": ""}, w.set)
	})

	t.Run("ignores empty keys in all and keep", func(t *testing.T) {
		t.Parallel()

		w := newFakeKeyWriter()
		credentials.ClearKeysExcept(w, []string{"a", "", "b"}, "")

		assert.Equal(t, map[string]any{"a": "", "b": ""}, w.set,
			"empty strings in all are skipped and an empty keep matches nothing")
	})

	t.Run("no-op when every key is kept", func(t *testing.T) {
		t.Parallel()

		w := newFakeKeyWriter()
		credentials.ClearKeysExcept(w, all, all...)

		assert.Empty(t, w.set)
	})
}
