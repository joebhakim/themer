package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRejectsWrongVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "themer.toml")
	data := []byte(`
version = 1
enabled_adapters = ["kde"]

[profiles.demo.targets]
kde = "BreezeDark"
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatalf("expected version validation error")
	}
}

func TestLoadReturnsConfigNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.toml")

	_, err := Load(path)
	if err == nil {
		t.Fatalf("expected missing config error")
	}
	var target *ErrConfigNotFound
	if !errors.As(err, &target) {
		t.Fatalf("expected ErrConfigNotFound, got %T", err)
	}
	if target.Path != path {
		t.Fatalf("path = %q, want %q", target.Path, path)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "themer.toml")
	cfg := &Config{
		Version:         CurrentVersion,
		EnabledAdapters: []string{"kde", "cursor"},
		UI: UIConfig{
			PreviewOnMove:   true,
			PreviewDebounce: 90,
			ShowDiagnostics: true,
		},
		Profiles: map[string]Profile{
			"demo": {
				Targets: map[string]string{
					"kde":    "BreezeDark",
					"cursor": "Default Dark+",
				},
			},
		},
	}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.Version != CurrentVersion {
		t.Fatalf("version = %d, want %d", loaded.Version, CurrentVersion)
	}
	if loaded.Profiles["demo"].Targets["cursor"] != "Default Dark+" {
		t.Fatalf("unexpected cursor target: %#v", loaded.Profiles["demo"].Targets)
	}
}

func TestLoadExpandsHomePlaceholders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("user home dir: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "themer.toml")
	data := []byte(`
version = 2
enabled_adapters = ["fish", "neovim", "cursor", "kitty"]

[adapters.kitty]
socket = "unix:~/.cache/kitty.sock"

[adapters.fish]
themes_dir = "/usr/share/fish/themes"
frozen_theme_path = "/home/you/.config/fish/conf.d/fish_frozen_theme.fish"

[adapters.neovim]
config_path = "~/.config/nvim/lua/theme.lua"
colorscheme_pattern = "vim\\.cmd\\.colorscheme\\s+['\\\"]([^'\\\"]+)['\\\"]"

[adapters.cursor]
settings_path = "$HOME/.config/Cursor/User/settings.json"

[profiles.demo.targets]
fish = "nord"
neovim = "tokyonight-storm"
cursor = "Default Dark+"
kitty = "Nord"
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.Adapters.Fish.FrozenThemePath != filepath.Join(home, ".config", "fish", "conf.d", "fish_frozen_theme.fish") {
		t.Fatalf("unexpected fish path: %q", loaded.Adapters.Fish.FrozenThemePath)
	}
	if loaded.Adapters.Neovim.ConfigPath != filepath.Join(home, ".config", "nvim", "lua", "theme.lua") {
		t.Fatalf("unexpected neovim path: %q", loaded.Adapters.Neovim.ConfigPath)
	}
	if loaded.Adapters.Cursor.SettingsPath != filepath.Join(home, ".config", "Cursor", "User", "settings.json") {
		t.Fatalf("unexpected cursor path: %q", loaded.Adapters.Cursor.SettingsPath)
	}
	if loaded.Adapters.Kitty.Socket != "unix:"+filepath.Join(home, ".cache", "kitty.sock") {
		t.Fatalf("unexpected kitty socket: %q", loaded.Adapters.Kitty.Socket)
	}
}
