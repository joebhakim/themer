# themer

Centralized theme manager for Linux desktops. Switch color themes across KDE Plasma, kitty, fish shell, Neovim, and Cursor/VS Code in one command -- with live preview as you browse.

## How it works

`themer` uses an adapter pattern: each application (KDE, kitty, fish, etc.) has an adapter that knows how to list, detect, preview, and commit themes using that application's **native** theme infrastructure. No colors are copy-pasted or manually maintained -- themer delegates to `plasma-apply-colorscheme`, `kitty +kitten themes`, `fish_config theme choose`, and so on.

**Profiles** coordinate themes across applications. A profile like "nord" maps to `BreezeDark` in KDE, `Nord` in kitty, `nord` in fish, `tokyonight-storm` in Neovim, and `Default Dark+` in Cursor. Applying a profile switches everything at once.

**Live preview** is the key feature. When browsing profiles or themes interactively, KDE and kitty change in real-time as you move the cursor. Press Escape and everything reverts. Press Enter to commit.

## Requirements

- **Python 3.11+** (uses `tomllib` from stdlib -- no pip packages needed)
- **fzf** (fuzzy finder, used for the interactive TUI)
- One or more supported applications:
  - **KDE Plasma 6** with `plasma-apply-colorscheme` CLI
  - **kitty** terminal with `allow_remote_control socket-only` and `listen_on` configured (needed for live preview)
  - **fish** shell 3.4+
  - **Neovim** (optional)
  - **Cursor** or VS Code (optional)

## Installation

1. Clone the repo:

```bash
git clone git@github.com:joebhakim/themer.git ~/themer
```

2. Symlink the entry point into your PATH:

```bash
ln -sf ~/themer/bin/themer ~/.local/bin/themer
```

3. Copy the example config:

```bash
mkdir -p ~/.config/themer
cp ~/themer/config.example.toml ~/.config/themer/config.toml
```

4. Edit `~/.config/themer/config.toml` to match your system (adapter paths, installed themes, etc.)

5. **For kitty live preview**, add to your `kitty.conf`:

```
allow_remote_control socket-only
listen_on unix:/tmp/kitty-{kitty_pid}.sock
```

Then restart kitty.

## Usage

### Interactive mode (recommended)

```bash
themer                # browse profiles with live preview
themer kitty          # browse kitty themes only
themer kde            # browse KDE color schemes only
themer fish           # browse fish themes only
```

The interactive modes use fzf. As you move the cursor:
- **KDE** changes instantly (panel, taskbar, window borders, all Qt apps)
- **Kitty** terminal colors change instantly (all windows)
- **Fish** and others show a preview in the fzf side pane

Press **Enter** to commit, **Escape** to revert to original.

### Non-interactive

```bash
themer apply nord           # switch everything to the "nord" profile
themer apply breeze-light   # switch to light theme
```

### Inspect

```bash
themer current              # show current theme per application
themer list                 # show all profiles and their theme mappings
```

### Save

```bash
themer save my-setup        # snapshot current themes as a new profile
```

## Configuration

Config lives at `~/.config/themer/config.toml`. See `config.example.toml` for the full reference.

### Active adapters

```toml
active_adapters = ["kde", "kitty", "fish", "neovim", "cursor"]
```

Only adapters listed here are used. Remove any you don't have installed.

### Profiles

Each profile maps adapter names to theme names:

```toml
[profiles.nord]
kde = "BreezeDark"          # name from: plasma-apply-colorscheme --list-schemes
kitty = "Nord"              # name from: kitty +kitten themes (GitHub theme repo)
fish = "nord"               # name from: fish_config theme list
neovim = "tokyonight-storm" # name from installed colorscheme plugin
cursor = "Default Dark+"    # name from VS Code/Cursor theme list
```

Profiles don't need entries for every adapter. Missing entries are skipped.

### Adapter settings

Some adapters need extra config:

