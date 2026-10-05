package generator

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sizedDiffPager(t *testing.T, content string) diffPagerModel {
	t.Helper()

	m := diffPagerModel{path: "pkg/cmd/foo/cmd.go", content: content}

	next, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	assert.Nil(t, cmd)

	sized, ok := next.(diffPagerModel)
	require.True(t, ok)
	require.True(t, sized.ready)

	return sized
}

func TestDiffPagerModel_InitReturnsNoCommand(t *testing.T) {
	t.Parallel()

	assert.Nil(t, diffPagerModel{}.Init())
}

func TestDiffPagerModel_ViewBeforeSizeIsPlaceholder(t *testing.T) {
	t.Parallel()

	v := diffPagerModel{}.View()

	assert.True(t, v.AltScreen)
	assert.Contains(t, v.Content, "Initializing")
}

func TestDiffPagerModel_ResizeKeepsViewport(t *testing.T) {
	t.Parallel()

	m := sizedDiffPager(t, "line one\nline two")

	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	resized, ok := next.(diffPagerModel)
	require.True(t, ok)

	assert.True(t, resized.ready)
	assert.Equal(t, 100, resized.viewport.Width())
	assert.Equal(t, 36, resized.viewport.Height())
}

func TestDiffPagerModel_ViewRendersHeaderBodyAndFooter(t *testing.T) {
	t.Parallel()

	m := sizedDiffPager(t, "the diff body")
	v := m.View()

	assert.True(t, v.AltScreen)
	assert.Contains(t, v.Content, "Diff: pkg/cmd/foo/cmd.go")
	assert.Contains(t, v.Content, "the diff body")
	assert.Contains(t, v.Content, "y overwrite")
}

func TestDiffPagerModel_KeysDecide(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  tea.KeyPressMsg
		want diffResult
		quit bool
	}{
		{key: tea.KeyPressMsg{Code: 'y', Text: "y"}, want: diffResultOverwrite, quit: true},
		{key: tea.KeyPressMsg{Code: 'Y', Text: "Y"}, want: diffResultOverwrite, quit: true},
		{key: tea.KeyPressMsg{Code: 'n', Text: "n"}, want: diffResultKeep, quit: true},
		{key: tea.KeyPressMsg{Code: 'q', Text: "q"}, want: diffResultKeep, quit: true},
		{key: tea.KeyPressMsg{Code: tea.KeyEscape}, want: diffResultKeep, quit: true},
		{key: tea.KeyPressMsg{Code: tea.KeyDown}, want: diffResultPending, quit: false},
	}

	for _, tt := range tests {
		t.Run(tt.key.String(), func(t *testing.T) {
			t.Parallel()

			m := sizedDiffPager(t, strings.Repeat("row\n", 100))

			next, cmd := m.Update(tt.key)
			got, ok := next.(diffPagerModel)
			require.True(t, ok)

			assert.Equal(t, tt.want, got.result)

			if tt.quit {
				require.NotNil(t, cmd)
				assert.IsType(t, tea.QuitMsg{}, cmd())
			}
		})
	}
}

func TestColoriseDiff_KeepsEveryLine(t *testing.T) {
	t.Parallel()

	diff := strings.Join([]string{
		"--- a (current)",
		"+++ b (incoming)",
		"@@ -1,2 +1,2 @@",
		"-old",
		"+new",
		" context",
	}, "\n")

	out := coloriseDiff(diff)

	lines := strings.Split(out, "\n")
	require.Len(t, lines, 6)

	for i, want := range []string{"--- a (current)", "+++ b (incoming)", "@@ -1,2 +1,2 @@", "-old", "+new", " context"} {
		assert.Contains(t, lines[i], want)
	}
}

func TestRunDiffPager_IdenticalContentKeeps(t *testing.T) {
	t.Parallel()

	same := []byte("package foo\n")

	assert.False(t, runDiffPager("foo.go", same, same))
}
