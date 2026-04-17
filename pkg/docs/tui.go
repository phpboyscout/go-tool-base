package docs

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/regexutil"
)

// Styles.
var (
	styleSidebar = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)

	styleContent = lipgloss.NewStyle().
			Padding(1, contentBorderWidth)
	styleSelected = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true)

	styleDir = lipgloss.NewStyle().
			Foreground(lipgloss.Color("69"))

	styleHelp = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			PaddingTop(1)

	styleHeader = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true).
			Padding(0, 1).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(lipgloss.Color("62"))

	styleSearchModal = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("205")).
				Padding(1, contentBorderWidth)

	styleSearchResult = lipgloss.NewStyle().
				Foreground(lipgloss.Color("62"))

	styleRegexBadge = lipgloss.NewStyle().
			Bold(true).
			Background(lipgloss.Color("34")).
			Foreground(lipgloss.Color("0")).
			Padding(0, 1).
			MarginLeft(1)
)

type focus int

const (
	focusSidebar focus = iota
	focusContent
)

// ListItem represents a navigation entry in the documentation sidebar.
type ListItem struct {
	Title    string
	Path     string
	IsGroup  bool
	Children []NavNode
}

// SearchResult holds a single search match with context excerpt.
type SearchResult struct {
	Title   string
	Path    string
	Excerpt string
}

const (
	// Constants.
	defaultWordWrap     = 80
	defaultSidebarRatio = 0.25
	logBufferSize       = 100
	uiPadding           = 4
	contentBorderWidth  = 2
	maxAskLogs          = 5
	searchContextPre    = 50
	searchContextPost   = 150
	searchScrollMargin  = 2
	sidebarResizeDelta  = 0.05
	searchResultHeight  = 4
	verticalBorderWidth = 2
	searchInputWidth    = 40
	askInputWidth       = 60
)

// Model is the Bubble Tea model for the interactive documentation browser.
// It manages navigation, content rendering, search, and AI Q&A.
type Model struct {
	fs fs.FS

	// Navigation state
	navRoot      []NavNode
	currentItems []ListItem
	navStack     [][]ListItem
	titleStack   []string
	cursor       int

	// Search state
	searchInput       textinput.Model
	showSearchInput   bool
	showSearchResults bool
	searchResults     []SearchResult
	searchCursor      int
	lastQuery         string
	lastRegex         *regexp.Regexp
	useRegex          bool
	searchViewport    viewport.Model

	// Ask state
	askInput     textinput.Model
	showAskInput bool
	asking       bool
	streaming    bool // true while streamed deltas are arriving
	streamBuf    strings.Builder
	askLogs      []string
	askFunc      AskFunc

	// Configuration
	title string

	// Layout state
	sidebarOpen  bool
	sidebarRatio float64 // Percentage of width [0.1, 0.9]
	focus        focus

	// Cache (still useful for non-mkdocs rendering fallback if needed, or window titles)
	titles map[string]string

	// Component models
	viewport viewport.Model
	renderer *glamour.TermRenderer

	// App state
	content     string
	frontmatter string // Raw frontmatter content
	showInfo    bool   // Toggle to show frontmatter footer
	ready       bool
	width       int
	height      int

	mkdocsLoaded bool
}

// NewModel creates a documentation browser Model backed by the given filesystem.
func NewModel(fsys fs.FS, opts ...Option) *Model {
	r, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(defaultWordWrap),
	)

	ti := textinput.New()
	ti.Placeholder = "Search documentation..."
	ti.CharLimit = 156
	ti.SetWidth(searchInputWidth)

	ai := textinput.New()
	ai.Placeholder = "Ask a question about the docs..."
	ai.CharLimit = 256
	ai.SetWidth(askInputWidth)

	m := &Model{
		fs:             fsys,
		renderer:       r,
		sidebarOpen:    true,
		sidebarRatio:   defaultSidebarRatio,
		focus:          focusSidebar,
		titles:         make(map[string]string),
		searchInput:    ti,
		askInput:       ai,
		title:          "Documentation",
		searchViewport: viewport.New(),
	}

	for _, opt := range opts {
		opt(m)
	}

	// Try to load mkdocs navigation
	nav, err := parseMkDocsNav(fsys)
	if err == nil && len(nav) > 0 {
		m.mkdocsLoaded = true
		m.navRoot = nav
		m.currentItems = m.nodesToListItems(nav)
	}
	// Init search
	// Default to index.md if found in root
	m.loadFile("index.md")

	return m
}

