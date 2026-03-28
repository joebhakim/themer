"""Cursor (VS Code clone) theme adapter."""

import json
from pathlib import Path
from .base import ThemeAdapter

DEFAULT_SETTINGS = Path.home() / ".config" / "Cursor" / "User" / "settings.json"
DEFAULT_THEMES = [
    "Default Dark+", "Default Dark Modern", "Default Light Modern",
    "Default Light+", "Quiet Light", "Default High Contrast",
]


class CursorAdapter(ThemeAdapter):
    name = "cursor"
    display_name = "Cursor"
    supports_preview = False

    @property
    def settings_path(self) -> Path:
        return Path(self.settings.get("settings_path", str(DEFAULT_SETTINGS)))

    def list_themes(self) -> list[str]:
        return self.settings.get("known_themes", list(DEFAULT_THEMES))

    def get_current(self) -> str | None:
        path = self.settings_path
        if not path.exists():
            return None
        data = json.loads(path.read_text())
        # Check for explicit colorTheme first
        if "workbench.colorTheme" in data:
            return data["workbench.colorTheme"]
        # Fall back to preferred dark/light theme
        if data.get("window.autoDetectColorScheme"):
            return data.get("workbench.preferredDarkColorTheme",
                            data.get("workbench.preferredLightColorTheme"))
        return None

    def commit(self, theme_name: str) -> bool:
        path = self.settings_path
        if not path.exists():
            return False
        data = json.loads(path.read_text())
        # Set the theme — if autoDetect is on, set the preferred variant
        if data.get("window.autoDetectColorScheme"):
            # Heuristic: "light" in name -> light theme, else dark
            is_light = any(w in theme_name.lower() for w in ("light", "quiet"))
            if is_light:
                data["workbench.preferredLightColorTheme"] = theme_name
            else:
                data["workbench.preferredDarkColorTheme"] = theme_name
        else:
            data["workbench.colorTheme"] = theme_name
        path.write_text(json.dumps(data, indent=4) + "\n")
        return True

    def describe(self, theme_name: str) -> str:
        current = self.get_current()
        marker = " (current)" if current == theme_name else ""
        return f"  {self.display_name}: {theme_name}{marker}"
