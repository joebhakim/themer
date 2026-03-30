package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/joebhakim/themer/internal/config"
	"github.com/joebhakim/themer/internal/testutil"
)

type fakeAdapter struct {
	name         string
	display      string
	current      string
	previewed    []string
	restored     int
	applied      []string
	previewState PreviewSupport
}

func (f *fakeAdapter) Name() string                          { return f.name }
func (f *fakeAdapter) DisplayName() string                   { return f.display }
func (f *fakeAdapter) Validate(context.Context) []Diagnostic { return nil }
func (f *fakeAdapter) ListThemes(context.Context) ([]string, error) {
	return []string{"one", "two"}, nil
}
func (f *fakeAdapter) Current(context.Context) (string, error) { return f.current, nil }
func (f *fakeAdapter) Describe(context.Context, string) (ThemeDescription, error) {
	return ThemeDescription{Summary: "fake"}, nil
}
func (f *fakeAdapter) PreviewStatus(context.Context) PreviewSupport { return f.previewState }
func (f *fakeAdapter) Preview(_ context.Context, theme string) (func(context.Context) error, error) {
	f.previewed = append(f.previewed, theme)
	previous := f.current
	f.current = theme
	return func(context.Context) error {
		f.restored++
		f.current = previous
		return nil
	}, nil
}
func (f *fakeAdapter) Apply(_ context.Context, theme string) error {
	f.applied = append(f.applied, theme)
	f.current = theme
	return nil
}

type blockingPreviewAdapter struct {
	fakeAdapter
	started chan string
	release chan struct{}
	blocked bool
}

