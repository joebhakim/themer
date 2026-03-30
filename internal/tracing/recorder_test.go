package tracing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewSessionWritesJSONLTrace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	recorder, err := NewSession(path)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	t.Cleanup(func() {
		if err := recorder.Close(); err != nil {
			t.Fatalf("close recorder: %v", err)
		}
	})

	recorder.Record(Event{
		Time:      time.Unix(1735689600, 0).UTC(),
		Kind:      "command",
		Adapter:   "kitty",
		Operation: "preview",
		Command:   "kitty @ set-colors --all <tempfile>",
		Status:    "ok",
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatalf("unmarshal trace event: %v", err)
	}
	if event.Kind != "command" {
		t.Fatalf("kind = %q, want command", event.Kind)
	}
	if event.Adapter != "kitty" {
		t.Fatalf("adapter = %q, want kitty", event.Adapter)
	}
	if event.Command != "kitty @ set-colors --all <tempfile>" {
		t.Fatalf("command = %q", event.Command)
	}
}
