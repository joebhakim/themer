package adapters

import (
	"context"
	"fmt"
	"os"
	"regexp"

	"github.com/joebhakim/themer/internal/config"
	"github.com/joebhakim/themer/internal/core"
)

type Neovim struct {
	cfg config.NeovimConfig
}

func NewNeovim(cfg config.NeovimConfig) *Neovim {
	return &Neovim{cfg: cfg}
}

func (n *Neovim) Name() string {
	return "neovim"
}

func (n *Neovim) DisplayName() string {
	return "Neovim"
}

func (n *Neovim) Validate(context.Context) []core.Diagnostic {
	if _, err := os.Stat(n.cfg.ConfigPath); err != nil {
		return []core.Diagnostic{{
			Adapter: n.Name(),
			Level:   "warn",
			Message: fmt.Sprintf("config path %s is unavailable", n.cfg.ConfigPath),
		}}
	}
	return nil
}

func (n *Neovim) ListThemes(context.Context) ([]string, error) {
	return append([]string(nil), n.cfg.KnownThemes...), nil
}

func (n *Neovim) Current(context.Context) (string, error) {
	data, err := os.ReadFile(n.cfg.ConfigPath)
	if err != nil {
		return "", err
	}
	re, err := regexp.Compile(n.cfg.ColorschemePattern)
	if err != nil {
		return "", err
	}
	match := re.FindStringSubmatch(string(data))
	if len(match) < 2 {
		return "", nil
	}
	return match[1], nil
}

func (n *Neovim) Describe(context.Context, string) (core.ThemeDescription, error) {
	return core.ThemeDescription{
		Summary: "Neovim colorscheme updated by editing the configured Lua file.",
		Notes:   []string{"Apply-only adapter; restart or reload Neovim config after changes."},
	}, nil
}

func (n *Neovim) PreviewStatus(context.Context) core.PreviewSupport {
	return core.PreviewSupport{Reason: "neovim adapter is apply-only"}
}

func (n *Neovim) Preview(context.Context, string) (func(context.Context) error, error) {
	return nil, fmt.Errorf("neovim does not support preview")
}

func (n *Neovim) ApplyWithTheme(theme string) error {
	data, err := os.ReadFile(n.cfg.ConfigPath)
	if err != nil {
		return err
	}
	re, err := regexp.Compile(n.cfg.ColorschemePattern)
	if err != nil {
		return err
	}
	matches := re.FindAllStringSubmatchIndex(string(data), -1)
	if len(matches) != 1 {
		return fmt.Errorf("colorscheme pattern must match exactly once, found %d matches", len(matches))
	}
	updated := re.ReplaceAllString(string(data), fmt.Sprintf("vim.cmd.colorscheme '%s'", theme))
	if updated == string(data) {
		return fmt.Errorf("colorscheme update made no changes")
	}
	return atomicWriteFile(n.cfg.ConfigPath, []byte(updated), 0o644)
}

func (n *Neovim) Apply(ctx context.Context, theme string) error {
	return n.ApplyWithTheme(theme)
}
