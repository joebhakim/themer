package adapters

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/joebhakim/themer/internal/config"
	"github.com/joebhakim/themer/internal/core"
	"github.com/joebhakim/themer/internal/testutil"
)

type blockingRunner struct {
	calls   []string
	started chan string
	release chan struct{}
	match   func(name string, args []string) bool
	result  func(name string, args []string) (CommandResult, error)
}

func (r *blockingRunner) Run(ctx context.Context, name string, args ...string) (CommandResult, error) {
	call := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, call)
	if r.match != nil && r.match(name, args) {
		if r.started != nil {
			r.started <- call
		}
		select {
		case <-r.release:
		case <-ctx.Done():
			return CommandResult{}, ctx.Err()
		}
	}
	if r.result != nil {
		return r.result(name, args)
	}
	return CommandResult{}, nil
}

func adapterDebugSnapshot(extra func() string) func() string {
	return func() string {
		return testutil.JoinDebug(
			testutil.LabeledSection("Adapter debug", extra()),
			testutil.LabeledSection("Goroutines", testutil.GoroutineDump()),
		)
	}
}

func formatCalls(calls []string) string {
	if len(calls) == 0 {
		return "(none)"
	}
	return strings.Join(calls, "\n")
}

func formatEntries(entries []core.ActivityEntry) string {
	if len(entries) == 0 {
		return "(none)"
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, fmt.Sprintf("%s [%s] %s", entry.Adapter, entry.Stage, entry.Message))
	}
	return strings.Join(lines, "\n")
}

func TestKittyPreviewBlockedRunnerRespectsContextCancellation(t *testing.T) {
	t.Setenv("KITTY_PID", "4930")
	t.Setenv("KITTY_WINDOW_ID", "46")
	t.Setenv("KITTY_LISTEN_ON", "")

	runner := &blockingRunner{
		started: make(chan string, 1),
		release: make(chan struct{}),
		match: func(name string, args []string) bool {
			return name == "kitty" && len(args) >= 4 && args[0] == "@" && args[1] == "set-colors"
		},
		result: func(name string, args []string) (CommandResult, error) {
			if name == "kitty" && len(args) >= 5 && args[0] == "+kitten" && args[1] == "themes" && args[2] == "--dump-theme" {
				return CommandResult{Stdout: "background #000000\nforeground #ffffff\ncursor #ffffff\n"}, nil
			}
			return CommandResult{}, nil
		},
	}
	adapter := NewKitty(config.KittyConfig{}, runner)
	debug := adapterDebugSnapshot(func() string {
		return testutil.LabeledSection("Calls", formatCalls(runner.calls))
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := testutil.RunAsync(func() error {
		_, err := adapter.Preview(ctx, "Nord")
		return err
	})

	call := testutil.MustReceive(t, "kitty remote preview start", runner.started, 2*time.Second, debug)
	if !strings.Contains(call, "kitty @ set-colors --all") {
		t.Fatalf("blocked call = %q, want kitty remote set-colors", call)
	}
	testutil.MustStayBlocked(t, "kitty preview", done, 150*time.Millisecond, debug)

	cancel()
	err := testutil.MustReceive(t, "kitty preview cancel", done, 2*time.Second, debug)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("preview error = %v, want context.Canceled", err)
	}
}

func TestFishApplyBlockedRunnerRespectsContextCancellation(t *testing.T) {
	runner := &blockingRunner{
		started: make(chan string, 1),
		release: make(chan struct{}),
		match: func(name string, args []string) bool {
			return name == "fish"
		},
	}
	adapter := NewFish(config.FishConfig{
		FrozenThemePath: "/home/example/.config/fish/conf.d/fish_frozen_theme.fish",
	}, runner)
	debug := adapterDebugSnapshot(func() string {
		return testutil.LabeledSection("Calls", formatCalls(runner.calls))
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := testutil.RunAsync(func() error {
		return adapter.Apply(ctx, "nord")
	})

	call := testutil.MustReceive(t, "fish apply start", runner.started, 2*time.Second, debug)
	if !strings.Contains(call, "fish -c") {
		t.Fatalf("blocked call = %q, want fish -c", call)
	}
	testutil.MustStayBlocked(t, "fish apply", done, 150*time.Millisecond, debug)

	cancel()
	err := testutil.MustReceive(t, "fish apply cancel", done, 2*time.Second, debug)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("apply error = %v, want context.Canceled", err)
	}
}

func TestKDEApplyLogsBeforeBlockedRunnerReturns(t *testing.T) {
	runner := &blockingRunner{
		started: make(chan string, 1),
		release: make(chan struct{}),
		match: func(name string, args []string) bool {
			return name == "plasma-apply-colorscheme" && len(args) == 1 && args[0] == "BreezeDark"
		},
		result: func(name string, args []string) (CommandResult, error) {
			if name == "plasma-apply-colorscheme" && len(args) == 1 && args[0] == "--list-schemes" {
				return CommandResult{Stdout: "* BreezeDark (current color scheme)\n"}, nil
			}
			return CommandResult{}, nil
		},
	}
	adapter := NewKDE(runner)
	var entries []core.ActivityEntry
	adapter.SetActivityLogger(func(entry core.ActivityEntry) {
		entries = append(entries, entry)
	})
	debug := adapterDebugSnapshot(func() string {
		return testutil.JoinDebug(
			testutil.LabeledSection("Calls", formatCalls(runner.calls)),
			testutil.LabeledSection("Activity", formatEntries(entries)),
		)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := testutil.RunAsync(func() error {
		return adapter.Apply(ctx, "BreezeDark")
	})

	call := testutil.MustReceive(t, "kde apply start", runner.started, 2*time.Second, debug)
	if !strings.Contains(call, "plasma-apply-colorscheme BreezeDark") {
		t.Fatalf("blocked call = %q, want BreezeDark apply", call)
	}
	testutil.MustStayBlocked(t, "kde apply", done, 150*time.Millisecond, debug)
	if len(entries) == 0 || entries[0].Stage != "apply" || !strings.Contains(entries[0].Message, "requested BreezeDark") {
		t.Fatalf("activity entries = %#v, want requested stage before block", entries)
	}

	cancel()
	err := testutil.MustReceive(t, "kde apply cancel", done, 2*time.Second, debug)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("apply error = %v, want context.Canceled", err)
	}
}