// Option is a functional option for configuring the documentation Model.
type Option func(*Model)

// AskFunc is the callback signature for AI Q&A. logFn receives status messages;
// deltaFn receives streamed text fragments as they arrive (may be nil).
type AskFunc func(question string, logFn func(string, logger.Level), deltaFn func(string)) (string, error)

// WithTitle sets the title displayed at the top of the documentation browser.
func WithTitle(title string) Option {
	return func(m *Model) {
		m.title = title
	}
}

// WithAskFunc configures the AI Q&A callback for the documentation browser.
func WithAskFunc(fn AskFunc) Option {
	return func(m *Model) {
		m.askFunc = fn
	}
}

func (m *Model) nodesToListItems(nodes []NavNode) []ListItem {
	items := make([]ListItem, 0, len(nodes))

	for _, n := range nodes {
		item := ListItem{
			Title:    n.Title,
			Path:     n.Path,
			Children: n.Children,
			IsGroup:  len(n.Children) > 0,
		}
		items = append(items, item)
	}

	return items
}

func (m *Model) Init() tea.Cmd {
	return textinput.Blink
}

// SearchResultMessage is a Bubble Tea message carrying search results.
type SearchResultMessage struct {
	Results []SearchResult
	Query   string
}

// AskResultMsg is a Bubble Tea message carrying the AI Q&A response.
type AskResultMsg struct {
	Response string
	Err      error
}

// AskLogMsg is a Bubble Tea message carrying a log line from the AI provider.
type AskLogMsg struct {
	Log string
	Ch  <-chan string
}

// AskDeltaMsg is a Bubble Tea message carrying a streamed text fragment from the AI.
type AskDeltaMsg struct {
	Delta string
	Ch    <-chan string
}

// LogFinishedMsg is a Bubble Tea message signalling that AI log streaming is complete.
type LogFinishedMsg struct{}

func waitForAskLog(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return LogFinishedMsg{}
		}

		return AskLogMsg{Log: msg, Ch: ch}
	}
}

func waitForAskDelta(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		delta, ok := <-ch
		if !ok {
			return nil
		}

		return AskDeltaMsg{Delta: delta, Ch: ch}
	}
}

func (m *Model) performSearch(query string) tea.Cmd {
	return func() tea.Msg {
		results := []SearchResult{}

		var lastRegex *regexp.Regexp

		if query == "" {
			return SearchResultMessage{Results: results, Query: query}
		}

		if m.useRegex {
			// User-supplied search query — bound compile time against
			// ReDoS. Closes H-3 from
			// docs/development/reports/security-audit-2026-04-17.md.
			re, err := regexutil.CompileBoundedTimeout("(?i)"+query, regexutil.DefaultCompileTimeout)
			if err != nil {
				return SearchResultMessage{Results: results, Query: query}
			}

			lastRegex = re
		}

		// Walk FS
		_ = fs.WalkDir(m.fs, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() {
				return nil
			}

			if !strings.HasSuffix(path, ".md") {
				return nil
			}

			content, err := fs.ReadFile(m.fs, path)
			if err != nil {
				return nil //nolint:nilerr // intentionally skip unreadable files during search
			}

			text := string(content)
			matchIndex, matchLen := m.searchMatch(text, query, lastRegex)

			if matchIndex != -1 {
				// Found logical match
				title := extractTitle(m.fs, path)
				if title == "" {
					title = path
				}

				excerpt := extractExcerpt(text, matchIndex, matchLen)

				results = append(results, SearchResult{
					Title:   title,
					Path:    path,
					Excerpt: excerpt,
				})
			}

			return nil
		})

		return SearchResultMessage{Results: results, Query: query}
	}
}

