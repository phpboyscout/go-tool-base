package docs

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
)

// TestEnsureRenderer_CachesSameWidth asserts the glamour renderer is
// only rebuilt when the word-wrap width actually changes. glamour v1
// made NewTermRenderer materially more expensive (larger eagerly-
// initialised chroma lexer set), so rebuilding per page-switch or
// per resize tick introduces user-visible latency.
func TestEnsureRenderer_CachesSameWidth(t *testing.T) {
	t.Parallel()

	m := NewModel(fstest.MapFS{})

	first := m.ensureRenderer(80)
	second := m.ensureRenderer(80)

	assert.Same(t, first, second, "same-width calls must return the cached renderer")
	assert.Equal(t, 80, m.rendererWidth)
}

// TestEnsureRenderer_RebuildsOnWidthChange verifies a width change
// (e.g. a terminal resize) invalidates the cache. This is the only
// case where a rebuild is justified.
func TestEnsureRenderer_RebuildsOnWidthChange(t *testing.T) {
	t.Parallel()

	m := NewModel(fstest.MapFS{})

	narrow := m.ensureRenderer(80)
	wide := m.ensureRenderer(120)

	assert.NotSame(t, narrow, wide, "width change must produce a new renderer")
	assert.Equal(t, 120, m.rendererWidth)
}