```toml
[adapters.kitty]
# Kitty themes are fetched from GitHub -- this curated list is used for browsing
known_themes = ["Dracula", "Nord", "Catppuccin-Mocha", ...]

[adapters.neovim]
# Path to the Lua file where vim.cmd.colorscheme is called
config_path = "/home/you/.config/nvim/lua/kickstart/plugins/tokyonight.lua"
# Regex to find and replace the colorscheme name
colorscheme_pattern = "vim\\.cmd\\.colorscheme '([^']+)'"

[adapters.cursor]
# Path to Cursor/VS Code settings.json
settings_path = "/home/you/.config/Cursor/User/settings.json"
known_themes = ["Default Dark+", "Default Light Modern", ...]
```

## Architecture

```
~/themer/
    bin/themer                   # entry point (symlinked to ~/.local/bin/themer)
    main.py                      # CLI dispatch, fzf orchestration
    config.py                    # TOML config loading, profile model
    fzf.py                       # fzf subprocess wrapper with live-preview bindings
    adapters/
        base.py                  # ThemeAdapter ABC
        kde.py                   # KDE Plasma via plasma-apply-colorscheme
        kitty.py                 # kitty via kitten themes + kitty @ set-colors
        fish.py                  # fish via fish_config theme choose
        neovim.py                # Neovim via config file regex edit
        cursor.py                # Cursor/VS Code via settings.json edit
~/.config/themer/
    config.toml                  # profiles and adapter settings
```

### Adapter interface

Every adapter implements:

| Method | Purpose |
|--------|---------|
| `list_themes()` | Return available theme names |
| `get_current()` | Detect which theme is currently active |
| `preview(name)` | Temporarily apply a theme (revertible) |
| `revert()` | Undo the preview, restore original |
| `commit(name)` | Permanently apply a theme |
| `describe(name)` | Return text + color swatches for fzf preview pane |

To add support for a new application, create a new adapter file implementing this interface and register it in `adapters/__init__.py`. No other code changes needed.

### How live preview works

The interactive mode launches fzf with event bindings:

```
fzf --bind 'focus:execute-silent(themer _preview-profile {})'
    --bind 'esc:execute-silent(themer _revert)+abort'
    --preview 'themer _describe-profile {}'
```

- `focus` fires on every cursor movement, calling `themer _preview-profile` which invokes each adapter's `preview()` method
- `esc` calls `themer _revert` (restores original themes) then closes fzf
- `--preview` renders the side pane with color swatches and theme details

The preview mechanisms differ by adapter:

| Adapter | Preview | Revert | Commit |
|---------|---------|--------|--------|
| KDE | `plasma-apply-colorscheme` (instant, affects all Qt apps) | Apply original scheme name | Same as preview |
| kitty | `kitty @ set-colors` via socket (in-memory, no config change) | `kitty @ set-colors --reset` | `kitty +kitten themes` (writes config) |
| fish | fzf preview pane only (ANSI swatches) | N/A | `fish_config theme choose` + universal var promotion |
| neovim | N/A | N/A | Regex edit of Lua config file |
| cursor | N/A | N/A | JSON edit of settings.json |

## Adapter details

### KDE Plasma

Uses `plasma-apply-colorscheme` for everything. Changes are instant and affect all running KDE/Qt applications. No restart needed.

Available schemes depend on what's installed in `/usr/share/color-schemes/` and `~/.local/share/color-schemes/`.

### kitty

Uses two separate mechanisms:

- **Preview**: `kitty @ set-colors --all <file>` via the remote control socket. This changes terminal colors in-memory without touching any config file. `kitty @ set-colors --all --reset` reverts to whatever is in kitty.conf.
- **Commit**: `kitty +kitten themes <name>` -- the native kitten theme manager. Downloads theme from the kitty theme repository, writes it to `current-theme.conf`, and updates `kitty.conf`.

Live preview requires kitty remote control. Add to `kitty.conf`:

```
allow_remote_control socket-only
listen_on unix:/tmp/kitty-{kitty_pid}.sock
```

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