func (m *Model) searchMatch(text, query string, re *regexp.Regexp) (int, int) {
	if re != nil {
		loc := re.FindStringIndex(text)
		if loc != nil {
			return loc[0], loc[1] - loc[0]
		}

		return -1, 0
	}

	if idx := strings.Index(strings.ToLower(text), strings.ToLower(query)); idx != -1 {
		return idx, len(query)
	}

	return -1, 0
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m, m.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.updateViewportSize()

	case AskResultMsg:
		m.handleAskResult(msg)

		return m, nil

	case AskDeltaMsg:
		m.handleAskDelta(msg)

		return m, waitForAskDelta(msg.Ch)

	case AskLogMsg:
		m.askLogs = append(m.askLogs, msg.Log)

		return m, waitForAskLog(msg.Ch)

	case SearchResultMessage:
		m.handleSearchResultMsg(msg)

		return m, nil
	}

	return m, nil
}

func (m *Model) handleAskDelta(msg AskDeltaMsg) {
	m.streaming = true
	m.streamBuf.WriteString(msg.Delta)
	// Show raw text while streaming — styled final render happens in handleAskResult.
	m.content = m.streamBuf.String()
	m.viewport.SetContent(m.content)
	m.viewport.GotoBottom()
	m.focus = focusContent
}

func (m *Model) handleAskResult(msg AskResultMsg) {
	m.asking = false
	m.streaming = false
	m.streamBuf.Reset()

	if msg.Err != nil {
		m.content = fmt.Sprintf("\nError: %v\n", msg.Err)
	} else {
		rendered, err := m.renderer.Render(msg.Response)
		if err != nil {
			rendered = msg.Response
		}

		m.content = rendered
	}

	m.showSearchResults = false
	m.viewport.SetContent(m.content)
	m.viewport.GotoTop()
	m.focus = focusContent
}

func (m *Model) handleSearchResultMsg(msg SearchResultMessage) {
	m.searchResults = msg.Results
	m.lastQuery = msg.Query

	if m.useRegex && msg.Query != "" {
		// Bounded compile — see performSearch; same H-3 mitigation.
		m.lastRegex, _ = regexutil.CompileBoundedTimeout("(?i)"+msg.Query, regexutil.DefaultCompileTimeout)
	} else {
		m.lastRegex = nil
	}

	m.showSearchResults = true
	m.searchCursor = 0
	m.updateSearchResults()
	m.searchViewport.GotoTop()
}

func (m *Model) handleKeyMsg(msg tea.KeyPressMsg) tea.Cmd {
	// Global keys (Ctrl+C)
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}

	// Mode-specific handling
	if m.showSearchInput {
		return m.handleSearchInputKey(msg)
	}

	if m.showAskInput || m.asking {
		return m.handleAskInputKey(msg)
	}

	if m.showSearchResults {
		return m.handleSearchResultsKey(msg)
	}

	if cmd, handled := m.handleGlobalKey(msg); handled {
		return cmd
	}

	return m.handleNormalKey(msg)
}

func (m *Model) handleSearchInputKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+r":
		m.useRegex = !m.useRegex

		return nil
	case "enter":
		query := m.searchInput.Value()
		m.showSearchInput = false
		m.searchInput.Blur()

		if query == "" {
			m.searchResults = []SearchResult{}
			m.showSearchResults = false

			return nil
		}

		return m.performSearch(query)
	case "esc":
		m.showSearchInput = false
		m.searchInput.Blur()

		return nil
	}

	var cmd tea.Cmd

	m.searchInput, cmd = m.searchInput.Update(msg)

	return cmd
}

