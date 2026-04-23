package adapters

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joebhakim/themer/internal/config"
	"github.com/joebhakim/themer/internal/core"
)

type Fish struct {
	cfg    config.FishConfig
	runner CommandRunner
}

const fishRefreshThemePrefix = "# themer fish theme: "

func NewFish(cfg config.FishConfig, runner CommandRunner) *Fish {
	return &Fish{cfg: cfg, runner: runner}
}

func (f *Fish) Name() string {
	return "fish"
}

func (f *Fish) DisplayName() string {
	return "Fish Shell"
}

func (f *Fish) Validate(context.Context) []core.Diagnostic {
	var diagnostics []core.Diagnostic
	if _, err := exec.LookPath("fish"); err != nil {
		return []core.Diagnostic{{
			Adapter: f.Name(),
			Level:   "error",
			Message: "fish is not available",
		}}
	}
	if _, err := os.Stat(f.cfg.ThemesDir); err != nil {
		diagnostics = append(diagnostics, core.Diagnostic{
			Adapter: f.Name(),
			Level:   "warn",
			Message: fmt.Sprintf("theme directory %s is unavailable", f.cfg.ThemesDir),
		})
	}
	if legacyPath := f.legacyFrozenThemePath(); legacyPath != "" {
		if _, err := os.Stat(legacyPath); err == nil {
			diagnostics = append(diagnostics, core.Diagnostic{
				Adapter: f.Name(),
				Level:   "warn",
				Message: fmt.Sprintf("legacy startup theme file %s still exists; themer no longer writes fish startup globals", legacyPath),
			})
		}
	}
	return diagnostics
}

func (f *Fish) ListThemes(ctx context.Context) ([]string, error) {
	result, err := f.runner.Run(ctx, "fish", "-c", "fish_config theme list")
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, errors.New(strings.TrimSpace(result.Stderr))
	}
	var themes []string
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			themes = append(themes, line)
		}
	}
	sort.Strings(themes)
	return themes, nil
}

func (f *Fish) Current(ctx context.Context) (string, error) {
	if f.applyMode() == config.FishApplyModeSessionRefresh {
		theme, ok, err := f.refreshTheme()
		if err != nil {
			return "", err
		}
		if ok {
			return theme, nil
		}
	}
	theme, ok, err := f.currentThemeMarker(ctx)
	if err != nil {
		return "", err
	}
	if ok {
		return theme, nil
	}
	signature, err := f.currentSignature(ctx)
	if err != nil {
		return "", err
	}
	if len(signature) == 0 {
		return "", nil
	}
	entries, err := os.ReadDir(f.cfg.ThemesDir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".theme" {
			continue
		}
		fileSignature, err := parseFishThemeSignature(filepath.Join(f.cfg.ThemesDir, entry.Name()))
		if err != nil {
			continue
		}
		if sameSignature(signature, fileSignature) {
			return strings.TrimSuffix(entry.Name(), ".theme"), nil
		}
	}
	return "", nil
}

func (f *Fish) Describe(ctx context.Context, theme string) (core.ThemeDescription, error) {
	description := core.ThemeDescription{
		Summary: "Fish shell theme applied through fish_config.",
	}
	switch f.applyMode() {
	case config.FishApplyModeUniversal:
		description.Notes = append(description.Notes,
			"Apply persists the theme via universal fish variables.",
			"Running fish shells without session-local overrides should pick up the new theme automatically.",
			"If a shell previously sourced a session-refresh script, run `themer fish-refresh | source` once to clear those session-local overrides.",
		)
	default:
		description.Notes = append(description.Notes,
			"Apply writes a per-session refresh script instead of startup globals.",
			fmt.Sprintf("Refresh the current fish shell with: themer fish-refresh | source (script: %s)", f.refreshPath()),
		)
	}
	description.Notes = append(description.Notes,
		"Live preview is disabled; preview sample below is rendered in an isolated fish process.",
	)
	sample, err := f.sampleThemePreview(ctx, theme)
	if err != nil {
		return description, err
	}
	description.Samples = sample
	return description, nil
}

