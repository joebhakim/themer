"""KDE Plasma color scheme adapter."""

import subprocess
from .base import ThemeAdapter


class KdeAdapter(ThemeAdapter):
    name = "kde"
    display_name = "KDE Plasma"
    supports_preview = True

    def list_themes(self) -> list[str]:
        result = subprocess.run(
            ["plasma-apply-colorscheme", "--list-schemes"],
            capture_output=True, text=True,
        )
        themes = []
        for line in result.stdout.splitlines():
            line = line.strip()
            if not line or line.startswith("*"):
                continue
            # Lines look like: " * WhiteSurDark (current color scheme)"
            # or just: " * BreezeDark"
            name = line.lstrip("* ").split(" (")[0].strip()
            if name:
                themes.append(name)
        return themes

    def get_current(self) -> str | None:
        result = subprocess.run(
            ["plasma-apply-colorscheme", "--list-schemes"],
            capture_output=True, text=True,
        )
        for line in result.stdout.splitlines():
            if "(current color scheme)" in line:
                return line.lstrip("* ").split(" (")[0].strip()
        return None

    def preview(self, theme_name: str) -> None:
        if self._original_theme is None:
            self._original_theme = self.get_current()
        subprocess.run(
            ["plasma-apply-colorscheme", theme_name],
            capture_output=True, text=True,
        )

    def revert(self) -> None:
        if self._original_theme:
            subprocess.run(
                ["plasma-apply-colorscheme", self._original_theme],
                capture_output=True, text=True,
            )
            self._original_theme = None

    def commit(self, theme_name: str) -> bool:
        result = subprocess.run(
            ["plasma-apply-colorscheme", theme_name],
            capture_output=True, text=True,
        )
        self._original_theme = None
        return result.returncode == 0

    def describe(self, theme_name: str) -> str:
        current = self.get_current()
        marker = " (current)" if current == theme_name else ""
        return f"  {self.display_name}: {theme_name}{marker}"
