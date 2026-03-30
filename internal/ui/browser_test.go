package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joebhakim/themer/internal/config"
	"github.com/joebhakim/themer/internal/core"
	"github.com/joebhakim/themer/internal/testutil"
)

func TestRenderPaletteValueStylesHexColors(t *testing.T) {
	rendered := renderPaletteValue("#89B4FA")
	if !strings.Contains(rendered, "#89B4FA") {
		t.Fatalf("rendered value missing hex text: %q", rendered)
	}
	if !strings.Contains(rendered, "\x1b[") {
		t.Fatalf("expected ANSI styling in rendered value: %q", rendered)
	}
}

func TestRenderPaletteValueLeavesPlainTextAlone(t *testing.T) {
	if got := renderPaletteValue("not-a-color"); got != "not-a-color" {
		t.Fatalf("got %q, want plain text unchanged", got)
	}
}

func TestActivityStatusUsesWorkerMessages(t *testing.T) {
	entry := core.ActivityEntry{
		Adapter: "themer",
		Stage:   "preview",
		Message: "calling preview adapter Kitty -> Solarized Dark; waiting for adapter to return",
	}
	if got := activityStatus(entry); got != entry.Message {
		t.Fatalf("status = %q, want %q", got, entry.Message)
	}
}

type browserTestAdapter struct {
	name         string
	display      string
	current      string
	previewState core.PreviewSupport
	applyErr     error
	previewed    []string
	applied      []string
	restored     int
}

type blockingBrowserRestoreAdapter struct {
	browserTestAdapter
	restoreStarted chan string
	restoreRelease chan struct{}
}

type blockingBrowserPreviewAdapter struct {
	browserTestAdapter
	previewStarted chan string
	previewRelease chan struct{}
	previewBlocked bool
}

func (a *browserTestAdapter) Name() string { return a.name }

func (a *browserTestAdapter) DisplayName() string { return a.display }

func (a *browserTestAdapter) Validate(context.Context) []core.Diagnostic { return nil }

func (a *browserTestAdapter) ListThemes(context.Context) ([]string, error) {
	return []string{"demo"}, nil
}

func (a *browserTestAdapter) Current(context.Context) (string, error) {
	return a.current, nil
}

func (a *browserTestAdapter) Describe(context.Context, string) (core.ThemeDescription, error) {
	return core.ThemeDescription{Summary: "test"}, nil
}

func (a *browserTestAdapter) PreviewStatus(context.Context) core.PreviewSupport {
	return a.previewState
}

func (a *browserTestAdapter) Preview(_ context.Context, theme string) (func(context.Context) error, error) {
	a.previewed = append(a.previewed, theme)
	previous := a.current
	a.current = theme
	return func(context.Context) error {
		a.restored++
		a.current = previous
		return nil
	}, nil
}

func (a *browserTestAdapter) Apply(_ context.Context, theme string) error {
	a.applied = append(a.applied, theme)
	if a.applyErr != nil {
		return a.applyErr
	}
	a.current = theme
	return nil
}

func (a *blockingBrowserRestoreAdapter) Preview(_ context.Context, theme string) (func(context.Context) error, error) {
	a.previewed = append(a.previewed, theme)
	previous := a.current
	a.current = theme
	return func(context.Context) error {
		a.restoreStarted <- theme
		<-a.restoreRelease
		a.restored++
		a.current = previous
		return nil
	}, nil
}

func (a *blockingBrowserPreviewAdapter) Preview(_ context.Context, theme string) (func(context.Context) error, error) {
	a.previewed = append(a.previewed, theme)
	if a.previewBlocked {
		a.previewStarted <- theme
		<-a.previewRelease
		a.previewBlocked = false
	}
	previous := a.current
	a.current = theme
	return func(context.Context) error {
		a.restored++
		a.current = previous
		return nil
	}, nil
}

func newTestBrowser(t *testing.T, adapters ...core.Adapter) (Browser, *core.Manager) {
	t.Helper()
	enabled := make([]string, 0, len(adapters))
	targets := make(map[string]string, len(adapters))
	for _, adapter := range adapters {
		enabled = append(enabled, adapter.Name())
		targets[adapter.Name()] = "theme-" + adapter.Name()
	}
	cfg := &config.Config{
		Version:         config.CurrentVersion,
		EnabledAdapters: enabled,
		UI: config.UIConfig{
			PreviewOnMove:   false,
			PreviewDebounce: 10,
		},
		Profiles: map[string]config.Profile{
			"demo": {Targets: targets},
		},
	}
	manager, err := core.NewManager(cfg, adapters)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	return NewBrowser(manager), manager
}

