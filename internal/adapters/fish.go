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
		Notes: []string{
			"Live preview is disabled; apply writes shell theme state.",
			"Preview sample below is rendered in an isolated fish process.",
		},
	}
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
	script := fmt.Sprintf(`
fish_config theme choose %q
or exit 1
mkdir -p (dirname %q)
or exit 1
set -l frozen %q
echo "# Theme set by themer: %s" > $frozen
for color in (__fish_theme_variables)
    if set -q $color
        set -l value $$color
        echo "set --global $color $value" >> $frozen
    end
end
`, theme, f.cfg.FrozenThemePath, f.cfg.FrozenThemePath, theme)
	result, err := f.runner.Run(ctx, "fish", "-c", script)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return errors.New(strings.TrimSpace(result.Stderr))
	}
	return nil
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