func (f *Fish) PreviewStatus(context.Context) core.PreviewSupport {
	return core.PreviewSupport{Reason: "fish adapter is apply-only"}
}

func (f *Fish) Preview(context.Context, string) (func(context.Context) error, error) {
	return nil, fmt.Errorf("fish does not support preview")
}

func (f *Fish) Apply(ctx context.Context, theme string) error {
	var err error
	switch f.applyMode() {
	case config.FishApplyModeUniversal:
		err = f.applyUniversal(ctx, theme)
	default:
		err = f.applySessionRefresh(ctx, theme)
	}
	if err != nil {
		return err
	}
	return f.cleanupLegacyFrozenTheme()
}

func (f *Fish) RefreshScript() (string, error) {
	if f.applyMode() == config.FishApplyModeUniversal {
		return buildFishUniversalRefreshScript(), nil
	}
	data, err := os.ReadFile(f.refreshPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("fish refresh script not found at %s; apply a profile first", f.refreshPath())
		}
		return "", err
	}
	return string(data), nil
}

func (f *Fish) applyUniversal(ctx context.Context, theme string) error {
	script := fmt.Sprintf(`
fish_config theme choose %q
or exit 1
for color in (__fish_theme_variables)
    if set -q $color
        set --universal $color $$color
    else
        set --erase --universal $color
    end
end
set --universal themer_current_theme %q
`, theme, theme)
	result, err := f.runner.Run(ctx, "fish", "-c", script)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return errors.New(strings.TrimSpace(result.Stderr))
	}
	return nil
}

func (f *Fish) applySessionRefresh(ctx context.Context, theme string) error {
	dump, err := f.dumpTheme(ctx, theme)
	if err != nil {
		return err
	}
	script, err := buildFishRefreshScript(theme, dump)
	if err != nil {
		return err
	}
	return atomicWriteFile(f.refreshPath(), []byte(script), 0o644)
}

func (f *Fish) dumpTheme(ctx context.Context, theme string) (string, error) {
	script := fmt.Sprintf(`
fish_config theme choose %q
or exit 1
fish_config theme dump
`, theme)
	result, err := f.runner.Run(ctx, "fish", "-c", script)
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", errors.New(strings.TrimSpace(result.Stderr))
	}
	return result.Stdout, nil
}

func (f *Fish) currentSignature(ctx context.Context) (map[string]string, error) {
	script := `
for name in fish_color_normal fish_color_command fish_color_keyword fish_color_param
    if set -q $name
        printf "%s=%s\n" $name $$name[1]
    end
end
`
	result, err := f.runner.Run(ctx, "fish", "-c", script)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, errors.New(strings.TrimSpace(result.Stderr))
	}
	signature := map[string]string{}
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			signature[key] = value
		}
	}
	return signature, nil
}

func (f *Fish) currentThemeMarker(ctx context.Context) (string, bool, error) {
	result, err := f.runner.Run(ctx, "fish", "-c", `
if set -q themer_current_theme
    printf "%s\n" $themer_current_theme
end
`)
	if err != nil {
		return "", false, err
	}
	if result.ExitCode != 0 {
		return "", false, errors.New(strings.TrimSpace(result.Stderr))
	}
	theme := strings.TrimSpace(result.Stdout)
	return theme, theme != "", nil
}

func (f *Fish) refreshTheme() (string, bool, error) {
	data, err := os.ReadFile(f.refreshPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, fishRefreshThemePrefix) {
			theme := strings.TrimSpace(strings.TrimPrefix(line, fishRefreshThemePrefix))
			return theme, theme != "", nil
		}
	}
	return "", false, nil
}

func parseFishThemeSignature(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signature := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "fish_color_") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		signature[parts[0]] = parts[1]
	}
	return signature, nil
}

func sameSignature(a, b map[string]string) bool {
	for _, key := range []string{"fish_color_normal", "fish_color_command", "fish_color_keyword", "fish_color_param"} {
		if a[key] == "" || b[key] == "" {
			continue
		}
		if a[key] != b[key] {
			return false
		}
	}
	return true
}