func (b *blockingPreviewAdapter) Preview(ctx context.Context, theme string) (func(context.Context) error, error) {
	b.previewed = append(b.previewed, theme)
	if b.blocked {
		b.started <- theme
		select {
		case <-b.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		b.blocked = false
	}
	previous := b.current
	b.current = theme
	return func(context.Context) error {
		b.restored++
		b.current = previous
		return nil
	}, nil
}

type blockingRestoreAdapter struct {
	fakeAdapter
	restoreStarted chan string
	restoreRelease chan struct{}
}

func (b *blockingRestoreAdapter) Preview(_ context.Context, theme string) (func(context.Context) error, error) {
	b.previewed = append(b.previewed, theme)
	previous := b.current
	b.current = theme
	return func(ctx context.Context) error {
		b.restoreStarted <- theme
		select {
		case <-b.restoreRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
		b.restored++
		b.current = previous
		return nil
	}, nil
}

type blockingApplyAdapter struct {
	fakeAdapter
	started chan string
	release chan struct{}
	blocked bool
}

func (b *blockingApplyAdapter) Apply(ctx context.Context, theme string) error {
	b.applied = append(b.applied, theme)
	if b.blocked {
		b.started <- theme
		select {
		case <-b.release:
		case <-ctx.Done():
			return ctx.Err()
		}
		b.blocked = false
	}
	b.current = theme
	return nil
}

func managerDebugSnapshot(manager *Manager) func() string {
	return func() string {
		return testutil.JoinDebug(
			testutil.LabeledSection("Activity log", formatActivityEntries(manager.ActivityLog())),
			testutil.LabeledSection("Goroutines", testutil.GoroutineDump()),
		)
	}
}

func formatActivityEntries(entries []ActivityEntry) string {
	if len(entries) == 0 {
		return "(empty)"
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, fmt.Sprintf("%s [%s] %s", entry.Adapter, entry.Stage, entry.Message))
	}
	return strings.Join(lines, "\n")
}

func activityLogContains(entries []ActivityEntry, needle string) bool {
	for _, entry := range entries {
		if strings.Contains(entry.Message, needle) {
			return true
		}
	}
	return false
}

func TestManagerPreviewRestoresPreviousSelection(t *testing.T) {
	cfg := &config.Config{
		Version:         config.CurrentVersion,
		EnabledAdapters: []string{"kde"},
		Profiles: map[string]config.Profile{
			"one": {Targets: map[string]string{"kde": "one"}},
			"two": {Targets: map[string]string{"kde": "two"}},
		},
		UI: config.UIConfig{PreviewDebounce: 10},
	}
	adapter := &fakeAdapter{
		name:         "kde",
		display:      "KDE",
		current:      "base",
		previewState: PreviewSupport{Enabled: true},
	}
	manager, err := NewManager(cfg, []Adapter{adapter})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if err := manager.PreviewProfile(context.Background(), "one", 1); err != nil {
		t.Fatalf("preview one: %v", err)
	}
	if adapter.current != "one" {
		t.Fatalf("current = %q, want one", adapter.current)
	}

	if err := manager.PreviewProfile(context.Background(), "two", 2); err != nil {
		t.Fatalf("preview two: %v", err)
	}
	if adapter.restored != 1 {
		t.Fatalf("restored = %d, want 1", adapter.restored)
	}
	if adapter.current != "two" {
		t.Fatalf("current = %q, want two", adapter.current)
	}

	if err := manager.RestorePreview(context.Background()); err != nil {
		t.Fatalf("restore preview: %v", err)
	}
	if adapter.current != "base" {
		t.Fatalf("current = %q, want base", adapter.current)
	}
}

func TestManagerCommitPreviewDropsRestoreState(t *testing.T) {
	cfg := &config.Config{
		Version:         config.CurrentVersion,
		EnabledAdapters: []string{"kde"},
		Profiles: map[string]config.Profile{
			"one": {Targets: map[string]string{"kde": "one"}},
		},
		UI: config.UIConfig{PreviewDebounce: 10},
	}
	adapter := &fakeAdapter{
		name:         "kde",
		display:      "KDE",
		current:      "base",
		previewState: PreviewSupport{Enabled: true},
	}
	manager, err := NewManager(cfg, []Adapter{adapter})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if err := manager.PreviewProfile(context.Background(), "one", 1); err != nil {
		t.Fatalf("preview one: %v", err)
	}
	manager.CommitPreview()
	if err := manager.RestorePreview(context.Background()); err != nil {
		t.Fatalf("restore preview: %v", err)
	}
	if adapter.restored != 0 {
		t.Fatalf("restored = %d, want 0", adapter.restored)
	}
	if adapter.current != "one" {
		t.Fatalf("current = %q, want one", adapter.current)
	}
}

func TestManagerQueuePreviewDropsIntermediateSelections(t *testing.T) {
	cfg := &config.Config{
		Version:         config.CurrentVersion,
		EnabledAdapters: []string{"kitty"},
		Profiles: map[string]config.Profile{
			"one":   {Targets: map[string]string{"kitty": "one"}},
			"two":   {Targets: map[string]string{"kitty": "two"}},
			"three": {Targets: map[string]string{"kitty": "three"}},
		},
		UI: config.UIConfig{PreviewDebounce: 10},
	}
	adapter := &blockingPreviewAdapter{
		fakeAdapter: fakeAdapter{
			name:         "kitty",
			display:      "Kitty",
			current:      "base",
			previewState: PreviewSupport{Enabled: true},
		},
		started: make(chan string, 1),
		release: make(chan struct{}),
		blocked: true,
	}
	manager, err := NewManager(cfg, []Adapter{adapter})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	manager.QueuePreview("one", 1)
	select {
	case started := <-adapter.started:
		if started != "one" {
			t.Fatalf("started preview = %q, want one", started)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first preview to start")
	}

	manager.QueuePreview("two", 2)
	manager.QueuePreview("three", 3)
	close(adapter.release)

	if err := manager.waitPreviewIdle(context.Background()); err != nil {
		t.Fatalf("wait preview idle: %v", err)
	}
	if got := len(adapter.previewed); got != 2 {
		t.Fatalf("preview count = %d, want 2 (%v)", got, adapter.previewed)
	}
	if adapter.previewed[0] != "one" || adapter.previewed[1] != "three" {
		t.Fatalf("preview order = %v, want [one three]", adapter.previewed)
	}
	if adapter.current != "three" {
		t.Fatalf("current = %q, want three", adapter.current)
	}
	if adapter.restored != 1 {
		t.Fatalf("restored = %d, want 1", adapter.restored)
	}
}

func TestManagerCancelPendingPreviewDropsQueuedSelection(t *testing.T) {
	cfg := &config.Config{
		Version:         config.CurrentVersion,
		EnabledAdapters: []string{"kitty"},
		Profiles: map[string]config.Profile{
			"one": {Targets: map[string]string{"kitty": "one"}},
			"two": {Targets: map[string]string{"kitty": "two"}},
		},
		UI: config.UIConfig{PreviewDebounce: 10},
	}
	adapter := &blockingPreviewAdapter{
		fakeAdapter: fakeAdapter{
			name:         "kitty",
			display:      "Kitty",
			current:      "base",
			previewState: PreviewSupport{Enabled: true},
		},
		started: make(chan string, 1),
		release: make(chan struct{}),
		blocked: true,
	}
	manager, err := NewManager(cfg, []Adapter{adapter})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	manager.QueuePreview("one", 1)
	select {
	case <-adapter.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first preview to start")
	}

	manager.QueuePreview("two", 2)
	manager.CancelPendingPreview()
	close(adapter.release)

	if err := manager.waitPreviewIdle(context.Background()); err != nil {
		t.Fatalf("wait preview idle: %v", err)
	}
	if got := len(adapter.previewed); got != 1 {
		t.Fatalf("preview count = %d, want 1 (%v)", got, adapter.previewed)
	}
	if adapter.previewed[0] != "one" {
		t.Fatalf("preview order = %v, want [one]", adapter.previewed)
	}
	if adapter.current != "one" {
		t.Fatalf("current = %q, want one", adapter.current)
	}
	if adapter.restored != 0 {
		t.Fatalf("restored = %d, want 0", adapter.restored)
	}
}

func TestManagerQueuePreviewLogsBusyWorkerState(t *testing.T) {
	cfg := &config.Config{
		Version:         config.CurrentVersion,
		EnabledAdapters: []string{"kitty"},
		Profiles: map[string]config.Profile{
			"one": {Targets: map[string]string{"kitty": "one"}},
			"two": {Targets: map[string]string{"kitty": "two"}},
		},
		UI: config.UIConfig{PreviewDebounce: 10},
	}
	adapter := &blockingPreviewAdapter{
		fakeAdapter: fakeAdapter{
			name:         "kitty",
			display:      "Kitty",
			current:      "base",
			previewState: PreviewSupport{Enabled: true},
		},
		started: make(chan string, 1),
		release: make(chan struct{}),
		blocked: true,
	}
	manager, err := NewManager(cfg, []Adapter{adapter})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	manager.QueuePreview("one", 1)
	select {
	case <-adapter.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first preview to start")
	}

	manager.QueuePreview("two", 2)
	found := false
	for _, entry := range manager.ActivityLog() {
		if strings.Contains(entry.Message, "queued preview two (seq 2); worker busy") {
			found = true
			break
		}
	}
	close(adapter.release)
	if err := manager.waitPreviewIdle(context.Background()); err != nil {
		t.Fatalf("wait preview idle: %v", err)
	}
	if !found {
		t.Fatalf("expected busy worker queue log, got %#v", manager.ActivityLog())
	}
}

func TestManagerFlushPreviewBlocksOnRestoreAndLogsPhase(t *testing.T) {
	cfg := &config.Config{
		Version:         config.CurrentVersion,
		EnabledAdapters: []string{"kitty"},
		Profiles: map[string]config.Profile{
			"one": {Targets: map[string]string{"kitty": "one"}},
		},
		UI: config.UIConfig{PreviewDebounce: 10},
	}
	adapter := &blockingRestoreAdapter{
		fakeAdapter: fakeAdapter{
			name:         "kitty",
			display:      "Kitty",
			current:      "base",
			previewState: PreviewSupport{Enabled: true},
		},
		restoreStarted: make(chan string, 1),
		restoreRelease: make(chan struct{}),
	}
	manager, err := NewManager(cfg, []Adapter{adapter})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := manager.PreviewProfile(context.Background(), "one", 1); err != nil {
		t.Fatalf("preview one: %v", err)
	}

	debug := managerDebugSnapshot(manager)
	flushDone := testutil.RunAsync(func() error {
		return manager.FlushPreview(context.Background())
	})
	started := testutil.MustReceive(t, "restore start", adapter.restoreStarted, 2*time.Second, debug)
	if started != "one" {
		t.Fatalf("restore started for %q, want one", started)
	}
	testutil.MustStayBlocked(t, "flush preview", flushDone, 150*time.Millisecond, debug)
	if !activityLogContains(manager.ActivityLog(), "calling restore for Kitty") {
		t.Fatalf("activity log missing restore call\n\n%s", debug())
	}

	close(adapter.restoreRelease)
	if err := testutil.MustReceive(t, "flush completion", flushDone, 2*time.Second, debug); err != nil {
		t.Fatalf("flush preview: %v", err)
	}
	if adapter.current != "base" {
		t.Fatalf("current = %q, want base", adapter.current)
	}
	if adapter.restored != 1 {
		t.Fatalf("restored = %d, want 1", adapter.restored)
	}
}

func TestManagerFlushPreviewCannotPreemptBlockedPreviewCall(t *testing.T) {
	cfg := &config.Config{
		Version:         config.CurrentVersion,
		EnabledAdapters: []string{"kitty"},
		Profiles: map[string]config.Profile{
			"one": {Targets: map[string]string{"kitty": "one"}},
		},
		UI: config.UIConfig{PreviewDebounce: 10},
	}
	adapter := &blockingPreviewAdapter{
		fakeAdapter: fakeAdapter{
			name:         "kitty",
			display:      "Kitty",
			current:      "base",
			previewState: PreviewSupport{Enabled: true},
		},
		started: make(chan string, 1),
		release: make(chan struct{}),
		blocked: true,
	}
	manager, err := NewManager(cfg, []Adapter{adapter})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	manager.QueuePreview("one", 1)
	select {
	case started := <-adapter.started:
		if started != "one" {
			t.Fatalf("started preview = %q, want one", started)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first preview to start")
	}

	debug := managerDebugSnapshot(manager)
	flushCtx, cancelFlush := context.WithCancel(context.Background())
	flushDone := testutil.RunAsync(func() error {
		return manager.FlushPreview(flushCtx)
	})
	testutil.MustStayBlocked(t, "flush preview behind blocked preview call", flushDone, 150*time.Millisecond, debug)

	runtime := manager.snapshotWorkerRuntime()
	if !runtime.busy || runtime.stage != "preview-call" || runtime.profile != "one" || runtime.adapter != "Kitty" {
		t.Fatalf("worker runtime = %#v, want busy preview-call for Kitty/one", runtime)
	}
	if !activityLogContains(manager.ActivityLog(), "calling preview adapter Kitty -> one; waiting for adapter to return") {
		t.Fatalf("activity log missing blocked preview call\n\n%s", debug())
	}
	if !activityLogContains(manager.ActivityLog(), "flush requested; waiting for preview worker") {
		t.Fatalf("activity log missing blocked flush request\n\n%s", debug())
	}
	if activityLogContains(manager.ActivityLog(), "flush-before-apply start") {
		t.Fatalf("flush unexpectedly reached worker while preview call was blocked\n\n%s", debug())
	}

	cancelFlush()
	if err := testutil.MustReceive(t, "flush cancellation", flushDone, 2*time.Second, debug); !errors.Is(err, context.Canceled) {
		t.Fatalf("flush error = %v, want context.Canceled", err)
	}

	close(adapter.release)
	if err := manager.waitPreviewIdle(context.Background()); err != nil {
		t.Fatalf("wait preview idle: %v", err)
	}
}

func TestManagerApplyProfileTimesOutBrokenAdapterAndContinues(t *testing.T) {
	oldTimeout := applyOperationTimeout
	applyOperationTimeout = 50 * time.Millisecond
	defer func() {
		applyOperationTimeout = oldTimeout
	}()

	cfg := &config.Config{
		Version:         config.CurrentVersion,
		EnabledAdapters: []string{"kitty", "fish"},
		Profiles: map[string]config.Profile{
			"demo": {Targets: map[string]string{"kitty": "nord", "fish": "mocha"}},
		},
		UI: config.UIConfig{PreviewDebounce: 10},
	}
	kitty := &blockingApplyAdapter{
		fakeAdapter: fakeAdapter{
			name:    "kitty",
			display: "Kitty",
			current: "base",
		},
		started: make(chan string, 1),
		release: make(chan struct{}),
		blocked: true,
	}
	fish := &fakeAdapter{
		name:    "fish",
		display: "Fish Shell",
		current: "base",
	}
	manager, err := NewManager(cfg, []Adapter{kitty, fish})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	debug := managerDebugSnapshot(manager)
	done := testutil.RunAsync(func() error {
		_, err := manager.ApplyProfile(context.Background(), "demo")
		return err
	})

	started := testutil.MustReceive(t, "kitty apply start", kitty.started, 2*time.Second, debug)
	if started != "nord" {
		t.Fatalf("kitty apply started for %q, want nord", started)
	}
	err = testutil.MustReceive(t, "apply completion", done, 2*time.Second, debug)
	if err == nil {
		t.Fatalf("apply error = nil, want timeout aggregate")
	}
	if !strings.Contains(err.Error(), "Kitty") || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("apply error = %v, want kitty timeout", err)
	}
	if len(fish.applied) != 1 || fish.applied[0] != "mocha" {
		t.Fatalf("fish applied = %#v, want [mocha]", fish.applied)
	}
}
