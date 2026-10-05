package docs

import (
	"errors"
	"fmt"
	"image/color"
	"strings"
	"testing"
	"testing/fstest"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

const (
	testWidth  = 120
	testHeight = 40
)

func newTestDocsFS() fstest.MapFS {
	return fstest.MapFS{
		"mkdocs.yml": {Data: []byte(`nav:
  - Home: index.md
  - Guide:
    - Getting Started: guide/start.md
    - Advanced: guide/advanced.md
  - Other: other.md
`)},
		"index.md":          {Data: []byte("---\nauthor: tester\n---\n# Welcome\n\nThe home page.\n")},
		"guide/start.md":    {Data: []byte("# Getting Started\n\nStart with alpha.\n")},
		"guide/advanced.md": {Data: []byte("# Advanced\n\nNothing to see.\n")},
		"other.md":          {Data: []byte("# Other\n\nAlpha again.\n")},
		"notes.txt":         {Data: []byte("alpha in a non-markdown file")},
	}
}

func keyText(s string) tea.KeyPressMsg {
	r := []rune(s)

	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func keyCode(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

func keyCtrl(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl}
}

func newSizedModel(t *testing.T, opts ...Option) *Model {
	t.Helper()

	m := NewModel(newTestDocsFS(), opts...)
	m.Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})

	return m
}

func press(m *Model, msgs ...tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd

	for _, msg := range msgs {
		_, cmd = m.Update(msg)
	}

	return cmd
}

func viewContent(m *Model) string {
	return ansi.Strip(m.View().Content)
}

func isQuit(t *testing.T, cmd tea.Cmd) bool {
	t.Helper()

	if cmd == nil {
		return false
	}

	_, ok := cmd().(tea.QuitMsg)

	return ok
}

func TestNewModel_AppliesOptionsAndLoadsIndex(t *testing.T) {
	t.Parallel()

	ask := func(string, func(string, logger.Level), func(string)) (string, error) { return "", nil }
	m := NewModel(newTestDocsFS(), WithTitle("My Docs"), WithAskFunc(ask))

	assert.Equal(t, "My Docs", m.title)
	assert.NotNil(t, m.askFunc)
	assert.True(t, m.mkdocsLoaded)
	require.Len(t, m.currentItems, 3)
	assert.True(t, m.currentItems[1].IsGroup)
	assert.Equal(t, "index.md", m.currentPath)
	assert.Equal(t, "author: tester", m.frontmatter)
	assert.Contains(t, m.content, "Welcome")
	assert.NotNil(t, m.Init())
}

func TestModel_ViewBeforeAndAfterWindowSize(t *testing.T) {
	t.Parallel()

	m := NewModel(newTestDocsFS(), WithTitle("Browser"))

	v := m.View()
	assert.Equal(t, "Initializing...", v.Content)
	assert.True(t, v.AltScreen)

	m.Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})

	out := viewContent(m)
	assert.Contains(t, out, "Browser")
	assert.Contains(t, out, "Home")
	assert.Contains(t, out, "Guide/")
	assert.Contains(t, out, "Enter/→: Select")
	assert.Equal(t, testWidth, m.width)
}

func TestModel_UnknownMessageIsIgnored(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)

	_, cmd := m.Update(struct{}{})
	assert.Nil(t, cmd)
}

func TestModel_SidebarNavigation(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)

	press(m, keyCode(tea.KeyUp))
	assert.Equal(t, 0, m.cursor, "up at the top stays put")

	press(m, keyText("j"))
	assert.Equal(t, 1, m.cursor)

	press(m, keyCode(tea.KeyDown), keyCode(tea.KeyDown))
	assert.Equal(t, 2, m.cursor, "down at the bottom stays put")

	press(m, keyText("k"))
	assert.Equal(t, 1, m.cursor)

	press(m, keyCode(tea.KeyLeft))
	assert.Empty(t, m.navStack, "back with an empty stack is a no-op")

	press(m, keyCode(tea.KeyEnter))
	require.Len(t, m.navStack, 1, "entering a group pushes the stack")
	assert.Equal(t, []string{"Guide"}, m.titleStack)
	require.Len(t, m.currentItems, 2)
	assert.Contains(t, viewContent(m), "Guide")

	press(m, keyCode(tea.KeyBackspace))
	assert.Empty(t, m.navStack)
	assert.Empty(t, m.titleStack)
	assert.Equal(t, 0, m.cursor)

	press(m, keyText("j"), keyText("l"))
	press(m, keyText("j"), keyText("l"))
	assert.Equal(t, "guide/advanced.md", m.currentPath)
	assert.Equal(t, focusContent, m.focus)
	assert.Contains(t, m.content, "Advanced")

	press(m, keyText("h"))
	assert.Equal(t, focusSidebar, m.focus)
}

