package ui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joebhakim/themer/internal/core"
)

type profileItem struct {
	name        string
	description string
	quit        bool
}

func (p profileItem) FilterValue() string { return p.name }
func (p profileItem) Title() string       { return p.name }
func (p profileItem) Description() string { return p.description }

type detailsMsg struct {
	profile string
	details []core.AdapterDetail
	err     error
}

type previewReadyMsg struct {
	profile string
	seq     int
}

type activityMsg struct {
	entry core.ActivityEntry
}

type applyDoneMsg struct {
	results []core.ApplyResult
	err     error
	warning string
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
	activity        []core.ActivityEntry
	quitting        bool
	applying        bool
	restoring       bool
}

var hexColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

var (
	applyFlushTimeout = 2 * time.Second
	restoreCmdTimeout = 1500 * time.Millisecond
	previewWarnPrefix = "preview cleanup"
)

func NewBrowser(manager *core.Manager) Browser {
	items := make([]list.Item, 0, len(manager.ProfileNames())+1)
	for _, name := range manager.ProfileNames() {
		items = append(items, profileItem{name: name, description: "profile"})
	}
	items = append(items, profileItem{name: "Exit Themer", description: "quit", quit: true})
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
		activity:        manager.ActivityLog(),
		status:          "enter: apply/quit  esc: cancel  /: filter",
	}
}

func (b Browser) Init() tea.Cmd {
	return tea.Batch(b.refreshSelected(), watchActivityCmd(b.manager))
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
		b.syncViewport()
		return b, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			if b.restoring {
				return b, tea.Quit
			}
			return b, b.beginQuit()
		case "enter":
			if b.applying {
				return b, nil
			}
			if b.selectedItem().quit {
				if b.restoring {
					return b, tea.Quit
				}
				return b, b.beginQuit()
			}
			b.applying = true
			b.status = "flushing preview before apply"
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
			b.syncViewport()
		}
	case activityMsg:
		b.activity = append(b.activity, msg.entry)
		if len(b.activity) > 80 {
			b.activity = append([]core.ActivityEntry(nil), b.activity[len(b.activity)-80:]...)
		}
		if status := activityStatus(msg.entry); status != "" {
			b.status = status
		}
		b.syncViewport()
		return b, watchActivityCmd(b.manager)
	case previewReadyMsg:
		if msg.profile == b.selected() && b.previewOnMove {
			b.manager.QueuePreview(msg.profile, msg.seq)
			if msg.seq == b.previewSeq {
				b.status = fmt.Sprintf("preview queued for %s", msg.profile)
			}
		}
	case applyDoneMsg:
		b.applying = false
		if msg.err != nil {
			if msg.warning != "" {
				b.status = "apply failed: " + msg.err.Error() + "; " + msg.warning
			} else {
				b.status = "apply failed: " + msg.err.Error()
			}
			return b, nil
		}
		b.manager.CommitPreview()
		b.status = "applied " + b.selected()
		if msg.warning != "" {
			b.status += "; " + msg.warning
		}
		return b, loadDetailsCmd(b.manager, b.selected())
	case restoreDoneMsg:
		b.restoring = false
		if msg.err != nil {
			b.status = "restore warning: " + msg.err.Error()
		}
		b.syncViewport()
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
	return b.selectedItem().name
}

func (b Browser) selectedItem() profileItem {
	item, ok := b.list.SelectedItem().(profileItem)
	if !ok {
		return profileItem{}
	}
	return item
}

