package adapters

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joebhakim/themer/internal/config"
)

type kittyRecordingRunner struct {
	calls []string
	run   func(name string, args []string) (CommandResult, error)
}

func (r *kittyRecordingRunner) Run(_ context.Context, name string, args ...string) (CommandResult, error) {
	r.calls = append(r.calls, strings.TrimSpace(name+" "+strings.Join(args, " ")))
	if r.run != nil {
		return r.run(name, args)
	}
	return CommandResult{}, nil
}

func installFakeKittyBinary(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "kitty")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake kitty: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestKittyPreviewStatusDisabledWhenSocketOnlyConfigHasNoLiveSocket(t *testing.T) {
	installFakeKittyBinary(t)
	t.Setenv("KITTY_PID", "4930")
	t.Setenv("KITTY_WINDOW_ID", "46")
	t.Setenv("KITTY_LISTEN_ON", "")

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	kittyDir := filepath.Join(configHome, "kitty")
	if err := os.MkdirAll(kittyDir, 0o755); err != nil {
		t.Fatalf("mkdir kitty config: %v", err)
	}
	configData := "allow_remote_control socket-only\nlisten_on unix:/tmp/kitty-{kitty_pid}.sock\n"
	if err := os.WriteFile(filepath.Join(kittyDir, "kitty.conf"), []byte(configData), 0o644); err != nil {
		t.Fatalf("write kitty.conf: %v", err)
	}

	adapter := NewKitty(config.KittyConfig{}, &kittyRecordingRunner{
		run: func(name string, args []string) (CommandResult, error) {
			if name == "kitty" && len(args) >= 2 && args[0] == "@" && args[1] == "get-colors" {
				return CommandResult{Stderr: "tty remote control disabled", ExitCode: 1}, nil
			}
			return CommandResult{ExitCode: 0}, nil
		},
	})
	status := adapter.PreviewStatus(context.Background())
	if status.Enabled {
		t.Fatalf("expected preview to be disabled, got %#v", status)
	}
	if !strings.Contains(status.Reason, "socket-only remote control") {
		t.Fatalf("status reason = %q, want socket-only explanation", status.Reason)
	}
	if !strings.Contains(status.Reason, "/tmp/kitty-4930.sock") {
		t.Fatalf("status reason = %q, want expanded socket path", status.Reason)
	}
}

