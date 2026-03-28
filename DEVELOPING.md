# Developing themer

## Project structure

```
themer/
    bin/themer              # entry point script (symlink target for ~/.local/bin/themer)
    main.py                 # CLI dispatch and fzf orchestration
    config.py               # TOML config loading and profile persistence
    fzf.py                  # fzf subprocess wrapper with live-preview event bindings
    adapters/
        __init__.py         # adapter registry
        base.py             # ThemeAdapter abstract base class
        kde.py              # KDE Plasma color scheme adapter
        kitty.py            # kitty terminal adapter (kitten themes + remote control)
        fish.py             # fish shell theme adapter
        neovim.py           # Neovim colorscheme adapter (config file edit)
        cursor.py           # Cursor/VS Code adapter (settings.json edit)
    config.example.toml     # example configuration with comments
    README.md               # user-facing documentation
    DEVELOPING.md           # this file
```

Config lives outside the repo at `~/.config/themer/config.toml`.

## Dependencies

**Zero external Python packages.** Only the Python 3.11+ standard library:
- `tomllib` for TOML parsing
- `subprocess` for shelling out to application CLIs
- `json` for VS Code/Cursor settings
- `re` for Neovim config editing
- `tempfile`, `pathlib`, `dataclasses`, `abc`, `glob`, `os`, `sys`

**External tools** (called via subprocess):
- `fzf` -- interactive fuzzy finder
- `plasma-apply-colorscheme` -- KDE Plasma theme CLI
- `kitty` -- kitty terminal (for `kitten themes` and `@ set-colors`)
- `fish` -- fish shell (for `fish_config theme`)

## Adding a new adapter

1. Create `adapters/youradapter.py` implementing `ThemeAdapter`:

```python
from .base import ThemeAdapter

class YourAdapter(ThemeAdapter):
    name = "youradapter"             # used in config.toml profiles
    display_name = "Your App"        # shown in output
    supports_preview = False         # True if live preview is supported

    def __init__(self, settings=None):
        super().__init__(settings)

    def list_themes(self) -> list[str]:
        # Return available theme names
        ...

    def get_current(self) -> str | None:
        # Detect the currently active theme
        ...

    def commit(self, theme_name: str) -> bool:
        # Permanently apply the theme, return True on success
        ...

    # Optional -- only if supports_preview = True:
    def preview(self, theme_name: str) -> None:
        # Temporarily apply theme (revertible)
        ...

    def revert(self) -> None:
        # Undo preview, restore original state
        ...

    def describe(self, theme_name: str) -> str:
        # Return text for fzf preview pane (supports ANSI escape codes)
        ...
```

2. Register it in `adapters/__init__.py`:

```python
from .youradapter import YourAdapter

ADAPTERS: dict[str, type[ThemeAdapter]] = {
    ...
    "youradapter": YourAdapter,
}
```

3. Add entries to `config.toml`:

```toml
active_adapters = [..., "youradapter"]

[profiles.nord]
youradapter = "Nord Theme"

[adapters.youradapter]
some_setting = "value"
```

That's it. The CLI, fzf integration, and profile system pick it up automatically.

## How the adapter interface works

### `list_themes() -> list[str]`

Return theme names that the application understands. These names are what gets passed to `commit()` and shown in fzf. Source them however the application expects:
- CLI commands (e.g., `plasma-apply-colorscheme --list-schemes`)
- Scanning a directory (e.g., `/usr/share/fish/themes/`)
- A curated list from config.toml (e.g., kitty themes from the GitHub repo)

### `get_current() -> str | None`

Detect which theme is active. This is used by `themer current` and to mark the active theme in fzf. Common approaches:
- Parse CLI output for a "current" marker
- Read a config/state file
- Check a marker variable the adapter previously set

### `preview(theme_name) / revert()`

For live preview during interactive browsing. Called by fzf's `focus` event binding -- fires on every cursor movement.

**Key requirements:**
- `preview()` must be fast (< 200ms ideally)
- `preview()` must be idempotent
- `revert()` must restore the exact original state
- Changes should be temporary/in-memory where possible

If your application doesn't support temporary changes, set `supports_preview = False` and skip these methods.

### `commit(theme_name) -> bool`

Permanently apply the theme. This is what runs when the user presses Enter in fzf or uses `themer apply`. Use the application's native theme-setting mechanism (CLI tool, config file edit, etc.).

### `describe(theme_name) -> str`

Text shown in fzf's preview pane. Supports ANSI escape codes for color rendering. Common pattern:

```python
def describe(self, theme_name):
    current = self.get_current()
    marker = " (current)" if current == theme_name else ""
    lines = [f"  {self.display_name}: {theme_name}{marker}"]
    # Add color swatches, config preview, etc.
    return "\n".join(lines)
```

## How fzf integration works

The `fzf.py` module wraps fzf with three event bindings:

1. **`focus:execute-silent(themer _preview-profile {})`** -- on every cursor movement, themer re-invokes itself to preview the focused item. This is a separate process so fzf stays responsive.

2. **`esc:execute-silent(themer _revert)+abort`** -- on Escape, revert all previewed adapters then close fzf.

3. **`--preview 'themer _describe-profile {}'`** -- the side pane runs themer to get descriptive text with color swatches.

The internal `_preview-profile`, `_revert`, and `_describe-profile` commands are not meant for users -- they're the mechanism by which fzf communicates with themer.

State between the main process and fzf-spawned subprocesses is coordinated via a `.state` file at `~/.config/themer/.state`, which records the original theme per adapter before previewing began.

## Design decisions

### Why Python + fzf (not a full TUI framework)

fzf provides fuzzy search, preview panes, and event bindings out of the box. Building an equivalent in ratatui/curses would be 500+ lines of UI code with no functional benefit. Python handles the adapter logic, config parsing, JSON/Lua editing, and subprocess orchestration cleanly with zero pip dependencies.

### Why adapters shell out instead of using libraries

Each application already has a CLI for theme management (plasma-apply-colorscheme, kitty kitten themes, fish_config). These CLIs handle edge cases, versioning, and state management that a library would have to reimplement. Shelling out is simpler, more maintainable, and guaranteed to stay compatible.

### Why profiles map names instead of defining colors

Themes for different applications are designed independently with care for contrast, readability, and aesthetics. A Dracula theme for kitty is crafted differently than for KDE. Rather than trying to derive one from the other or maintain a unified color palette, themer just coordinates: "when the user wants nord, use Nord in kitty, BreezeDark in KDE, etc." This respects each theme author's work.