func TestModel_SidebarNavStackWithoutTitle(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)
	m.navStack = [][]ListItem{m.currentItems}

	assert.Contains(t, viewContent(m), "...")

	press(m, keyText("h"))
	assert.Empty(t, m.navStack)
}

func TestModel_HandleSelectEdgeCases(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)

	m.currentItems = nil
	m.handleSelect()
	assert.Nil(t, m.currentItems)

	m.currentItems = []ListItem{{Title: "No path"}}
	m.handleSelect()
	assert.Equal(t, focusSidebar, m.focus, "an item with neither children nor a path does nothing")
}

func TestModel_ContentFocusScrollsViewport(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)
	m.focus = focusContent
	m.viewport.SetContent(strings.Repeat("line\n", 200))

	press(m, keyCode(tea.KeyDown))
	assert.Positive(t, m.viewport.YOffset())

	out := viewContent(m)
	assert.Contains(t, out, "←: Focus Sidebar")
}

func TestModel_QuitKeys(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)

	assert.True(t, isQuit(t, press(m, keyText("q"))))
	assert.True(t, isQuit(t, press(m, keyCtrl('c'))))
}

func TestModel_ToggleSidebar(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)
	widthOpen := m.viewport.Width()

	press(m, keyCode(tea.KeyTab))
	assert.False(t, m.sidebarOpen)
	assert.Equal(t, focusContent, m.focus)
	assert.Greater(t, m.viewport.Width(), widthOpen)
	assert.Equal(t, 0, m.sidebarWidth())
	assert.NotContains(t, viewContent(m), "Guide/")

	press(m, keyCode(tea.KeyTab))
	assert.True(t, m.sidebarOpen)
	assert.Equal(t, focusContent, m.focus, "reopening with content keeps content focus")

	m.content = ""
	press(m, keyCode(tea.KeyTab), keyCode(tea.KeyTab))
	assert.Equal(t, focusSidebar, m.focus, "reopening with no content focuses the sidebar")
	assert.NotContains(t, viewContent(m), "Welcome")
}

func TestModel_ResizeSidebar(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)

	press(m, keyText(">"))
	assert.InDelta(t, defaultSidebarRatio+sidebarResizeDelta, m.sidebarRatio, 1e-9)

	press(m, keyText("<"), keyText("<"))
	assert.InDelta(t, defaultSidebarRatio-sidebarResizeDelta, m.sidebarRatio, 1e-9)

	for range 10 {
		press(m, keyText("<"))
	}

	assert.GreaterOrEqual(t, m.sidebarRatio, 0.1, "ratio is clamped at the lower bound")

	m.sidebarOpen = false
	before := m.sidebarRatio

	press(m, keyText(">"))
	assert.InDelta(t, before, m.sidebarRatio, 1e-9, "a closed sidebar does not resize")
}

func TestModel_InfoToggle(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)
	heightWithout := m.viewport.Height()

	press(m, keyText("i"))
	assert.True(t, m.showInfo)
	assert.Less(t, m.viewport.Height(), heightWithout, "the frontmatter footer takes viewport height")
	assert.Contains(t, viewContent(m), "author: tester")

	press(m, keyText("i"))
	assert.False(t, m.showInfo)

	m.loadFile("other.md")
	assert.Empty(t, m.frontmatter)

	press(m, keyText("i"))
	assert.False(t, m.showInfo, "info does not toggle without frontmatter")
}