func (m *Model) handleAskInputKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		if m.askFunc != nil && m.askInput.Value() != "" {
			m.asking = true
			m.streaming = false
			m.streamBuf.Reset()
			m.showAskInput = false
			question := m.askInput.Value()
			m.askLogs = []string{}

			logCh := make(chan string, logBufferSize)
			deltaCh := make(chan string, logBufferSize)

			askCmd := func() tea.Msg {
				ans, err := m.askFunc(
					question,
					func(s string, level logger.Level) { logCh <- s },
					func(delta string) { deltaCh <- delta },
				)

				close(logCh)
				close(deltaCh)

				return AskResultMsg{Response: ans, Err: err}
			}

			return tea.Batch(askCmd, waitForAskLog(logCh), waitForAskDelta(deltaCh))
		}

		m.showAskInput = false
		m.askInput.Blur()

		return nil
	case "esc":
		m.showAskInput = false
		m.asking = false
		m.askInput.Blur()

		return nil
	}

	var cmd tea.Cmd

	m.askInput, cmd = m.askInput.Update(msg)

	return cmd
}

func (m *Model) handleSearchResultsKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		m.showSearchResults = false
		m.focus = focusSidebar

		return nil
	case "down", "j":
		m.scrollSearchResults(1)
	case "up", "k":
		m.scrollSearchResults(-1)
	case "enter":
		if len(m.searchResults) > 0 {
			res := m.searchResults[m.searchCursor]
			m.loadFile(res.Path)
			m.showSearchResults = false
			m.focus = focusContent
		}
	case "s":
		// Search again
		m.showSearchResults = false

		return m.toggleSearch()
	case "r":
		m.useRegex = !m.useRegex

		return m.performSearch(m.lastQuery)
	}

	return nil
}

func (m *Model) scrollSearchResults(delta int) {
	newCursor := m.searchCursor + delta

	if newCursor < 0 || newCursor >= len(m.searchResults) {
		return
	}

	m.searchCursor = newCursor
	m.updateSearchResults()

	if delta > 0 {
		// Scrolling down
		if m.searchCursor*searchResultHeight > m.searchViewport.YOffset()+m.searchViewport.Height()-searchResultHeight {
			m.searchViewport.SetYOffset(m.searchViewport.YOffset() + searchResultHeight)
		}
	} else {
		// Scrolling up
		if m.searchCursor*searchResultHeight < m.searchViewport.YOffset() {
			m.searchViewport.SetYOffset(m.searchViewport.YOffset() - searchResultHeight)
		}
	}
}

func (m *Model) handleGlobalKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "q": // Quit if not typing
		return tea.Quit, true
	case "s":
		return m.toggleSearch(), true
	case "?":
		return m.toggleAsk(), true
	case "tab":
		m.toggleSidebar()

		return nil, true
	case ">":
		m.resizeSidebar(sidebarResizeDelta)

		return nil, true
	case "<":
		m.resizeSidebar(-sidebarResizeDelta)

		return nil, true
	case "i":
		if m.frontmatter != "" {
			m.showInfo = !m.showInfo
			m.updateViewportSize()
		}

		return nil, true
	}

	return nil, false
}

func (m *Model) resizeSidebar(delta float64) {
	newRatio := m.sidebarRatio + delta
	if m.sidebarOpen && newRatio >= 0.1 && newRatio <= 0.8 {
		m.sidebarRatio = newRatio
		m.updateViewportSize()
	}
}

func (m *Model) toggleSearch() tea.Cmd {
	m.showSearchInput = true
	m.searchInput.Focus()
	m.searchInput.SetValue("")

	return textinput.Blink
}

func (m *Model) toggleAsk() tea.Cmd {
	if m.askFunc != nil {
		m.asking = false
		m.showAskInput = true
		m.askLogs = nil
		m.askInput.Focus()
		m.askInput.SetValue("")

		return textinput.Blink
	}

	return nil
}

func (m *Model) toggleSidebar() {
	m.sidebarOpen = !m.sidebarOpen
	if !m.sidebarOpen {
		m.focus = focusContent
	} else if m.content == "" {
		m.focus = focusSidebar
	}

	m.updateViewportSize()
}

