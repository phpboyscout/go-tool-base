package generator

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/pmezard/go-difflib/difflib"
)

type diffResult int

const (
	diffResultPending diffResult = iota
	diffResultOverwrite
	diffResultKeep
)

type diffPagerModel struct {
	viewport viewport.Model
	path     string
	content  string
	result   diffResult
	ready    bool
}

func (m diffPagerModel) Init() tea.Cmd {
	return nil
}

func (m diffPagerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		headerHeight := 2
		footerHeight := 2
		verticalMargins := headerHeight + footerHeight

		if !m.ready {
			m.viewport = viewport.New(
				viewport.WithWidth(msg.Width),
				viewport.WithHeight(msg.Height-verticalMargins),
			)
			m.viewport.YPosition = headerHeight
			m.viewport.SetContent(m.content)
			m.ready = true
		} else {
			m.viewport.SetWidth(msg.Width)
			m.viewport.SetHeight(msg.Height - verticalMargins)
		}

	case tea.KeyPressMsg:
		switch msg.String() {
		case "y", "Y":
			m.result = diffResultOverwrite

			return m, tea.Quit
		case "n", "N", "q", "Q", "esc":
			m.result = diffResultKeep

			return m, tea.Quit
		}
	}

	var cmd tea.Cmd

	m.viewport, cmd = m.viewport.Update(msg)

	return m, cmd
}

const (
	diffContextLines        = 5
	diffScrollPctMultiplier = 100
)

var (
	diffHeaderStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99")).PaddingBottom(1)
	diffFooterStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	diffAddStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	diffRemoveStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	diffHunkStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	diffContextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
)

func (m diffPagerModel) View() tea.View {
	if !m.ready {
		v := tea.NewView("Initializing...")
		v.AltScreen = true

		return v
	}

	scrollPct := int(m.viewport.ScrollPercent() * diffScrollPctMultiplier)
	header := diffHeaderStyle.Render("Diff: " + m.path)
	footer := diffFooterStyle.Render(fmt.Sprintf("↑/↓ scroll  pgup/pgdn page  y overwrite  n keep  (%d%%)", scrollPct))

	v := tea.NewView(header + "\n" + m.viewport.View() + "\n" + footer)
	v.AltScreen = true

	return v
}

func coloriseDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	out := make([]string, 0, len(lines))

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			out = append(out, diffHunkStyle.Render(line))
		case strings.HasPrefix(line, "+"):
			out = append(out, diffAddStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			out = append(out, diffRemoveStyle.Render(line))
		case strings.HasPrefix(line, "@@"):
			out = append(out, diffHunkStyle.Render(line))
		default:
			out = append(out, diffContextStyle.Render(line))
		}
	}

	return strings.Join(out, "\n")
}

// runDiffPager shows a full-screen scrollable diff between existing and new
// content and returns true if the user chose to overwrite, false to keep.
func runDiffPager(path string, existing, newContent []byte) bool {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(existing)),
		B:        difflib.SplitLines(string(newContent)),
		FromFile: path + " (current)",
		ToFile:   path + " (incoming)",
		Context:  diffContextLines,
	})
	if err != nil || diff == "" {
		return false
	}

	m := diffPagerModel{path: path, content: coloriseDiff(diff)}

	p := tea.NewProgram(m)

	final, err := p.Run()
	if err != nil {
		return false
	}

	return final.(diffPagerModel).result == diffResultOverwrite
}