func TestModel_LoadFileMissingIsNoOp(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)
	m.loadFile("does-not-exist.md")

	assert.Equal(t, "index.md", m.currentPath)
}

func TestModel_BackgroundColorSwitchesStyle(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)

	m.Update(tea.BackgroundColorMsg{Color: color.Black})
	assert.Equal(t, defaultGlamourStyle, m.styleName, "dark on dark is a no-op")

	m.Update(tea.BackgroundColorMsg{Color: color.White})
	assert.Equal(t, lightGlamourStyle, m.styleName)
	assert.NotNil(t, m.renderer, "the current page is re-rendered with a fresh renderer")
	assert.Contains(t, m.content, "Welcome")

	blank := &Model{fs: fstest.MapFS{}, styleName: lightGlamourStyle}
	blank.applyBackground(true)
	assert.Equal(t, defaultGlamourStyle, blank.styleName)
	assert.Nil(t, blank.renderer, "nothing to re-render without a current page")
}

func TestModel_SearchInput(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)

	assert.NotNil(t, press(m, keyText("s")))
	assert.True(t, m.showSearchInput)

	out := viewContent(m)
	assert.Contains(t, out, "Press Ctrl+R for Regex")
	assert.Contains(t, out, "Enter: Search • Esc: Cancel")

	press(m, keyCtrl('r'))
	assert.True(t, m.useRegex)
	assert.Contains(t, viewContent(m), "REGEX MODE")

	press(m, keyCtrl('r'))
	assert.False(t, m.useRegex)

	press(m, keyText("x"))
	assert.Equal(t, "x", m.searchInput.Value())

	press(m, keyCode(tea.KeyEscape))
	assert.False(t, m.showSearchInput)

	press(m, keyText("s"), keyCode(tea.KeyEnter))
	assert.False(t, m.showSearchInput)
	assert.False(t, m.showSearchResults, "an empty query shows no results")
	assert.Empty(t, m.searchResults)
}

func runSearch(t *testing.T, m *Model, query string) {
	t.Helper()

	press(m, keyText("s"))

	for _, r := range query {
		press(m, keyText(string(r)))
	}

	cmd := press(m, keyCode(tea.KeyEnter))
	require.NotNil(t, cmd)

	msg, ok := cmd().(SearchResultMessage)
	require.True(t, ok)

	m.Update(msg)
}

func TestModel_SearchResultsFlow(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)
	runSearch(t, m, "alpha")

	require.True(t, m.showSearchResults)
	require.Len(t, m.searchResults, 2, "only markdown files match")
	assert.Nil(t, m.lastRegex)

	out := viewContent(m)
	assert.Contains(t, out, "Search Results: alpha")
	assert.Contains(t, out, "Esc: Close")

	press(m, keyText("j"))
	assert.Equal(t, 1, m.searchCursor)

	press(m, keyCode(tea.KeyDown))
	assert.Equal(t, 1, m.searchCursor, "cannot move past the last result")

	press(m, keyText("k"), keyCode(tea.KeyUp))
	assert.Equal(t, 0, m.searchCursor)

	want := m.searchResults[0].Path

	press(m, keyCode(tea.KeyEnter))
	assert.False(t, m.showSearchResults)
	assert.Equal(t, focusContent, m.focus)
	assert.Equal(t, want, m.currentPath)
}

func TestModel_SearchResultsKeys(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)
	runSearch(t, m, "alpha")

	cmd := press(m, keyText("r"))
	require.NotNil(t, cmd)
	assert.True(t, m.useRegex)

	m.Update(cmd())
	assert.NotNil(t, m.lastRegex)
	assert.Contains(t, viewContent(m), "REGEX")

	assert.NotNil(t, press(m, keyText("s")))
	assert.True(t, m.showSearchInput)
	assert.False(t, m.showSearchResults)

	press(m, keyCode(tea.KeyEscape))
	m.showSearchResults = true

	press(m, keyText("q"))
	assert.False(t, m.showSearchResults)
	assert.Equal(t, focusSidebar, m.focus)
}

