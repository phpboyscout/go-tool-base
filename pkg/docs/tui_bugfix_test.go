package docs

import (
	"sync"
	"testing"
	"testing/fstest"

	"github.com/charmbracelet/glamour"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPerformSearch_UseRegexCapturedByValue proves the search Cmd closure
// snapshots m.useRegex at construction time on the Update goroutine rather
// than reading the field from the Cmd's own goroutine. Run under -race, a
// by-reference read here would be flagged as a data race against the
// concurrent flip of m.useRegex.
func TestPerformSearch_UseRegexCapturedByValue(t *testing.T) {
	t.Parallel()

	m := &Model{
		fs:       fstest.MapFS{"readme.md": &fstest.MapFile{Data: []byte("# alpha\n")}},
		useRegex: false,
	}

	// Build the Cmd while useRegex is false. The snapshot must be taken now.
	cmd := m.performSearch("alpha")

	var wg sync.WaitGroup

	wg.Add(2)

	// Goroutine A: run the search Cmd (reads the captured snapshot, never
	// m.useRegex).
	go func() {
		defer wg.Done()

		_ = cmd()
	}()

	// Goroutine B: mutate the model field the way Update would on Ctrl+R.
	go func() {
		defer wg.Done()

		m.useRegex = true
	}()

	wg.Wait()
}

// TestRenderMarkdown_StartsFromNilRendererWithoutPanic proves the render
// path is nil-safe end to end. A freshly-zeroed Model has a nil renderer
// (the latent NewTermRenderer (nil, nil) case); renderMarkdown must rebuild
// or fall back to raw content rather than dereferencing nil. Either way the
// match text survives, and no panic occurs.
func TestRenderMarkdown_StartsFromNilRendererWithoutPanic(t *testing.T) {
	t.Parallel()

	m := &Model{fs: fstest.MapFS{}, renderer: nil}

	assert.NotPanics(t, func() {
		got := m.renderMarkdown("# hello", 80)
		assert.Contains(t, got, "hello",
			"render must preserve content whether styled or raw")
	})
}

// TestRenderMarkdown_NilGuardReturnsRaw exercises the nil-renderer guard
// directly: the helper must return the raw content unchanged when it cannot
// obtain a renderer, never panic. We assert against renderMarkdownWith so
// the nil branch is covered deterministically without depending on whether
// glamour.NewTermRenderer ever actually returns nil.
func TestRenderMarkdown_NilGuardReturnsRaw(t *testing.T) {
	t.Parallel()

	got := renderMarkdownWith(nil, "# hello")
	assert.Equal(t, "# hello", got,
		"nil renderer must fall back to the raw markdown unchanged")
}

// TestRenderMarkdownWith_RealRendererStyles covers the non-nil success
// branch: a working renderer produces styled output that still contains the
// source text.
func TestRenderMarkdownWith_RealRendererStyles(t *testing.T) {
	t.Parallel()

	r, err := glamour.NewTermRenderer(glamour.WithWordWrap(80))
	require.NoError(t, err)

	got := renderMarkdownWith(r, "# hello")
	assert.Contains(t, got, "hello")
}

// TestRenderMarkdown_RefreshesOnWidthChange proves renderMarkdown rebuilds
// the renderer when the width changes (the "refresh on width change" half
// of the renderer finding), then renders successfully.
func TestRenderMarkdown_RefreshesOnWidthChange(t *testing.T) {
	t.Parallel()

	r, err := glamour.NewTermRenderer(glamour.WithWordWrap(80))
	require.NoError(t, err)

	m := &Model{
		fs:            fstest.MapFS{},
		renderer:      r,
		rendererWidth: 80,
	}

	out := m.renderMarkdown("# hello", 120)
	assert.Equal(t, 120, m.rendererWidth, "width change must refresh the renderer")
	assert.NotEmpty(t, out)
}
