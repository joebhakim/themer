package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/joebhakim/themer/internal/config"
	"github.com/joebhakim/themer/internal/core"
	"github.com/tailscale/hujson"
)

type Cursor struct {
	cfg config.CursorConfig
}

func NewCursor(cfg config.CursorConfig) *Cursor {
	return &Cursor{cfg: cfg}
}

func (c *Cursor) Name() string {
	return "cursor"
}

func (c *Cursor) DisplayName() string {
	return "Cursor"
}

func (c *Cursor) Validate(context.Context) []core.Diagnostic {
	if _, err := os.Stat(c.cfg.SettingsPath); err != nil {
		return []core.Diagnostic{{
			Adapter: c.Name(),
			Level:   "warn",
			Message: fmt.Sprintf("settings path %s is unavailable", c.cfg.SettingsPath),
		}}
	}
	return nil
}

func (c *Cursor) ListThemes(context.Context) ([]string, error) {
	return append([]string(nil), c.cfg.KnownThemes...), nil
}

func (c *Cursor) Current(context.Context) (string, error) {
	settings, err := c.load()
	if err != nil {
		return "", err
	}
	if theme, ok := settings["workbench.colorTheme"].(string); ok && strings.TrimSpace(theme) != "" {
		return theme, nil
	}
	if enabled, ok := settings["window.autoDetectColorScheme"].(bool); ok && enabled {
		if theme, ok := settings["workbench.preferredDarkColorTheme"].(string); ok && theme != "" {
			return theme, nil
		}
		if theme, ok := settings["workbench.preferredLightColorTheme"].(string); ok && theme != "" {
			return theme, nil
		}
	}
	return "", nil
}

func (c *Cursor) Describe(context.Context, string) (core.ThemeDescription, error) {
	return core.ThemeDescription{
		Summary: "Cursor or VS Code theme stored in settings.json (JSONC supported).",
		Notes:   []string{"Apply-only adapter; theme is written back as formatted JSON."},
	}, nil
}

func (c *Cursor) PreviewStatus(context.Context) core.PreviewSupport {
	return core.PreviewSupport{Reason: "cursor adapter is apply-only"}
}

func (c *Cursor) Preview(context.Context, string) (func(context.Context) error, error) {
	return nil, fmt.Errorf("cursor does not support preview")
}

func (c *Cursor) Apply(ctx context.Context, theme string) error {
	settings, err := c.load()
	if err != nil {
		return err
	}
	if enabled, ok := settings["window.autoDetectColorScheme"].(bool); ok && enabled {
		name := strings.ToLower(theme)
		if strings.Contains(name, "light") || strings.Contains(name, "quiet") {
			settings["workbench.preferredLightColorTheme"] = theme
		} else {
			settings["workbench.preferredDarkColorTheme"] = theme
		}
	} else {
		settings["workbench.colorTheme"] = theme
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(c.cfg.SettingsPath, append(data, '\n'), 0o644)
}

func (c *Cursor) load() (map[string]any, error) {
	data, err := os.ReadFile(c.cfg.SettingsPath)
	if err != nil {
		return nil, err
	}
	value, err := hujson.Parse(data)
	if err != nil {
		return nil, err
	}
	value.Standardize()
	standardized := value.Pack()
	settings := map[string]any{}
	if err := json.Unmarshal(standardized, &settings); err != nil {
		return nil, err
	}
	return settings, nil
}