func (m *Model) handleContentCmd(msg tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd

	m.viewport, cmd = m.viewport.Update(msg)

	return cmd
}

func (m *Model) handleSidebarNav(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.currentItems)-1 {
			m.cursor++
		}
	case "left", "h", "backspace":
		if len(m.navStack) > 0 {
			// Pop stack
			last := len(m.navStack) - 1
			m.currentItems = m.navStack[last]
			m.navStack = m.navStack[:last]

			// Pop title stack
			if len(m.titleStack) > 0 {
				m.titleStack = m.titleStack[:len(m.titleStack)-1]
			}

			m.cursor = 0 // Reset cursor on back
		}
	case "right", "l":
		m.handleSelect()
	case "enter":
		m.handleSelect()
	}
}

func (m *Model) handleSelect() {
	if len(m.currentItems) == 0 {
		return
	}

	selected := m.currentItems[m.cursor]

	if selected.IsGroup {
		// Push current to stack
		m.navStack = append(m.navStack, m.currentItems)
		m.titleStack = append(m.titleStack, selected.Title)
		// Enter group
		m.currentItems = m.nodesToListItems(selected.Children)
		m.cursor = 0
	} else if selected.Path != "" {
		// It's a file
		m.loadFile(selected.Path)
		m.focus = focusContent
	}
}

func (m *Model) loadFile(path string) {
	content, err := fs.ReadFile(m.fs, path)
	if err != nil {
		return
	}

	// Strip frontmatter if present (robust regex)
	textContent := string(content)
	re := regexp.MustCompile(`(?s)^---\n(.*?)\n---\n`)

	matches := re.FindStringSubmatch(textContent)
	if len(matches) > 1 {
		m.frontmatter = strings.TrimSpace(matches[1])
		textContent = re.ReplaceAllString(textContent, "")
	} else {
		m.frontmatter = ""
	}

	sidebarWidth := m.sidebarWidth()
	contentWidth := m.width - sidebarWidth - uiPadding

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(contentWidth),
	)
	m.renderer = renderer

	rendered, err := m.renderer.Render(textContent)
	if err != nil {
		rendered = textContent
	}

	m.content = rendered
	m.viewport.SetContent(rendered)
	m.viewport.GotoTop()
}

func (m *Model) updateViewportSize() {
	if !m.ready {
		return
	}

	headerHeight := 2
	verticalMargin := headerHeight

	sidebarWidth := m.sidebarWidth()
	contentWidth := m.width - sidebarWidth - uiPadding

	if m.sidebarOpen {
		contentWidth -= 2
	}

	contentWidth -= 2

	// Calculate available height for UI components (Content Box)
	availableHeight := m.height - verticalMargin - contentBorderWidth

	// Adjust for padding inside the content box (styleContent Padding(1, 2))
	// 1 top + 1 bottom = 2 lines of padding
	viewportHeight := availableHeight - verticalBorderWidth

	// If info is shown, subtract its height from the viewport
	if m.showInfo && m.frontmatter != "" {
		// Footer content calculation to determine height
		// We use a temporary width for wrapping calculation (contentWidth matches viewport info)
		// Note: Footer is now inside the box, so it shares the width
		content := fmt.Sprintf("Frontmatter:\n\n%s", m.frontmatter)

		// Use a renderer or style to estimate height given width
		// Simple approach: Render it with current width
		r, _ := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(contentWidth),
		)

		renderedInfo, err := r.Render(content)
		if err != nil {
			renderedInfo = content
		}

		footerHeight := lipgloss.Height(renderedInfo) + 1 // +1 for spacing/divider
		viewportHeight -= footerHeight
	}

	m.viewport.SetWidth(contentWidth)
	m.viewport.SetHeight(viewportHeight)

	// Update search viewport
	m.searchViewport.SetWidth(contentWidth)
	m.searchViewport.SetHeight(m.viewport.Height())
}

