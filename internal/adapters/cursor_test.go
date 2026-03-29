package adapters

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/joebhakim/themer/internal/config"
)

func TestCursorSupportsJSONC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	data := []byte(`{
  // comment
  "workbench.colorTheme": "Default Dark+",
  "editor.tabSize": 2,
}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	adapter := NewCursor(config.CursorConfig{SettingsPath: path})
	current, err := adapter.Current(context.Background())
	if err != nil {
		t.Fatalf("current theme: %v", err)
	}
	if current != "Default Dark+" {
		t.Fatalf("current = %q, want Default Dark+", current)
	}

	if err := adapter.Apply(context.Background(), "Quiet Light"); err != nil {
		t.Fatalf("apply theme: %v", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(written, &parsed); err != nil {
		t.Fatalf("parse rewritten settings: %v", err)
	}
	if parsed["workbench.colorTheme"] != "Quiet Light" {
		t.Fatalf("theme = %#v, want Quiet Light", parsed["workbench.colorTheme"])
	}
	if parsed["editor.tabSize"] != float64(2) {
		t.Fatalf("editor.tabSize = %#v, want 2", parsed["editor.tabSize"])
	}
}