func (b *Browser) refreshSelected() tea.Cmd {
	selected := b.selectedItem()
	if selected.name == "" {
		return nil
	}
	if selected.quit {
		b.manager.CancelPendingPreview()
		b.detailProfile = selected.name
		b.details = nil
		b.detailErr = nil
		b.status = "enter: quit  esc: cancel  /: filter"
		b.syncViewport()
		return nil
	}
	b.status = "enter: apply/quit  esc: cancel  /: filter"
	b.previewSeq++
	seq := b.previewSeq
	cmds := []tea.Cmd{
		loadDetailsCmd(b.manager, selected.name),
	}
	if b.previewOnMove {
		cmds = append(cmds, previewDelayCmd(selected.name, seq, b.previewDebounce))
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

func applyCmd(manager *core.Manager, profile string) tea.Cmd {
	return func() tea.Msg {
		var warning string
		flushCtx, cancel := context.WithTimeout(context.Background(), applyFlushTimeout)
		err := manager.FlushPreview(flushCtx)
		cancel()
		if err != nil {
			warning = summarizeControlWarning(previewWarnPrefix, err)
		}
		results, err := manager.ApplyProfile(context.Background(), profile)
		return applyDoneMsg{results: results, err: err, warning: warning}
	}
}

func restorePreviewCmd(manager *core.Manager) tea.Cmd {
	return func() tea.Msg {
		restoreCtx, cancel := context.WithTimeout(context.Background(), restoreCmdTimeout)
		err := manager.RestorePreview(restoreCtx)
		cancel()
		return restoreDoneMsg{err: err}
	}
}

func watchActivityCmd(manager *core.Manager) tea.Cmd {
	return func() tea.Msg {
		entry, ok := <-manager.ActivityStream()
		if !ok {
			return nil
		}
		return activityMsg{entry: entry}
	}
}

func (b *Browser) beginQuit() tea.Cmd {
	b.quitting = true
	b.restoring = true
	b.status = "restoring preview before exit; press q again to force quit"
	return restorePreviewCmd(b.manager)
}

func summarizeControlWarning(prefix string, err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return prefix + " timed out"
	case errors.Is(err, context.Canceled):
		return prefix + " canceled"
	default:
		return prefix + " failed: " + err.Error()
	}
}

func (b *Browser) syncViewport() {
	if b.selectedItem().quit {
		b.viewport.SetContent(renderQuitDetails(b.activity))
		return
	}
	b.viewport.SetContent(renderDetails(b.detailProfile, b.details, b.detailErr, b.activity))
}

func renderDetails(profile string, details []core.AdapterDetail, err error, activity []core.ActivityEntry) string {
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
			lines = append(lines, fmt.Sprintf("  %s: %s", palette.Label, renderPaletteValue(palette.Value)))
		}
		for _, sample := range detail.Description.Samples {
			if sample == "" {
				lines = append(lines, "")
				continue
			}
			lines = append(lines, "  "+sample)
		}
		for _, note := range detail.Description.Notes {
			lines = append(lines, "  note: "+note)
		}
		if detail.Error != "" {
			lines = append(lines, "  error: "+detail.Error)
		}
		lines = append(lines, "")
	}
	if len(activity) > 0 {
		lines = append(lines, "Activity")
		lines = append(lines, "")
		start := len(activity) - 12
		if start < 0 {
			start = 0
		}
		for _, entry := range activity[start:] {
			lines = append(lines, fmt.Sprintf("  %s  %s  [%s] %s", entry.Time.Format("15:04:05"), entry.Adapter, entry.Stage, entry.Message))
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func renderQuitDetails(activity []core.ActivityEntry) string {
	lines := []string{
		"Exit Themer",
		"",
		"Press enter on this row to quit.",
		"Press esc, q, or ctrl+c to cancel and quit as well.",
	}
	if len(activity) > 0 {
		lines = append(lines, "", "Activity", "")
		start := len(activity) - 12
		if start < 0 {
			start = 0
		}
		for _, entry := range activity[start:] {
			lines = append(lines, fmt.Sprintf("  %s  %s  [%s] %s", entry.Time.Format("15:04:05"), entry.Adapter, entry.Stage, entry.Message))
		}
	}
	return strings.Join(lines, "\n")
}

func renderPaletteValue(value string) string {
	if !hexColorPattern.MatchString(value) {
		return value
	}
	r, g, b, ok := parseHexColor(value)
	if !ok {
		return value
	}
	return fmt.Sprintf("\x1b[1;38;2;%d;%d;%d;48;2;255;255;255m %s \x1b[0m", r, g, b, value)
}

func activityStatus(entry core.ActivityEntry) string {
	if entry.Adapter == "themer" {
		switch entry.Stage {
		case "preview", "restore", "flush", "commit":
			return entry.Message
		}
	}
	if entry.Stage == "preview-error" {
		return fmt.Sprintf("preview warning: %s: %s", entry.Adapter, entry.Message)
	}
	return ""
}

func parseHexColor(value string) (int, int, int, bool) {
	if !hexColorPattern.MatchString(value) {
		return 0, 0, 0, false
	}
	r, err := strconv.ParseInt(value[1:3], 16, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	g, err := strconv.ParseInt(value[3:5], 16, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	b, err := strconv.ParseInt(value[5:7], 16, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	return int(r), int(g), int(b), true
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
