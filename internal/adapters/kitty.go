package adapters

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/joebhakim/themer/internal/config"
	"github.com/joebhakim/themer/internal/core"
)

type Kitty struct {
	cfg              config.KittyConfig
	runner           CommandRunner
	currentThemePath string
}

func NewKitty(cfg config.KittyConfig, runner CommandRunner) *Kitty {
	return &Kitty{
		cfg:              cfg,
		runner:           runner,
		currentThemePath: filepath.Join(xdgConfigHome(), "kitty", "current-theme.conf"),
	}
}

func (k *Kitty) Name() string {
	return "kitty"
}

func (k *Kitty) DisplayName() string {
	return "Kitty"
}

func (k *Kitty) Validate(context.Context) []core.Diagnostic {
	var diagnostics []core.Diagnostic
	if _, err := exec.LookPath("kitty"); err != nil {
		diagnostics = append(diagnostics, core.Diagnostic{
			Adapter: k.Name(),
			Level:   "error",
			Message: "kitty binary is not available",
		})
		return diagnostics
	}
	if support := k.PreviewStatus(context.Background()); !support.Enabled {
		diagnostics = append(diagnostics, core.Diagnostic{
			Adapter: k.Name(),
			Level:   "warn",
			Message: "preview disabled: " + support.Reason,
		})
	}
	return diagnostics
}

func (k *Kitty) ListThemes(context.Context) ([]string, error) {
	themes := append([]string(nil), k.cfg.KnownThemes...)
	return themes, nil
}

func (k *Kitty) Current(ctx context.Context) (string, error) {
	data, err := os.ReadFile(k.currentThemePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	content := string(data)
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "## name:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "## name:")), nil
		}
	}
	currentColors := parseKittyColors(content)
	if len(currentColors) == 0 {
		return "", nil
	}
	for _, theme := range k.cfg.KnownThemes {
		dump, err := k.dumpTheme(ctx, theme)
		if err != nil {
			continue
		}
		colors := parseKittyColors(dump)
		if colors["background"] == currentColors["background"] && colors["foreground"] == currentColors["foreground"] {
			return theme, nil
		}
	}
	return "", nil
}

func (k *Kitty) Describe(ctx context.Context, theme string) (core.ThemeDescription, error) {
	dump, err := k.dumpTheme(ctx, theme)
	if err != nil {
		return core.ThemeDescription{}, err
	}
	colors := parseKittyColors(dump)
	description := core.ThemeDescription{
		Summary: "Kitty theme applied via kitten themes.",
	}
	for _, key := range []string{"background", "foreground", "cursor", "color0", "color1", "color2", "color3", "color4"} {
		if value := colors[key]; value != "" {
			description.Palette = append(description.Palette, core.PaletteEntry{Label: key, Value: value})
		}
	}
	return description, nil
}

func (k *Kitty) PreviewStatus(context.Context) core.PreviewSupport {
	if _, err := exec.LookPath("kitty"); err != nil {
		return core.PreviewSupport{Reason: "kitty not found"}
	}
	socket, ok := k.resolveSocket()
	if !ok {
		if kittySessionAvailable() {
			return core.PreviewSupport{Enabled: true, Reason: "kitty terminal detected"}
		}
		return core.PreviewSupport{Reason: "run inside kitty or configure adapters.kitty.socket"}
	}
	if reason := validateKittySocket(socket); reason != "" {
		return core.PreviewSupport{Reason: reason}
	}
	return core.PreviewSupport{Enabled: true, Reason: "remote control socket available"}
}

func (k *Kitty) Preview(ctx context.Context, theme string) (func(context.Context) error, error) {
	socket, ok := k.resolveSocket()
	if !ok {
		if !kittySessionAvailable() {
			return nil, fmt.Errorf("kitty preview requires either a kitty terminal session or adapters.kitty.socket")
		}
	}
	if reason := validateKittySocket(socket); reason != "" {
		return nil, errors.New(reason)
	}
	dump, err := k.dumpTheme(ctx, theme)
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp("", "themer-kitty-*.conf")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file.Name())
	if _, err := file.WriteString(dump); err != nil {
		file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	if err := k.remote(ctx, socket, "set-colors", "--all", file.Name()); err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		return k.remote(ctx, socket, "set-colors", "--all", "--reset")
	}, nil
}

func (k *Kitty) Apply(ctx context.Context, theme string) error {
	result, err := k.runner.Run(ctx, "kitty", "+kitten", "themes", "--reload-in=all", theme)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return errors.New(strings.TrimSpace(result.Stderr))
	}
	return nil
}

func (k *Kitty) dumpTheme(ctx context.Context, theme string) (string, error) {
	result, err := k.runner.Run(ctx, "kitty", "+kitten", "themes", "--dump-theme", theme)
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", errors.New(strings.TrimSpace(result.Stderr))
	}
	return result.Stdout, nil
}

func (k *Kitty) remote(ctx context.Context, socket string, args ...string) error {
	base := []string{"@"}
	if socket != "" {
		base = append(base, "--to", socket)
	}
	base = append(base, args...)
	result, err := k.runner.Run(ctx, "kitty", base...)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return errors.New(strings.TrimSpace(result.Stderr))
	}
	return nil
}

func (k *Kitty) resolveSocket() (string, bool) {
	if socket := strings.TrimSpace(k.cfg.Socket); socket != "" {
		return socket, true
	}
	if socket := strings.TrimSpace(os.Getenv("KITTY_LISTEN_ON")); socket != "" {
		return socket, true
	}
	return "", false
}

func kittySessionAvailable() bool {
	if strings.TrimSpace(os.Getenv("KITTY_LISTEN_ON")) != "" {
		return true
	}
	return strings.TrimSpace(os.Getenv("KITTY_PID")) != "" || strings.TrimSpace(os.Getenv("KITTY_WINDOW_ID")) != ""
}

func validateKittySocket(socket string) string {
	if strings.HasPrefix(socket, "unix:") {
		path := strings.TrimPrefix(socket, "unix:")
		if _, err := os.Stat(path); err != nil {
			return fmt.Sprintf("kitty socket %s is unavailable", path)
		}
	}
	return ""
}

func parseKittyColors(content string) map[string]string {
	colors := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		key := parts[0]
		if !strings.HasPrefix(key, "color") && key != "background" && key != "foreground" && key != "cursor" && key != "selection_background" && key != "selection_foreground" {
			continue
		}
		colors[key] = parts[1]
	}
	return colors
}
