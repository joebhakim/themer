package adapters

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/joebhakim/themer/internal/config"
)

func TestNeovimApplyRequiresSingleMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "theme.lua")
	cfg := config.NeovimConfig{
		ConfigPath:         path,
		ColorschemePattern: `vim\.cmd\.colorscheme\s+['"]([^'"]+)['"]`,
	}
	adapter := NewNeovim(cfg)

	if err := os.WriteFile(path, []byte("return {}\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := adapter.Apply(context.Background(), "tokyonight-storm"); err == nil {
		t.Fatalf("expected no-match error")
	}

	if err := os.WriteFile(path, []byte("vim.cmd.colorscheme 'one'\nvim.cmd.colorscheme 'two'\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := adapter.Apply(context.Background(), "tokyonight-storm"); err == nil {
		t.Fatalf("expected multiple-match error")
	}
}

func TestNeovimApplyUpdatesTheme(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "theme.lua")
	if err := os.WriteFile(path, []byte("vim.cmd.colorscheme 'tokyonight-night'\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	adapter := NewNeovim(config.NeovimConfig{
		ConfigPath:         path,
		ColorschemePattern: `vim\.cmd\.colorscheme\s+['"]([^'"]+)['"]`,
	})
	if err := adapter.Apply(context.Background(), "tokyonight-day"); err != nil {
		t.Fatalf("apply theme: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != "vim.cmd.colorscheme 'tokyonight-day'\n" {
		t.Fatalf("unexpected config contents: %q", string(data))
	}
}
