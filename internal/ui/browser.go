package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joebhakim/themer/internal/core"
)

type profileItem struct {
	name string
}

func (p profileItem) FilterValue() string { return p.name }
func (p profileItem) Title() string       { return p.name }
func (p profileItem) Description() string { return "profile" }

type detailsMsg struct {
	profile string
	details []core.AdapterDetail
	err     error
}

type previewReadyMsg struct {
	profile string
	seq     int
}

type previewDoneMsg struct {
	seq int
	err error
}

type applyDoneMsg struct {
	results []core.ApplyResult
	err     error
}

type restoreDoneMsg struct {
	err error
}

type Browser struct {
	manager         *core.Manager
	list            list.Model
	details         []core.AdapterDetail
	detailProfile   string
	detailErr       error
	viewport        viewport.Model
	width           int
	height          int
	previewSeq      int
	previewOnMove   bool
	previewDebounce time.Duration
	status          string
	quitting        bool
	applying        bool
}

func NewBrowser(manager *core.Manager) Browser {
	items := make([]list.Item, 0, len(manager.ProfileNames()))
	for _, name := range manager.ProfileNames() {
		items = append(items, profileItem{name: name})
	}
	l := list.New(items, list.NewDefaultDelegate(), 24, 10)
	l.Title = "Profiles"
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(false)
	l.SetShowHelp(true)
	l.DisableQuitKeybindings()
	vp := viewport.New(20, 10)

	return Browser{
		manager:         manager,
		list:            l,
		viewport:        vp,
		previewOnMove:   manager.Config().UI.PreviewOnMove,
		previewDebounce: time.Duration(manager.Config().UI.PreviewDebounce) * time.Millisecond,
		status:          "enter: apply  esc: cancel  /: filter",
	}
}

func (b Browser) Init() tea.Cmd {
	return b.refreshSelected()
}

func (b Browser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.width = msg.Width
		b.height = msg.Height
		listWidth := msg.Width / 3
		if listWidth < 24 {
			listWidth = 24
		}
		b.list.SetSize(listWidth, max(msg.Height-2, 8))
		detailWidth := max(msg.Width-listWidth-3, 20)
		b.viewport.Width = detailWidth
		b.viewport.Height = max(msg.Height-4, 6)
		b.viewport.SetContent(renderDetails(b.detailProfile, b.details, b.detailErr))
		return b, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			b.quitting = true
			return b, restoreAndQuitCmd(b.manager)
		case "enter":
			if b.applying {
				return b, nil
			}
			b.applying = true
			return b, applyCmd(b.manager, b.selected())
		}
	}

	previous := b.selected()
	var cmd tea.Cmd
	b.list, cmd = b.list.Update(msg)
	if current := b.selected(); current != previous {
		return b, tea.Batch(cmd, b.refreshSelected())
	}

	switch msg := msg.(type) {
	case detailsMsg:
		if msg.profile == b.selected() {
			b.detailProfile = msg.profile
			b.details = msg.details
			b.detailErr = msg.err
			b.viewport.SetContent(renderDetails(b.detailProfile, b.details, b.detailErr))
		}
	case previewReadyMsg:
		if msg.profile == b.selected() && b.previewOnMove {
			return b, previewCmd(b.manager, msg.profile, msg.seq)
		}
	case previewDoneMsg:
		if msg.seq == b.previewSeq && msg.err != nil {
			b.status = "preview warning: " + msg.err.Error()
		} else if msg.seq == b.previewSeq {
			b.status = fmt.Sprintf("previewing %s", b.selected())
		}
	case applyDoneMsg:
		b.applying = false
		if msg.err != nil {
			b.status = "apply failed: " + msg.err.Error()
			return b, nil
		}
		b.quitting = true
		return b, tea.Quit
	case restoreDoneMsg:
		if msg.err != nil {
			b.status = "restore warning: " + msg.err.Error()
		}
		if b.quitting {
			return b, tea.Quit
		}
	}
	return b, cmd
}

func (b Browser) View() string {
	listPane := b.list.View()
	detailPane := b.viewport.View()
	header := lipgloss.NewStyle().Bold(true).Render("themer v2")
	status := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(b.status)
	body := lipgloss.JoinHorizontal(lipgloss.Top, listPane, "  ", detailPane)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, status)
}

func (b Browser) selected() string {
	item, ok := b.list.SelectedItem().(profileItem)
	if !ok {
		return ""
	}
	return item.name
}

func (b *Browser) refreshSelected() tea.Cmd {
	selected := b.selected()
	if selected == "" {
		return nil
	}
	b.previewSeq++
	seq := b.previewSeq
	cmds := []tea.Cmd{
		loadDetailsCmd(b.manager, selected),
	}
	if b.previewOnMove {
		cmds = append(cmds, previewDelayCmd(selected, seq, b.previewDebounce))
	}
	return tea.Batch(cmds...)
}

func loadDetailsCmd(manager *core.Manager, profile string) tea.Cmd {
	return func() tea.Msg {
		details, err := manager.DescribeProfile(context.Background(), profile)
		return detailsMsg{profile: profile, details: details, err: err}
	}
}

func previewDelayCmd(profile string, seq int, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return previewReadyMsg{profile: profile, seq: seq}
	})
}

func previewCmd(manager *core.Manager, profile string, seq int) tea.Cmd {
	return func() tea.Msg {
		err := manager.PreviewProfile(context.Background(), profile, seq)
		return previewDoneMsg{seq: seq, err: err}
	}
}

func applyCmd(manager *core.Manager, profile string) tea.Cmd {
	return func() tea.Msg {
		results, err := manager.ApplyProfile(context.Background(), profile)
		if err == nil {
			_ = manager.RestorePreview(context.Background())
		}
		return applyDoneMsg{results: results, err: err}
	}
}

func restoreAndQuitCmd(manager *core.Manager) tea.Cmd {
	return func() tea.Msg {
		err := manager.RestorePreview(context.Background())
		return restoreDoneMsg{err: err}
	}
}

func renderDetails(profile string, details []core.AdapterDetail, err error) string {
	if err != nil {
		return "Failed to load profile details:\n\n" + err.Error()
	}
	if profile == "" {
		return "No profile selected."
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("Profile: %s", profile), "")
	for _, detail := range details {
		lines = append(lines, fmt.Sprintf("%s", detail.Display))
		if detail.Theme == "" {
			lines = append(lines, "  not targeted")
			lines = append(lines, "")
			continue
		}
		lines = append(lines, fmt.Sprintf("  target:  %s", detail.Theme))
		if detail.Current != "" {
			lines = append(lines, fmt.Sprintf("  current: %s", detail.Current))
		}
		if detail.PreviewSupport.Enabled {
			lines = append(lines, "  preview: enabled")
		} else {
			lines = append(lines, "  preview: apply-only")
			if detail.PreviewSupport.Reason != "" {
				lines = append(lines, "  reason:  "+detail.PreviewSupport.Reason)
			}
		}
		if detail.Description.Summary != "" {
			lines = append(lines, "  "+detail.Description.Summary)
		}
		for _, palette := range detail.Description.Palette {
			lines = append(lines, fmt.Sprintf("  %s: %s", palette.Label, palette.Value))
		}
		for _, note := range detail.Description.Notes {
			lines = append(lines, "  note: "+note)
		}
		if detail.Error != "" {
			lines = append(lines, "  error: "+detail.Error)
		}
		lines = append(lines, "")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
