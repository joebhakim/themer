package adapters

import (
	"context"
	"testing"

	"github.com/joebhakim/themer/internal/config"
)

func TestKittyPreviewStatusEnabledInsideKittyWithoutSocket(t *testing.T) {
	t.Setenv("KITTY_PID", "4930")
	t.Setenv("KITTY_WINDOW_ID", "46")
	t.Setenv("KITTY_LISTEN_ON", "")

	adapter := NewKitty(config.KittyConfig{}, ExecRunner{})
	status := adapter.PreviewStatus(context.Background())
	if !status.Enabled {
		t.Fatalf("expected preview to be enabled, got %#v", status)
	}
}

func TestKittyPreviewStatusDisabledOutsideKittyWithoutSocket(t *testing.T) {
	t.Setenv("KITTY_PID", "")
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("KITTY_LISTEN_ON", "")

	adapter := NewKitty(config.KittyConfig{}, ExecRunner{})
	status := adapter.PreviewStatus(context.Background())
	if status.Enabled {
		t.Fatalf("expected preview to be disabled, got %#v", status)
	}
}