func TestKittyPreviewStatusEnabledWhenTTYProbeSucceeds(t *testing.T) {
	installFakeKittyBinary(t)
	t.Setenv("KITTY_PID", "4930")
	t.Setenv("KITTY_WINDOW_ID", "46")
	t.Setenv("KITTY_LISTEN_ON", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	runner := &kittyRecordingRunner{
		run: func(name string, args []string) (CommandResult, error) {
			if name == "kitty" && len(args) >= 4 && args[0] == "@" && args[1] == "get-colors" {
				return CommandResult{Stdout: "background #000000\nforeground #ffffff\n", ExitCode: 0}, nil
			}
			return CommandResult{ExitCode: 0}, nil
		},
	}
	adapter := NewKitty(config.KittyConfig{}, runner)
	status := adapter.PreviewStatus(context.Background())
	if !status.Enabled {
		t.Fatalf("expected preview to be enabled, got %#v", status)
	}
	if !strings.Contains(status.Reason, "current window") {
		t.Fatalf("status reason = %q, want tty probe success reason", status.Reason)
	}
}

func TestKittyPreviewTargetsCurrentWindowAndRestoresCapturedColors(t *testing.T) {
	installFakeKittyBinary(t)
	t.Setenv("KITTY_PID", "4930")
	t.Setenv("KITTY_WINDOW_ID", "46")
	t.Setenv("KITTY_LISTEN_ON", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	runner := &kittyRecordingRunner{
		run: func(name string, args []string) (CommandResult, error) {
			switch {
			case name == "kitty" && len(args) >= 4 && args[0] == "@" && args[1] == "get-colors":
				return CommandResult{Stdout: "background #000000\nforeground #ffffff\ncursor #ffffff\n", ExitCode: 0}, nil
			case name == "kitty" && len(args) >= 5 && args[0] == "+kitten" && args[1] == "themes" && args[2] == "--dump-theme":
				return CommandResult{Stdout: "background #111111\nforeground #eeeeee\ncursor #eeeeee\n", ExitCode: 0}, nil
			case name == "kitty" && len(args) >= 4 && args[0] == "@" && args[1] == "set-colors":
				return CommandResult{ExitCode: 0}, nil
			default:
				return CommandResult{ExitCode: 0}, nil
			}
		},
	}
	adapter := NewKitty(config.KittyConfig{}, runner)

	restore, err := adapter.Preview(context.Background(), "Nord")
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if restore == nil {
		t.Fatalf("restore = nil, want restore function")
	}
	if err := restore(context.Background()); err != nil {
		t.Fatalf("restore preview: %v", err)
	}

	var setColorCalls []string
	for _, call := range runner.calls {
		if strings.Contains(call, "@ set-colors") {
			setColorCalls = append(setColorCalls, call)
			if !strings.Contains(call, "--match id:46") {
				t.Fatalf("set-colors call = %q, want window match", call)
			}
			if strings.Contains(call, "--all") {
				t.Fatalf("set-colors call = %q, did not expect --all", call)
			}
			if strings.Contains(call, "--reset") {
				t.Fatalf("set-colors call = %q, did not expect --reset", call)
			}
		}
	}
	if len(setColorCalls) != 2 {
		t.Fatalf("set-colors calls = %#v, want preview and restore", setColorCalls)
	}
}

func TestKittyPreviewPrefersVerifiedSocketTransport(t *testing.T) {
	installFakeKittyBinary(t)
	t.Setenv("KITTY_PID", "4930")
	t.Setenv("KITTY_WINDOW_ID", "46")
	t.Setenv("KITTY_LISTEN_ON", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	socketDir := t.TempDir()
	socketPath := filepath.Join(socketDir, "kitty.sock")
	file, err := os.Create(socketPath)
	if err != nil {
		t.Fatalf("create fake socket path: %v", err)
	}
	file.Close()

	runner := &kittyRecordingRunner{
		run: func(name string, args []string) (CommandResult, error) {
			switch {
			case name == "kitty" && len(args) >= 6 && args[0] == "@" && args[1] == "--to" && args[2] == "unix:"+socketPath && args[3] == "get-colors":
				return CommandResult{Stdout: "background #000000\nforeground #ffffff\n", ExitCode: 0}, nil
			case name == "kitty" && len(args) >= 7 && args[0] == "@" && args[1] == "--to" && args[2] == "unix:"+socketPath && args[3] == "set-colors":
				return CommandResult{ExitCode: 0}, nil
			case name == "kitty" && len(args) >= 5 && args[0] == "+kitten" && args[1] == "themes" && args[2] == "--dump-theme":
				return CommandResult{Stdout: "background #111111\nforeground #eeeeee\n", ExitCode: 0}, nil
			default:
				return CommandResult{ExitCode: 0}, nil
			}
		},
	}
	adapter := NewKitty(config.KittyConfig{Socket: "unix:" + socketPath}, runner)

	status := adapter.PreviewStatus(context.Background())
	if !status.Enabled {
		t.Fatalf("expected preview to be enabled, got %#v", status)
	}
	if _, err := adapter.Preview(context.Background(), "Nord"); err != nil {
		t.Fatalf("preview: %v", err)
	}

	foundSocketCall := false
	for _, call := range runner.calls {
		if strings.Contains(call, "@ --to unix:"+socketPath+" get-colors") {
			foundSocketCall = true
			break
		}
	}
	if !foundSocketCall {
		t.Fatalf("calls = %#v, want verified socket probe", runner.calls)
	}
}
