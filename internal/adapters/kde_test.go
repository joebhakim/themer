package adapters

import (
	"context"
	"strings"
	"testing"

	"github.com/joebhakim/themer/internal/core"
)

type kdeRunner struct {
	calls []string
}

func (r *kdeRunner) Run(_ context.Context, name string, args ...string) (CommandResult, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	switch {
	case len(args) == 1 && args[0] == "BreezeDark":
		return CommandResult{}, nil
	case len(args) == 1 && args[0] == "--list-schemes":
		return CommandResult{Stdout: "* BreezeDark (current color scheme)\n  BreezeLight\n"}, nil
	default:
		return CommandResult{}, nil
	}
}

func TestKDEApplyLogsPropagationStages(t *testing.T) {
	runner := &kdeRunner{}
	adapter := NewKDE(runner)
	var entries []core.ActivityEntry
	adapter.SetActivityLogger(func(entry core.ActivityEntry) {
		entries = append(entries, entry)
	})

	if err := adapter.Apply(context.Background(), "BreezeDark"); err != nil {
		t.Fatalf("apply theme: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("entries = %#v, want 4 activity stages", entries)
	}
	if entries[0].Stage != "apply" || !strings.Contains(entries[0].Message, "requested BreezeDark") {
		t.Fatalf("first entry = %#v", entries[0])
	}
	if entries[2].Stage != "propagation" {
		t.Fatalf("third entry = %#v, want propagation", entries[2])
	}
	if entries[3].Stage != "ready" || !strings.Contains(entries[3].Message, "BreezeDark") {
		t.Fatalf("final entry = %#v", entries[3])
	}
	if len(runner.calls) < 2 {
		t.Fatalf("runner calls = %#v", runner.calls)
	}
}
