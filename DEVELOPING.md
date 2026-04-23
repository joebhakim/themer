# Developing themer

## Project structure

```text
themer/
  bin/themer              source-checkout launcher
  cmd/themer/             main package
  internal/cli/           cobra command tree
  internal/config/        config schema, validation, persistence
  internal/core/          adapter interface and preview manager
  internal/adapters/      built-in adapters
  internal/ui/            Bubble Tea browser
  config.example.toml     example v2 config
  config.full.example.toml larger v2 example for manual testing
  docs/v2-migration.md    migration notes from the legacy Python version
```

The runtime config lives at `~/.config/themer/themer.toml`.

## Tooling

- Go 1.26+
- `go test ./...` for the basic validation pass
- external adapter binaries only when testing the real integrations

The project depends on:

- `cobra` for CLI wiring
- `bubbletea`, `bubbles`, and `lipgloss` for the TUI
- `go-toml/v2` for config parsing
- `hujson` for JSONC parsing in the Cursor adapter

## Adapter contract

Each adapter implements the interface in [`internal/core/types.go`](/home/joe/themer/internal/core/types.go):

- `Validate(ctx)` returns diagnostics for `themer doctor`
- `ListThemes(ctx)` returns discoverable or configured themes
- `Current(ctx)` detects the current theme
- `Describe(ctx, theme)` returns UI-facing details
- `PreviewStatus(ctx)` reports whether preview is safe enough to enable
- `Preview(ctx, theme)` applies a temporary preview and returns a restore function
- `Apply(ctx, theme)` performs the persistent change

Preview rules in v2:

- If an adapter cannot explain how preview will restore state, it must report apply-only.
- The UI will not attempt preview for adapters that return a disabled `PreviewStatus`.
- Restore functions are owned by the core preview manager, not by the UI.

## Adding an adapter

1. Add a new type under `internal/adapters/`.
2. Make it satisfy `core.Adapter`.
3. Register it in `internal/adapters/registry.go`.
4. Extend the config schema only if the adapter needs explicit settings.
5. Add unit tests around detection, apply, and preview support decisions.

## Testing strategy

- `internal/config`: schema validation and save/load behavior
- `internal/adapters`: parsing, file updates, JSONC handling, preview support decisions
- `internal/core`: preview session lifecycle and apply orchestration

Keep adapter tests hermetic. Prefer temp files and fake runners over hitting real desktop tools in unit tests.

### Hang-focused coverage

- `go test ./...` now includes blocked-operation regressions for:
  - preview worker dequeue/apply sequencing
  - restore and flush paths that wait on preview state
  - adapter command calls that do not return promptly
- Hang-oriented tests use short test-only deadlines and, on failure, dump:
  - goroutine stacks
  - the manager activity log snapshot
- This is diagnostic coverage only. Production code still does not use timeout-based recovery.

## Manual smoke checklist

Use `config.full.example.toml` or a local config that targets your real environment.

### Kitty preview

1. Launch `themer` inside a real kitty window.
2. Move up/down quickly through several preview-enabled profiles.
3. Confirm the right-pane activity log shows a sequence like:
   - `queued preview ...`
   - `worker picked queued preview ...`
   - `calling preview adapter Kitty -> ...; waiting for adapter to return`
   - `preview adapter Kitty returned; preview applied for ...`
4. Press `enter` while a preview is visibly in progress.
5. Confirm the status line and activity log show:
   - `flushing preview before apply`
   - `flush-before-apply start`
   - `calling restore for Kitty`
   - `flush complete`
6. Press `q`, then `q` again while restore is still in progress, and confirm the second key exits immediately.

### KDE apply

1. Run `themer` in a Plasma session with KDE enabled in config.
2. Apply a profile that changes the Plasma scheme.
3. Confirm the activity log shows:
   - `requested <theme>`
   - `plasma-apply-colorscheme completed`
   - `waiting for Plasma to report <theme>`
   - either `Plasma now reports <theme>` or a propagation warning
4. If Plasma appears slow, verify the app stays responsive and the log keeps the last known stage visible.

### Fish apply modes

1. Leave `adapters.fish.apply_mode = "session_refresh"` and apply a profile that changes the fish theme.
2. Confirm themer writes the refresh script to `adapters.fish.refresh_path`.
3. In a live fish shell, run `themer fish-refresh | source` and confirm syntax colors update in-place.
4. Switch to `adapters.fish.apply_mode = "universal"`, apply again, run `themer fish-refresh | source` in the live shell once if needed, and confirm `fish -c 'set -q themer_current_theme; echo $themer_current_theme'` reports the selected theme.
5. Confirm `~/.config/fish/conf.d/fish_frozen_theme.fish` is no longer created by themer.

### Broken-path sanity checks

1. Use a profile with a missing fish theme or missing Neovim config file.
2. Confirm the UI reports a concrete apply error, not a silent stall.
3. Confirm the activity log still shows the last worker stage so the failure is distinguishable from a hang.
