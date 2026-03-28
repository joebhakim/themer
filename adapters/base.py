"""Base adapter interface."""

from abc import ABC, abstractmethod


class ThemeAdapter(ABC):
    name: str = ""
    display_name: str = ""
    supports_preview: bool = True

    def __init__(self, settings: dict | None = None):
        self.settings = settings or {}
        self._original_theme: str | None = None

    @abstractmethod
    def list_themes(self) -> list[str]:
        ...

    @abstractmethod
    def get_current(self) -> str | None:
        ...

    def preview(self, theme_name: str) -> None:
        """Live preview (temporary). Default: no-op."""
        pass

    def revert(self) -> None:
        """Revert to pre-preview state. Default: no-op."""
        pass

    @abstractmethod
    def commit(self, theme_name: str) -> bool:
        """Permanently apply theme. Return True on success."""
        ...

    def describe(self, theme_name: str) -> str:
        """Text description for fzf preview pane."""
        current = self.get_current()
        marker = " (current)" if current == theme_name else ""
        return f"  {self.display_name}: {theme_name}{marker}"
