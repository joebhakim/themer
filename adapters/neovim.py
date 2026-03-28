"""Neovim colorscheme adapter (config file edit)."""

import re
from pathlib import Path
from .base import ThemeAdapter

DEFAULT_CONFIG = Path.home() / ".config" / "nvim" / "lua" / "kickstart" / "plugins" / "tokyonight.lua"
DEFAULT_PATTERN = r"vim\.cmd\.colorscheme\s+'([^']+)'"
DEFAULT_THEMES = ["tokyonight-night", "tokyonight-day", "tokyonight-moon", "tokyonight-storm"]


class NeovimAdapter(ThemeAdapter):
    name = "neovim"
    display_name = "Neovim"
    supports_preview = False

    @property
    def config_path(self) -> Path:
        return Path(self.settings.get("config_path", str(DEFAULT_CONFIG)))

    @property
    def pattern(self) -> str:
        return self.settings.get("colorscheme_pattern", DEFAULT_PATTERN)

    def list_themes(self) -> list[str]:
        return list(DEFAULT_THEMES)

    def get_current(self) -> str | None:
        path = self.config_path
        if not path.exists():
            return None
        content = path.read_text()
        match = re.search(self.pattern, content)
        return match.group(1) if match else None

    def commit(self, theme_name: str) -> bool:
        path = self.config_path
        if not path.exists():
            return False
        content = path.read_text()
        new_content = re.sub(
            self.pattern,
            f"vim.cmd.colorscheme '{theme_name}'",
            content,
        )
        if new_content != content:
            path.write_text(new_content)
        return True

    def describe(self, theme_name: str) -> str:
        current = self.get_current()
        marker = " (current)" if current == theme_name else ""
        note = " [restart nvim to apply]" if current != theme_name else ""
        return f"  {self.display_name}: {theme_name}{marker}{note}"
