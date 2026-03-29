# themer

`themer` is a Linux-first theme orchestrator for KDE Plasma, kitty, fish, Neovim, and Cursor/VS Code. The v2 rewrite replaces the old Python + `fzf` flow with a Go CLI/TUI, a versioned config schema, explicit adapter diagnostics, and safer preview behavior.

## Requirements

- Go 1.26+ if you run from a source checkout
- Linux desktop environment with any adapters you enable
- Supported adapter tools:
  - KDE: `plasma-apply-colorscheme`
  - kitty: `kitty` with remote control configured for preview
  - fish: `fish` and `fish_config`
  - Neovim: a writable config file containing your colorscheme expression
  - Cursor/VS Code: writable `settings.json` or JSONC-compatible settings file

## Installation

```bash
git clone git@github.com:joebhakim/themer.git ~/themer
ln -sf ~/themer/bin/themer ~/.local/bin/themer
mkdir -p ~/.config/themer
cp ~/themer/config.example.toml ~/.config/themer/themer.toml
```

For a broader manual-testing config with many more profiles and adapter values, use:

```bash
cp ~/themer/config.full.example.toml ~/.config/themer/themer.toml
```

For kitty preview, the simplest path is to launch `themer` from inside kitty.
If you want an explicit remote-control target instead, set a stable socket and
point `adapters.kitty.socket` at it:

```conf
allow_remote_control socket-only
listen_on unix:/tmp/kitty-theme-preview.sock
```

## Commands

```bash
themer                  # open the TUI browser
themer browse           # same as above
themer apply nord       # apply a profile
themer current          # show current themes
themer current --json   # machine-readable current state
themer capture desktop  # snapshot current state into a profile
themer doctor           # validate adapter readiness and preview support
```

## Config

The config path is `~/.config/themer/themer.toml`.

- `config.example.toml` is the small starter config.
- `config.full.example.toml` is a larger test-oriented example with more profiles, more known themes, and comments around path expansion.
- `adapters.kitty.socket` is optional. When omitted, themer will try to use the current kitty terminal session.

```toml
version = 2
enabled_adapters = ["kde", "kitty", "fish", "neovim", "cursor"]

[ui]
preview_on_move = true
preview_debounce_ms = 120

[profiles.nord.targets]
kde = "BreezeDark"
kitty = "Nord"
fish = "nord"
neovim = "tokyonight-storm"
cursor = "Default Dark+"
```

Key changes in v2:

- Profiles live under `[profiles.<name>.targets]`.
- Config is versioned and validated on startup.
- Preview is explicit per adapter. If `doctor` says an adapter is apply-only, the TUI will not try to fake preview/revert for it.
- Cursor settings are read as JSONC, so comments and trailing commas no longer crash the tool.

## Architecture

```text
cmd/themer/        Cobra entrypoint
internal/cli/      command wiring
internal/config/   TOML schema, validation, persistence
internal/core/     adapter contract, preview session manager
internal/adapters/ built-in adapters for KDE, kitty, fish, Neovim, Cursor
internal/ui/       Bubble Tea profile browser
```

The TUI is the primary interaction model. Non-interactive commands call the same adapter layer as the browser.

## Migration

v2 intentionally breaks the old CLI and config layout. See [docs/v2-migration.md](docs/v2-migration.md) for the new schema and the minimal manual migration steps from v1.

The adapter auto-discovers the socket via `$KITTY_LISTEN_ON`, `$KITTY_PID`, or glob of `/tmp/kitty-*.sock`.

### fish

Uses `fish_config theme choose` which sets global (session) variables, then promotes them to universal variables for persistence. Also rewrites the frozen theme file (`~/.config/fish/conf.d/fish_frozen_theme.fish`) so new shells start with the correct colors.

The 25+ built-in themes live in `/usr/share/fish/themes/`. Theme detection works by reading a `themer_current_theme` universal variable marker, with fallback to color-matching against known theme files.

### Neovim

Edits the Lua config file using a regex pattern. Only themes from installed colorscheme plugins work (e.g., tokyonight variants). Running Neovim instances are not affected -- restart Neovim to pick up the change.

### Cursor / VS Code

Edits `settings.json` to set `workbench.colorTheme` (or the preferred dark/light variant if `window.autoDetectColorScheme` is enabled). Cursor/VS Code auto-detect settings changes on window focus.

Only themes already installed as extensions in Cursor/VS Code are available.

## License

MIT
