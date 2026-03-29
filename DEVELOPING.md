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
