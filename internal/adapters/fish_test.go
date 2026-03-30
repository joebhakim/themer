package adapters

import (
	"context"
	"strings"
	"testing"

	"github.com/joebhakim/themer/internal/config"
)

type recordingRunner struct {
	name   string
	args   []string
	result CommandResult
	err    error
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) (CommandResult, error) {
	r.name = name
	r.args = append([]string(nil), args...)
	return r.result, r.err
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

func TestFishDescribeIncludesSamplePreview(t *testing.T) {
	runner := &recordingRunner{
		result: CommandResult{
			Stdout: "+-- sample --+\n| command |\n+------------+\n",
		},
	}
	adapter := NewFish(config.FishConfig{}, runner)

	description, err := adapter.Describe(context.Background(), "nord")
	if err != nil {
		t.Fatalf("describe theme: %v", err)
	}
	if runner.name != "fish" {
		t.Fatalf("runner name = %q, want fish", runner.name)
	}
	if len(runner.args) < 2 {
		t.Fatalf("runner args = %#v", runner.args)
	}
	if !strings.Contains(runner.args[1], "fish_config theme choose \"nord\"") {
		t.Fatalf("describe script does not choose theme:\n%s", runner.args[1])
	}
	if len(description.Samples) != 3 {
		t.Fatalf("samples = %#v, want 3 lines", description.Samples)
	}
	if description.Samples[1] != "| command |" {
		t.Fatalf("sample line = %q, want %q", description.Samples[1], "| command |")
	}
}