func TestModel_SearchNoResults(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)
	runSearch(t, m, "zzz-no-match")

	assert.Empty(t, m.searchResults)
	assert.Contains(t, viewContent(m), "No results found.")

	press(m, keyCode(tea.KeyEnter))
	assert.True(t, m.showSearchResults, "enter with no results does nothing")
}

func TestModel_RegexSearchWithoutMatch(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)
	m.useRegex = true

	msg, ok := m.performSearch("zz+top")().(SearchResultMessage)
	require.True(t, ok)
	assert.Empty(t, msg.Results)

	idx, n := m.searchMatch("abc", "", nil)
	assert.Equal(t, 0, idx)
	assert.Equal(t, 0, n)
}

func TestModel_SearchScrollKeepsCursorVisible(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{}
	for i := range 12 {
		fsys[fmt.Sprintf("page%02d.md", i)] = &fstest.MapFile{Data: []byte("needle\n")}
	}

	m := NewModel(fsys)
	m.Update(tea.WindowSizeMsg{Width: testWidth, Height: 20})

	msg, ok := m.performSearch("needle")().(SearchResultMessage)
	require.True(t, ok)
	require.Len(t, msg.Results, 12)

	m.Update(msg)

	for range 11 {
		press(m, keyText("j"))
	}

	assert.Equal(t, 11, m.searchCursor)
	assert.Positive(t, m.searchViewport.YOffset(), "scrolling down moves the viewport")

	for range 11 {
		press(m, keyText("k"))
	}

	assert.Equal(t, 0, m.searchCursor)
	assert.Equal(t, 0, m.searchViewport.YOffset(), "scrolling back up returns to the top")
}

func TestModel_AskWithoutFuncIsNoOp(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)

	assert.Nil(t, press(m, keyText("?")))
	assert.False(t, m.showAskInput)
}

func TestModel_AskInputKeys(t *testing.T) {
	t.Parallel()

	ask := func(string, func(string, logger.Level), func(string)) (string, error) { return "", nil }
	m := newSizedModel(t, WithAskFunc(ask))

	assert.NotNil(t, press(m, keyText("?")))
	assert.True(t, m.showAskInput)

	out := viewContent(m)
	assert.Contains(t, out, "Enter: Ask AI • Esc: Cancel")

	press(m, keyText("h"), keyText("i"))
	assert.Equal(t, "hi", m.askInput.Value())

	press(m, keyCode(tea.KeyEscape))
	assert.False(t, m.showAskInput)
	assert.False(t, m.asking)

	press(m, keyText("?"))
	assert.Nil(t, press(m, keyCode(tea.KeyEnter)), "enter with an empty question just closes")
	assert.False(t, m.showAskInput)
}

func drainBatch(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()

	batch, ok := cmd().(tea.BatchMsg)
	require.True(t, ok)

	msgs := make([]tea.Msg, 0, len(batch))
	for _, c := range batch {
		msgs = append(msgs, c())
	}

	return msgs
}

