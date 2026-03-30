package adapters

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/joebhakim/themer/internal/core"
)

type KDE struct {
	runner CommandRunner
	log    core.ActivityLogger
}

func NewKDE(runner CommandRunner) *KDE {
	return &KDE{runner: runner}
}

func (k *KDE) Name() string {
	return "kde"
}

func (k *KDE) DisplayName() string {
	return "KDE Plasma"
}

func (k *KDE) SetActivityLogger(logger core.ActivityLogger) {
	k.log = logger
}

func (k *KDE) Validate(context.Context) []core.Diagnostic {
	if _, err := exec.LookPath("plasma-apply-colorscheme"); err != nil {
		return []core.Diagnostic{{
			Adapter: k.Name(),
			Level:   "error",
			Message: "plasma-apply-colorscheme is not available",
		}}
	}
	return nil
}

func (k *KDE) ListThemes(ctx context.Context) ([]string, error) {
	result, err := k.runner.Run(ctx, "plasma-apply-colorscheme", "--list-schemes")
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, errors.New(strings.TrimSpace(result.Stderr))
	}
	seen := map[string]struct{}{}
	var themes []string
	for _, line := range strings.Split(result.Stdout, "\n") {
		name := parseKDEThemeLine(line)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		themes = append(themes, name)
	}
	return themes, nil
}

func (k *KDE) Current(ctx context.Context) (string, error) {
	result, err := k.runner.Run(ctx, "plasma-apply-colorscheme", "--list-schemes")
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", errors.New(strings.TrimSpace(result.Stderr))
	}
	for _, line := range strings.Split(result.Stdout, "\n") {
		if strings.Contains(line, "(current color scheme)") {
			return parseKDEThemeLine(line), nil
		}
	}
	return "", nil
}

func (k *KDE) Describe(context.Context, string) (core.ThemeDescription, error) {
	return core.ThemeDescription{
		Summary: "Native KDE Plasma color scheme.",
	}, nil
}

func (k *KDE) PreviewStatus(context.Context) core.PreviewSupport {
	if _, err := exec.LookPath("plasma-apply-colorscheme"); err != nil {
		return core.PreviewSupport{Reason: "plasma-apply-colorscheme not found"}
	}
	return core.PreviewSupport{Reason: "KDE preview is disabled; Plasma applies propagate live"}
}

func (k *KDE) Preview(ctx context.Context, theme string) (func(context.Context) error, error) {
	current, err := k.Current(ctx)
	if err != nil {
		return nil, err
	}
	if current == "" {
		return nil, fmt.Errorf("current KDE theme could not be determined")
	}
	if err := k.Apply(ctx, theme); err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		return k.Apply(ctx, current)
	}, nil
}

func (k *KDE) Apply(ctx context.Context, theme string) error {
	k.activity("apply", fmt.Sprintf("requested %s", theme))
	result, err := k.runner.Run(ctx, "plasma-apply-colorscheme", theme)
	if err != nil {
		k.activity("error", err.Error())
		return err
	}
	if result.ExitCode != 0 {
		err := errors.New(strings.TrimSpace(result.Stderr))
		k.activity("error", err.Error())
		return err
	}
	k.activity("apply", "plasma-apply-colorscheme completed")
	k.activity("propagation", fmt.Sprintf("waiting for Plasma to report %s", theme))
	if err := k.waitForTheme(ctx, theme); err != nil {
		k.activity("propagation", err.Error())
		return nil
	}
	k.activity("ready", fmt.Sprintf("Plasma now reports %s", theme))
	return nil
}

func parseKDEThemeLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	line = strings.TrimLeft(line, "* ")
	if idx := strings.Index(line, " ("); idx >= 0 {
		line = line[:idx]
	}
	return strings.TrimSpace(line)
}

func (k *KDE) waitForTheme(ctx context.Context, theme string) error {
	deadline := time.NewTimer(8 * time.Second)
	ticker := time.NewTicker(350 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		current, err := k.Current(ctx)
		if err == nil && current == theme {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			current, currentErr := k.Current(ctx)
			if currentErr != nil {
				return fmt.Errorf("Plasma did not confirm %s yet: %v", theme, currentErr)
			}
			if current == "" {
				return fmt.Errorf("Plasma did not confirm %s yet", theme)
			}
			return fmt.Errorf("Plasma still reports %s", current)
		case <-ticker.C:
		}
	}
}

func (k *KDE) activity(stage, message string) {
	if k.log == nil {
		return
	}
	k.log(core.ActivityEntry{
		Adapter: k.DisplayName(),
		Stage:   stage,
		Message: message,
	})
}