func browserDebugSnapshot(manager *core.Manager) func() string {
	return func() string {
		lines := make([]string, 0, len(manager.ActivityLog()))
		for _, entry := range manager.ActivityLog() {
			lines = append(lines, fmt.Sprintf("%s [%s] %s", entry.Adapter, entry.Stage, entry.Message))
		}
		return testutil.JoinDebug(
			testutil.LabeledSection("Activity log", strings.Join(lines, "\n")),
			testutil.LabeledSection("Goroutines", testutil.GoroutineDump()),
		)
	}
}

func receiveActivityEntry(t *testing.T, manager *core.Manager, wait time.Duration, match func(core.ActivityEntry) bool, debug func() string) core.ActivityEntry {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case entry := <-manager.ActivityStream():
			if match(entry) {
				return entry
			}
		case <-deadline:
			t.Fatalf("timed out waiting for matching activity entry\n\n%s", debug())
			return core.ActivityEntry{}
		}
	}
}

func TestRefreshSelectedQuitRowDoesNotRestorePreview(t *testing.T) {
	kitty := &browserTestAdapter{
		name:         "kitty",
		display:      "Kitty",
		current:      "base",
		previewState: core.PreviewSupport{Enabled: true},
	}
	browser, manager := newTestBrowser(t, kitty)
	if err := manager.PreviewProfile(context.Background(), "demo", 1); err != nil {
		t.Fatalf("preview profile: %v", err)
	}
	browser.list.Select(1)

	if cmd := browser.refreshSelected(); cmd != nil {
		t.Fatalf("quit row should not schedule a restore command")
	}
	if kitty.restored != 0 {
		t.Fatalf("restored = %d, want 0", kitty.restored)
	}
	if !browser.selectedItem().quit {
		t.Fatalf("selected item is not the quit row")
	}
}

func TestApplyFailureFlushesPreviewBeforeApply(t *testing.T) {
	kitty := &browserTestAdapter{
		name:         "kitty",
		display:      "Kitty",
		current:      "base",
		previewState: core.PreviewSupport{Enabled: true},
	}
	fish := &browserTestAdapter{
		name:         "fish",
		display:      "Fish Shell",
		current:      "fish-base",
		previewState: core.PreviewSupport{Reason: "apply-only"},
		applyErr:     errors.New("No such theme: broken"),
	}
	browser, manager := newTestBrowser(t, kitty, fish)
	if err := manager.PreviewProfile(context.Background(), "demo", 1); err != nil {
		t.Fatalf("preview profile: %v", err)
	}

	msg := applyCmd(manager, "demo")()
	applyMsg, ok := msg.(applyDoneMsg)
	if !ok {
		t.Fatalf("apply command returned %T, want applyDoneMsg", msg)
	}
	if applyMsg.err == nil {
		t.Fatalf("apply error = nil, want failure")
	}
	if kitty.restored != 1 {
		t.Fatalf("kitty restored = %d, want 1 after flush-before-apply", kitty.restored)
	}

	updatedModel, cmd := browser.Update(applyMsg)
	updated, ok := updatedModel.(Browser)
	if !ok {
		t.Fatalf("updated model type = %T, want Browser", updatedModel)
	}
	if updated.restoring {
		t.Fatalf("restoring = true, want false")
	}
	if updated.quitting {
		t.Fatalf("quitting = true, want false")
	}
	if !strings.Contains(updated.status, "apply failed:") {
		t.Fatalf("status = %q, want apply failure", updated.status)
	}
	if cmd != nil {
		t.Fatalf("unexpected follow-up command after apply failure")
	}
}

func TestApplySuccessCommitsPreviewAndStaysOpen(t *testing.T) {
	kitty := &browserTestAdapter{
		name:         "kitty",
		display:      "Kitty",
		current:      "base",
		previewState: core.PreviewSupport{Enabled: true},
	}
	browser, manager := newTestBrowser(t, kitty)
	if err := manager.PreviewProfile(context.Background(), "demo", 1); err != nil {
		t.Fatalf("preview profile: %v", err)
	}

	msg := applyCmd(manager, "demo")()
	applyMsg, ok := msg.(applyDoneMsg)
	if !ok {
		t.Fatalf("apply command returned %T, want applyDoneMsg", msg)
	}
	if applyMsg.err != nil {
		t.Fatalf("apply error = %v, want nil", applyMsg.err)
	}

	updatedModel, cmd := browser.Update(applyMsg)
	updated, ok := updatedModel.(Browser)
	if !ok {
		t.Fatalf("updated model type = %T, want Browser", updatedModel)
	}
	if updated.quitting {
		t.Fatalf("quitting = true, want false")
	}
	if updated.restoring {
		t.Fatalf("restoring = true, want false")
	}
	if got := updated.status; got != "applied demo" {
		t.Fatalf("status = %q, want %q", got, "applied demo")
	}
	if cmd == nil {
		t.Fatalf("expected details refresh command")
	}

	if kitty.restored != 1 {
		t.Fatalf("kitty restored = %d, want 1 after flush-before-apply", kitty.restored)
	}
	if err := manager.RestorePreview(context.Background()); err != nil {
		t.Fatalf("restore preview: %v", err)
	}
	if kitty.restored != 1 {
		t.Fatalf("kitty restored = %d, want 1 after commit and later restore", kitty.restored)
	}
}