func (m *Model) sidebarWidth() int {
	if !m.sidebarOpen {
		return 0
	}

	return int(float64(m.width) * m.sidebarRatio)
}

func (m *Model) View() tea.View {
	var content string
	if !m.ready {
		content = "Initializing..."
	} else {
		content = m.renderSplitView()
	}

	v := tea.NewView(content)
	v.AltScreen = true

	return v
}

func (m *Model) renderSplitView() string {
	// Render Header
	header := styleHeader.Width(m.width).Render(m.title)
	headerHeight := lipgloss.Height(header)

	// Available height for the boxes themselves
	// Logic in updateViewportSize handles 'availableHeight' as (height - header - border)
	// So we should match that here.
	height := m.height - headerHeight - contentBorderWidth

	sidebarView := m.renderSidebar(height)
	contentView := m.renderContent(height)

	mainView := lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, contentView)

	return lipgloss.JoinVertical(lipgloss.Left, header, mainView, m.helpView())
}

func (m *Model) renderSidebar(height int) string {
	if !m.sidebarOpen {
		return ""
	}

	var s strings.Builder

	// Simple scrolling
	maxFiles := max(height-uiPadding, 1)

	// Title/Context
	title := m.title
	if len(m.titleStack) > 0 {
		title = m.titleStack[len(m.titleStack)-1]
	} else if len(m.navStack) > 0 {
		title = "..."
	}

	s.WriteString(lipgloss.NewStyle().Underline(true).Bold(true).Render(title) + "\n\n")

	start, end := m.calculateVisibleRange(maxFiles)

	for i := start; i < end; i++ {
		s.WriteString(m.renderSidebarItem(i))
	}

	style := styleSidebar.Width(m.sidebarWidth()).Height(height)
	if m.focus == focusSidebar {
		style = style.BorderForeground(lipgloss.Color("205"))
	}

	return style.Render(s.String())
}

func (m *Model) calculateVisibleRange(maxFiles int) (int, int) {
	end := len(m.currentItems)

	start := 0
	if m.cursor >= maxFiles {
		start = m.cursor - maxFiles + 1
	}

	if end > start+maxFiles {
		return start, start + maxFiles
	}

	return start, end
}

func (m *Model) renderSidebarItem(i int) string {
	item := m.currentItems[i]

	cursor := "  "
	if m.focus == focusSidebar && m.cursor == i {
		cursor = "> "
	}

	title := item.Title
	if item.IsGroup {
		title += "/"
	}

	style := lipgloss.NewStyle()
	if m.focus == focusSidebar && m.cursor == i {
		style = styleSelected
	} else if item.IsGroup {
		style = styleDir
	}

	return fmt.Sprintf("%s%s\n", cursor, style.Render(title))
}

func (m *Model) renderContent(height int) string {
	if m.showSearchInput {
		content := m.searchInput.View()
		if m.useRegex {
			content += "\n\n" + styleRegexBadge.Render("REGEX MODE (Ctrl+R)")
		} else {
			content += "\n\n" + lipgloss.NewStyle().Faint(true).Render("Press Ctrl+R for Regex")
		}

		return lipgloss.Place(
			m.width-m.sidebarWidth()-uiPadding,
			height,
			lipgloss.Center,
			lipgloss.Center,
			styleSearchModal.Render(content),
		)
	}

	if m.showAskInput || (m.asking && !m.streaming) {
		return m.renderAskInput(height)
	}

	if m.showSearchResults {
		return m.renderSearchResults(height)
	}

	if m.content == "" {
		return ""
	}

	return m.renderMainContent(height)
}

func (m *Model) renderAskInput(height int) string {
	var logView string

	if len(m.askLogs) > 0 {
		// Show last few logs
		start := 0
		if len(m.askLogs) > maxAskLogs {
			start = len(m.askLogs) - maxAskLogs
		}

		logView = "\n\n" + strings.Join(m.askLogs[start:], "\n")
	}

	content := m.askInput.View() + logView
	if m.asking {
		content = "Thinking...\n\n" + logView
	}

	return lipgloss.Place(
		m.width-m.sidebarWidth()-uiPadding,
		height,
		lipgloss.Center,
		lipgloss.Center,
		styleSearchModal.Render(content),
	)
}

