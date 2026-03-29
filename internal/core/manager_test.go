package core

import (
	"context"
	"testing"

	"github.com/joebhakim/themer/internal/config"
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