func TestQuitDuringRestoreForcesExit(t *testing.T) {
	kitty := &browserTestAdapter{
		name:         "kitty",
		display:      "Kitty",
		current:      "base",
		previewState: core.PreviewSupport{Enabled: true},
	}
	browser, _ := newTestBrowser(t, kitty)
	browser.restoring = true

	_, cmd := browser.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatalf("expected quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("command did not force quit during restore")
	}
}

func TestBrowserApplyWaitsOnBlockedFlushAndTracksActivity(t *testing.T) {
	kitty := &blockingBrowserRestoreAdapter{
		browserTestAdapter: browserTestAdapter{
			name:         "kitty",
			display:      "Kitty",
			current:      "base",
			previewState: core.PreviewSupport{Enabled: true},
		},
		restoreStarted: make(chan string, 1),
		restoreRelease: make(chan struct{}),
	}
	browser, manager := newTestBrowser(t, kitty)
	if err := manager.PreviewProfile(context.Background(), "demo", 1); err != nil {
		t.Fatalf("preview profile: %v", err)
	}
	debug := browserDebugSnapshot(manager)

	updatedModel, cmd := browser.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, ok := updatedModel.(Browser)
	if !ok {
		t.Fatalf("updated model type = %T, want Browser", updatedModel)
	}
	if cmd == nil {
		t.Fatalf("expected apply command")
	}
	if !updated.applying {
		t.Fatalf("applying = false, want true")
	}
	if got := updated.status; got != "flushing preview before apply" {
		t.Fatalf("status = %q, want flush message", got)
	}

	msgDone := testutil.RunAsync(func() tea.Msg {
		return cmd()
	})
	started := testutil.MustReceive(t, "restore start", kitty.restoreStarted, 2*time.Second, debug)
	if started != "theme-kitty" {
		t.Fatalf("restore started for %q, want theme-kitty", started)
	}
	testutil.MustStayBlocked(t, "apply command", msgDone, 150*time.Millisecond, debug)

	entry := receiveActivityEntry(t, manager, 2*time.Second, func(entry core.ActivityEntry) bool {
		return entry.Adapter == "themer" && strings.Contains(entry.Message, "calling restore for Kitty")
	}, debug)
	statusModel, _ := updated.Update(activityMsg{entry: entry})
	statusBrowser, ok := statusModel.(Browser)
	if !ok {
		t.Fatalf("status model type = %T, want Browser", statusModel)
	}
	if got := statusBrowser.status; got != entry.Message {
		t.Fatalf("status = %q, want %q", got, entry.Message)
	}

	close(kitty.restoreRelease)
	msg := testutil.MustReceive(t, "apply completion", msgDone, 2*time.Second, debug)
	applyMsg, ok := msg.(applyDoneMsg)
	if !ok {
		t.Fatalf("apply command returned %T, want applyDoneMsg", msg)
	}
	if applyMsg.err != nil {
		t.Fatalf("apply error = %v, want nil", applyMsg.err)
	}
}

func TestBrowserSecondQForceQuitsWhileRestoreCmdIsBlocked(t *testing.T) {
	kitty := &blockingBrowserRestoreAdapter{
		browserTestAdapter: browserTestAdapter{
			name:         "kitty",
			display:      "Kitty",
			current:      "base",
			previewState: core.PreviewSupport{Enabled: true},
		},
		restoreStarted: make(chan string, 1),
		restoreRelease: make(chan struct{}),
	}
	browser, manager := newTestBrowser(t, kitty)
	if err := manager.PreviewProfile(context.Background(), "demo", 1); err != nil {
		t.Fatalf("preview profile: %v", err)
	}
	debug := browserDebugSnapshot(manager)

	updatedModel, restoreCmd := browser.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	updated, ok := updatedModel.(Browser)
	if !ok {
		t.Fatalf("updated model type = %T, want Browser", updatedModel)
	}
	if restoreCmd == nil {
		t.Fatalf("expected restore command")
	}
	restoreDone := testutil.RunAsync(func() tea.Msg {
		return restoreCmd()
	})
	started := testutil.MustReceive(t, "restore start", kitty.restoreStarted, 2*time.Second, debug)
	if started != "theme-kitty" {
		t.Fatalf("restore started for %q, want theme-kitty", started)
	}
	testutil.MustStayBlocked(t, "restore command", restoreDone, 150*time.Millisecond, debug)

	_, quitCmd := updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if quitCmd == nil {
		t.Fatalf("expected quit command")
	}
	if _, ok := quitCmd().(tea.QuitMsg); !ok {
		t.Fatalf("quit command returned %T, want tea.QuitMsg", quitCmd())
	}

	close(kitty.restoreRelease)
	msg := testutil.MustReceive(t, "restore completion", restoreDone, 2*time.Second, debug)
	if _, ok := msg.(restoreDoneMsg); !ok {
		t.Fatalf("restore command returned %T, want restoreDoneMsg", msg)
	}
}