func (f *Fish) sampleThemePreview(ctx context.Context, theme string) ([]string, error) {
	script := fmt.Sprintf(`
fish_config theme choose %q
or exit 1

function __themer_cell -a varname label
    set_color $$varname || set_color $fish_color_normal || set_color normal
    printf "%%s" $label
    set_color normal
end

echo "+---------------- fish sample ----------------+"
printf "| "
__themer_cell fish_color_command command
printf "  "
__themer_cell fish_color_keyword keyword
printf "  "
__themer_cell fish_color_param param
printf "  "
__themer_cell fish_color_quote quote
printf " |\n"
printf "| "
__themer_cell fish_color_option option
printf "  "
__themer_cell fish_color_operator operator
printf "  "
__themer_cell fish_color_redirection redir
printf "  "
__themer_cell fish_color_end end
printf " |\n"
printf "| "
__themer_cell fish_color_comment comment
printf "  "
__themer_cell fish_color_error error
printf "  "
__themer_cell fish_color_autosuggestion suggest
printf " |\n"
printf "| "
__themer_cell fish_color_match match
printf "  "
__themer_cell fish_color_selection select
printf "  "
__themer_cell fish_color_search_match search
printf " |\n"
echo "+---------------------------------------------+"
`, theme)
	result, err := f.runner.Run(ctx, "fish", "-c", script)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, errors.New(strings.TrimSpace(result.Stderr))
	}
	output := strings.TrimRight(result.Stdout, "\n")
	if output == "" {
		return nil, nil
	}
	return strings.Split(output, "\n"), nil
}

func (f *Fish) applyMode() string {
	if strings.TrimSpace(f.cfg.ApplyMode) == "" {
		return config.FishApplyModeSessionRefresh
	}
	return f.cfg.ApplyMode
}

func (f *Fish) refreshPath() string {
	if path := strings.TrimSpace(f.cfg.RefreshPath); path != "" {
		return path
	}
	return filepath.Join(config.DefaultStateDir(), "fish", "theme.fish")
}

func (f *Fish) legacyFrozenThemePath() string {
	if path := strings.TrimSpace(f.cfg.FrozenThemePath); path != "" {
		return path
	}
	return filepath.Join(xdgConfigHome(), "fish", "conf.d", "fish_frozen_theme.fish")
}

func (f *Fish) cleanupLegacyFrozenTheme() error {
	path := f.legacyFrozenThemePath()
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove legacy fish startup theme file %s: %w", path, err)
	}
	return nil
}

func buildFishRefreshScript(theme, dump string) (string, error) {
	lines := []string{
		"# Generated by themer. Refresh a live fish shell with:",
		"#   themer fish-refresh | source",
		fishRefreshThemePrefix + theme,
		fmt.Sprintf("set --global themer_current_theme %s", quoteFishWord(theme)),
	}
	foundThemeVariable := false
	for _, rawLine := range strings.Split(dump, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "fish_") {
			continue
		}
		foundThemeVariable = true
		args := make([]string, 0, len(fields)-1)
		for _, arg := range fields[1:] {
			args = append(args, quoteFishWord(arg))
		}
		lines = append(lines, "set --global "+fields[0]+" "+strings.Join(args, " "))
	}
	if !foundThemeVariable {
		return "", errors.New("fish theme dump did not return any theme variables")
	}
	lines = append(lines,
		"",
		"if status is-interactive",
		"    commandline -f repaint 2>/dev/null",
		"end",
		"",
	)
	return strings.Join(lines, "\n"), nil
}

func buildFishUniversalRefreshScript() string {
	lines := []string{
		"# Generated by themer. Clear session-local fish theme overrides so universal variables take effect.",
		"for color in (__fish_theme_variables)",
		"    if set -q --global $color",
		"        set --erase --global $color",
		"    end",
		"end",
		"if set -q --global themer_current_theme",
		"    set --erase --global themer_current_theme",
		"end",
		"",
		"if status is-interactive",
		"    commandline -f repaint 2>/dev/null",
		"end",
		"",
	}
	return strings.Join(lines, "\n")
}

func quoteFishWord(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		`"`, `\"`,
		"$", `\$`,
	)
	return `"` + replacer.Replace(value) + `"`
}