func (m *Model) renderMainContent(height int) string {
	// Prepare Footer content if needed
	var footer string

	if m.showInfo && m.frontmatter != "" {
		// Just styled text
		content := fmt.Sprintf("\n%s\n\n%s", strings.Repeat("─", m.viewport.Width()), m.frontmatter)
		footer = lipgloss.NewStyle().Faint(true).Render(content)
	}

	view := m.viewport.View()
	if footer != "" {
		view = lipgloss.JoinVertical(lipgloss.Left, view, footer)
	}

	style := styleContent.Width(m.width - m.sidebarWidth() - uiPadding).Height(height)

	if m.focus == focusContent {
		style = style.Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("205"))
	} else {
		style = style.Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62"))
	}

	return style.Render(view)
}

func (m *Model) renderSearchResults(height int) string {
	style := styleSearchModal.Width(m.width - m.sidebarWidth() - uiPadding).Height(height)

	return style.Render(m.searchViewport.View())
}

func (m *Model) handleNormalKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.focus == focusSidebar {
		m.handleSidebarNav(msg)

		return nil
	}

	// Content focus
	switch msg.String() {
	case "left", "h":
		// Switch focus to sidebar
		m.focus = focusSidebar

		return nil
	}

	return m.handleContentCmd(msg)
}

func (m *Model) updateSearchResults() {
	var s strings.Builder

	s.WriteString(lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("Search Results: %s", m.lastQuery)))

	if m.useRegex {
		s.WriteString(styleRegexBadge.Render("REGEX"))
	}

	s.WriteString("\n\n")

	if len(m.searchResults) == 0 {
		s.WriteString("No results found.")
	} else {
		for i, res := range m.searchResults {
			cursor := " "
			if i == m.searchCursor {
				cursor = ">"
			}

			style := styleSearchResult
			if i == m.searchCursor {
				style = style.Bold(true).Foreground(lipgloss.Color("205"))
			}

			fmt.Fprintf(&s, "%s %s\n", cursor, style.Render(res.Title))
			s.WriteString(lipgloss.NewStyle().Faint(true).Render(fmt.Sprintf("  %s", res.Path)) + "\n")
			s.WriteString(lipgloss.NewStyle().Italic(true).Render(fmt.Sprintf("  %s", res.Excerpt)) + "\n\n")
		}
	}

	m.searchViewport.SetContent(s.String())
	// Ensure cursor is visible - crude approximation for now, just sync top?
	// Actually, we should probably scroll to make sure the selected item is in view
	// For now, let's just make it scrollable content.
}

func (m *Model) helpView() string {
	var help string

	switch {
	case m.showSearchInput:
		help = "Enter: Search • Esc: Cancel"
	case m.showAskInput:
		help = "Enter: Ask AI • Esc: Cancel"
	case m.showSearchResults:
		help = "↑/↓: Navigate • Enter: Open • S: Search Again • R: Toggle Regex • Esc: Close"
	default:
		help = "Tab: Toggle Sidebar • S: Search • ?: Ask AI • Q: Quit • I: Info"
		if m.focus == focusSidebar {
			help += " • ↑/↓: Navigate • Enter/→: Select"
		} else {
			help += " • ↑/↓: Scroll • ←: Focus Sidebar"
		}
	}

	return styleHelp.Render(help)
}

func extractExcerpt(text string, start, length int) string {
	// Extract context
	ctxStart := max(0, start-searchContextPre)
	ctxEnd := min(len(text), start+length+searchContextPost)

	excerpt := text[ctxStart:ctxEnd]
	excerpt = strings.ReplaceAll(excerpt, "\n", " ")

	return "..." + excerpt + "..."
}