func TestBrowserApplyCmdContinuesWhenPreviewFlushTimesOut(t *testing.T) {
	oldFlushTimeout := applyFlushTimeout
	applyFlushTimeout = 50 * time.Millisecond
	defer func() {
		applyFlushTimeout = oldFlushTimeout
	}()

	kitty := &blockingBrowserPreviewAdapter{
		browserTestAdapter: browserTestAdapter{
			name:         "kitty",
			display:      "Kitty",
			current:      "base",
			previewState: core.PreviewSupport{Enabled: true},
		},
		previewStarted: make(chan string, 1),
		previewRelease: make(chan struct{}),
		previewBlocked: true,
	}
	browser, manager := newTestBrowser(t, kitty)
	debug := browserDebugSnapshot(manager)

	manager.QueuePreview("demo", 1)
	started := testutil.MustReceive(t, "preview start", kitty.previewStarted, 2*time.Second, debug)
	if started != "theme-kitty" {
		t.Fatalf("preview started for %q, want theme-kitty", started)
	}

	updatedModel, cmd := browser.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, ok := updatedModel.(Browser)
	if !ok {
		t.Fatalf("updated model type = %T, want Browser", updatedModel)
	}
	if cmd == nil {
		t.Fatalf("expected apply command")
	}
	if !updated.applying {
		t.Fatalf("applying = false, want true")
	}
	if got := updated.status; got != "flushing preview before apply" {
		t.Fatalf("status = %q, want flush message", got)
	}

	msgDone := testutil.RunAsync(func() tea.Msg {
		return cmd()
	})
	msg := testutil.MustReceive(t, "apply completion despite blocked preview", msgDone, 2*time.Second, debug)
	applyMsg, ok := msg.(applyDoneMsg)
	if !ok {
		t.Fatalf("apply command returned %T, want applyDoneMsg", msg)
	}
	if applyMsg.err != nil {
		t.Fatalf("apply error = %v, want nil", applyMsg.err)
	}
	if applyMsg.warning == "" {
		t.Fatalf("apply warning = empty, want preview cleanup warning")
	}
	if len(kitty.applied) != 1 || kitty.applied[0] != "theme-kitty" {
		t.Fatalf("applied = %#v, want [theme-kitty]", kitty.applied)
	}

	foundBlockedPreview := false
	foundFlushWait := false
	for _, entry := range manager.ActivityLog() {
		if strings.Contains(entry.Message, "calling preview adapter Kitty -> theme-kitty; waiting for adapter to return") {
			foundBlockedPreview = true
		}
		if strings.Contains(entry.Message, "flush requested; waiting for preview worker") {
			foundFlushWait = true
		}
	}
	if !foundBlockedPreview {
		t.Fatalf("activity log missing blocked preview call\n\n%s", debug())
	}
	if !foundFlushWait {
		t.Fatalf("activity log missing blocked flush request\n\n%s", debug())
	}

	updatedModel, followup := updated.Update(applyMsg)
	updated, ok = updatedModel.(Browser)
	if !ok {
		t.Fatalf("updated model type = %T, want Browser", updatedModel)
	}
	if !strings.Contains(updated.status, "applied demo") {
		t.Fatalf("status = %q, want applied demo", updated.status)
	}
	if !strings.Contains(updated.status, "preview cleanup timed out") {
		t.Fatalf("status = %q, want preview cleanup warning", updated.status)
	}
	if followup == nil {
		t.Fatalf("expected details refresh command")
	}

	close(kitty.previewRelease)
	receiveActivityEntry(t, manager, 2*time.Second, func(entry core.ActivityEntry) bool {
		return entry.Adapter == "themer" && strings.Contains(entry.Message, "preview settled on demo")
	}, debug)
}
