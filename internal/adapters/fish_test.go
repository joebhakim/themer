package adapters

import (
	"context"
	"strings"
	"testing"

	"github.com/joebhakim/themer/internal/config"
)

type recordingRunner struct {
	name string
	args []string
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) (CommandResult, error) {
	r.name = name
	r.args = append([]string(nil), args...)
	return CommandResult{}, nil
}

func TestFishApplyFailsFastAfterMkdir(t *testing.T) {
	runner := &recordingRunner{}
	adapter := NewFish(config.FishConfig{
		FrozenThemePath: "/home/example/.config/fish/conf.d/fish_frozen_theme.fish",
	}, runner)

	if err := adapter.Apply(context.Background(), "nord"); err != nil {
		t.Fatalf("apply theme: %v", err)
	}
	if runner.name != "fish" {
		t.Fatalf("runner name = %q, want fish", runner.name)
	}
	if len(runner.args) < 2 {
		t.Fatalf("runner args = %#v", runner.args)
	}
	script := runner.args[1]
	if !strings.Contains(script, "mkdir -p (dirname \"/home/example/.config/fish/conf.d/fish_frozen_theme.fish\")\nor exit 1") {
		t.Fatalf("fish script does not fail fast after mkdir:\n%s", script)
	}
}
