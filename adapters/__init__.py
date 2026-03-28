"""Adapter registry."""

from .kde import KdeAdapter
from .kitty import KittyAdapter
from .fish import FishAdapter
from .neovim import NeovimAdapter
from .cursor import CursorAdapter
from .base import ThemeAdapter

ADAPTERS: dict[str, type[ThemeAdapter]] = {
    "kde": KdeAdapter,
    "kitty": KittyAdapter,
    "fish": FishAdapter,
    "neovim": NeovimAdapter,
    "cursor": CursorAdapter,
}


def get_adapter(name: str, settings: dict | None = None) -> ThemeAdapter:
    cls = ADAPTERS[name]
    return cls(settings=settings or {})


def get_active_adapters(active_names: list[str], all_settings: dict) -> list[ThemeAdapter]:
    adapters = []
    for name in active_names:
        if name in ADAPTERS:
            adapters.append(get_adapter(name, all_settings.get(name, {})))
    return adapters
