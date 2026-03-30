package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joebhakim/themer/internal/adapters"
	"github.com/joebhakim/themer/internal/config"
	"github.com/joebhakim/themer/internal/core"
	"github.com/joebhakim/themer/internal/tracing"
	"github.com/joebhakim/themer/internal/ui"
	"github.com/spf13/cobra"
)

type app struct {
	configPath string
	trace      bool
	traceFile  string
}

func Execute() error {
	a := &app{}
	root := &cobra.Command{
		Use:           "themer",
		Short:         "Linux theme manager with profile browser and adapter diagnostics",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runBrowse()
		},
	}
	root.PersistentFlags().StringVar(&a.configPath, "config", "", "Path to themer.toml")
	root.PersistentFlags().BoolVar(&a.trace, "trace", false, "Write a per-session timing trace")
	root.PersistentFlags().StringVar(&a.traceFile, "trace-file", "", "Path to JSONL timing trace file (implies --trace)")
	root.AddCommand(
		a.newBrowseCommand(),
		a.newApplyCommand(),
		a.newCurrentCommand(),
		a.newCaptureCommand(),
		a.newDoctorCommand(),
	)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, formatError(err))
		return err
	}
	return nil
}

func (a *app) newBrowseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "browse",
		Short: "Open the interactive profile browser",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runBrowse()
		},
	}
}

func (a *app) newApplyCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "apply <profile>",
		Short: "Apply a profile non-interactively",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := a.buildManager()
			if err != nil {
				return err
			}
			defer manager.Close()
			a.reportTrace(manager, false)
			results, err := manager.ApplyProfile(context.Background(), args[0])
			for _, result := range results {
				if result.Skipped {
					fmt.Printf("%s: skipped\n", result.Adapter)
					continue
				}
				if result.Err != nil {
					fmt.Printf("%s: %s [failed: %v]\n", result.Adapter, result.Theme, result.Err)
					continue
				}
				fmt.Printf("%s: %s [ok]\n", result.Adapter, result.Theme)
			}
			return err
		},
	}
}

func (a *app) newCurrentCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "current",
		Short: "Show the current theme per adapter",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := a.buildManager()
			if err != nil {
				return err
			}
			defer manager.Close()
			a.reportTrace(manager, false)
			results := manager.Current(context.Background())
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(results)
			}
			for _, result := range results {
				if result.Error != "" {
					fmt.Printf("%s: error: %s\n", result.Display, result.Error)
					continue
				}
				if result.Theme == "" {
					fmt.Printf("%s: unknown\n", result.Display)
					continue
				}
				fmt.Printf("%s: %s\n", result.Display, result.Theme)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit machine-readable JSON")
	return cmd
}

func (a *app) newCaptureCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "capture <profile>",
		Short: "Capture current adapter state into a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := a.buildManager()
			if err != nil {
				return err
			}
			defer manager.Close()
			a.reportTrace(manager, false)
			targets, err := manager.CaptureCurrent(context.Background())
			if err != nil {
				return err
			}
			cfg := manager.Config()
			cfg.SetProfile(args[0], targets)
			if err := config.Save(a.resolveConfigPath(cfg), cfg); err != nil {
				return err
			}
			fmt.Printf("Captured profile %q with %d targets\n", args[0], len(targets))
			return nil
		},
	}
}

func (a *app) newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Validate adapter readiness and preview support",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := a.buildManager()
			if err != nil {
				return err
			}
			defer manager.Close()
			a.reportTrace(manager, false)
			diagnostics := manager.Diagnostics(context.Background())
			for _, diagnostic := range diagnostics {
				fmt.Printf("[%s] %s: %s\n", diagnostic.Level, diagnostic.Adapter, diagnostic.Message)
			}
			if len(diagnostics) == 0 {
				fmt.Println("[ok] no adapter diagnostics")
			}
			for _, adapter := range manager.EnabledAdapters() {
				support := adapter.PreviewStatus(context.Background())
				state := "apply-only"
				if support.Enabled {
					state = "preview-enabled"
				}
				fmt.Printf("%s: %s", adapter.DisplayName(), state)
				if support.Reason != "" {
					fmt.Printf(" (%s)", support.Reason)
				}
				fmt.Println()
			}
			return nil
		},
	}
}

func (a *app) runBrowse() error {
	manager, err := a.buildManager()
	if err != nil {
		return err
	}
	defer manager.Close()
	model := ui.NewBrowser(manager)
	program := tea.NewProgram(model, tea.WithAltScreen())
	_, err = program.Run()
	return err
}

func (a *app) buildManager() (*core.Manager, error) {
	cfg, err := config.Load(a.resolveConfigPath(nil))
	if err != nil {
		return nil, err
	}
	recorder, err := a.buildTraceRecorder()
	if err != nil {
		return nil, err
	}
	runner := adapters.NewInstrumentedRunner(adapters.ExecRunner{}, recorder)
	manager, err := core.NewManager(cfg, adapters.BuildWithRunner(cfg, runner))
	if err != nil {
		_ = recorder.Close()
		return nil, err
	}
	manager.SetTraceRecorder(recorder)
	return manager, nil
}

func (a *app) buildTraceRecorder() (tracing.Recorder, error) {
	if !a.trace && strings.TrimSpace(a.traceFile) == "" {
		return tracing.Disabled(), nil
	}
	return tracing.NewSession(strings.TrimSpace(a.traceFile))
}

func (a *app) reportTrace(manager *core.Manager, interactive bool) {
	if interactive {
		return
	}
	if path := manager.TracePath(); path != "" {
		fmt.Fprintf(os.Stderr, "Trace: %s\n", path)
	}
}

func (a *app) resolveConfigPath(cfg *config.Config) string {
	if strings.TrimSpace(a.configPath) != "" {
		return a.configPath
	}
	if cfg != nil && cfg.Path != "" {
		return cfg.Path
	}
	return config.DefaultPath()
}

func formatError(err error) string {
	var missing *config.ErrConfigNotFound
	if errors.As(err, &missing) {
		return fmt.Sprintf(
			"No config found at %s\n\nCreate one by copying config.example.toml from this repo to:\n  %s\n\nOr run with --config /path/to/themer.toml.",
			missing.Path,
			missing.Path,
		)
	}
	return err.Error()
}