func TestModel_AskStreamsLogsDeltasAndResult(t *testing.T) {
	t.Parallel()

	var gotQuestion string

	ask := func(q string, logFn func(string, logger.Level), deltaFn func(string)) (string, error) {
		gotQuestion = q

		for i := range 7 {
			logFn(fmt.Sprintf("log %d", i), logger.InfoLevel)
		}

		deltaFn("partial ")
		deltaFn("answer")

		return "# Final answer", nil
	}
	m := newSizedModel(t, WithAskFunc(ask))

	press(m, keyText("?"), keyText("w"), keyText("h"), keyText("y"))
	cmd := press(m, keyCode(tea.KeyEnter))
	require.NotNil(t, cmd)
	assert.True(t, m.asking)
	assert.False(t, m.showAskInput)

	msgs := drainBatch(t, cmd)
	assert.Equal(t, "why", gotQuestion)
	require.Len(t, msgs, 3)

	result, ok := msgs[0].(AskResultMsg)
	require.True(t, ok)

	logMsg, ok := msgs[1].(AskLogMsg)
	require.True(t, ok)

	var next tea.Cmd

	for {
		_, next = m.Update(logMsg)
		require.NotNil(t, next)

		nextMsg := next()
		if _, done := nextMsg.(LogFinishedMsg); done {
			break
		}

		logMsg, ok = nextMsg.(AskLogMsg)
		require.True(t, ok)
	}

	assert.Len(t, m.askLogs, 7)

	out := viewContent(m)
	assert.Contains(t, out, "Thinking...")
	assert.Contains(t, out, "log 6")
	assert.NotContains(t, out, "log 0", "only the most recent logs are shown")

	deltaMsg, ok := msgs[2].(AskDeltaMsg)
	require.True(t, ok)

	for {
		_, next = m.Update(deltaMsg)
		require.NotNil(t, next)

		nextMsg := next()
		if nextMsg == nil {
			break
		}

		deltaMsg, ok = nextMsg.(AskDeltaMsg)
		require.True(t, ok)
	}

	assert.True(t, m.streaming)
	assert.Equal(t, "partial answer", m.content)
	assert.Equal(t, focusContent, m.focus)
	assert.Contains(t, viewContent(m), "partial answer", "streamed text replaces the thinking panel")

	m.Update(result)
	assert.False(t, m.asking)
	assert.False(t, m.streaming)
	assert.Contains(t, ansi.Strip(m.content), "Final answer")
}

func TestModel_AskResultError(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)
	m.asking = true

	_, cmd := m.Update(AskResultMsg{Err: errors.New("provider down")})
	assert.Nil(t, cmd)
	assert.Contains(t, m.content, "Error: provider down")
	assert.False(t, m.asking)
}

func TestModel_EscCancelsInFlightAsk(t *testing.T) {
	t.Parallel()

	m := newSizedModel(t)
	m.asking = true

	press(m, keyCode(tea.KeyEscape))
	assert.False(t, m.asking)
}

func TestModel_SidebarScrollsWithCursor(t *testing.T) {
	t.Parallel()

	items := make([]ListItem, 0, 60)
	for i := range 60 {
		items = append(items, ListItem{Title: fmt.Sprintf("item-%02d", i), Path: "index.md"})
	}

	m := newSizedModel(t)
	m.currentItems = items
	m.cursor = 59

	out := viewContent(m)
	assert.Contains(t, out, "item-59")
	assert.NotContains(t, out, "item-00", "items above the window are scrolled off")

	m.cursor = 0
	start, end := m.calculateVisibleRange(100)
	assert.Equal(t, 0, start)
	assert.Equal(t, 60, end)
}

func TestEnsureRenderer_UnknownStyleKeepsPreviousRenderer(t *testing.T) {
	t.Parallel()

	m := &Model{fs: fstest.MapFS{}, styleName: "no-such-glamour-style"}

	assert.Nil(t, m.ensureRenderer(80), "a style glamour cannot load leaves the renderer unset")
	assert.Equal(t, "# raw", m.renderMarkdown("# raw", 80), "rendering falls back to the raw text")
}

func TestModel_UpdateViewportSizeWaitsForWindowSize(t *testing.T) {
	t.Parallel()

	m := NewModel(newTestDocsFS())
	m.updateViewportSize()

	assert.Equal(t, 0, m.viewport.Height(), "no layout before the first WindowSizeMsg")
}

func TestModel_CalculateVisibleRangeTruncatesFromTop(t *testing.T) {
	t.Parallel()

	m := &Model{currentItems: make([]ListItem, 20)}

	start, end := m.calculateVisibleRange(5)
	assert.Equal(t, 0, start)
	assert.Equal(t, 5, end)
}

func TestWaitForAskDelta_ClosedChannelReturnsNil(t *testing.T) {
	t.Parallel()

	ch := make(chan string)
	close(ch)

	assert.Nil(t, waitForAskDelta(ch)())
	assert.IsType(t, LogFinishedMsg{}, waitForAskLog(ch)())
}
