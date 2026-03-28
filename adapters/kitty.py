"""Kitty terminal adapter with live preview via kitty @ set-colors."""

import glob
import os
import subprocess
import tempfile
from pathlib import Path
from .base import ThemeAdapter

CURRENT_THEME_CONF = Path.home() / ".config" / "kitty" / "current-theme.conf"


def _parse_colors(text: str) -> dict[str, str]:
    """Parse color key-value pairs from kitty theme format."""
    colors = {}
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#") or line.startswith("//"):
            continue
        parts = line.split(None, 1)
        if len(parts) == 2 and (parts[0].startswith("color") or parts[0] in (
            "background", "foreground", "cursor", "selection_background", "selection_foreground",
        )):
            colors[parts[0]] = parts[1].strip()
    return colors


def _find_kitty_socket() -> str | None:
    """Find the kitty remote control socket."""
    # Check KITTY_LISTEN_ON env var first
    listen_on = os.environ.get("KITTY_LISTEN_ON")
    if listen_on:
        return listen_on
    # Check KITTY_PID and try the conventional socket path
    pid = os.environ.get("KITTY_PID")
    if pid:
        sock = f"/tmp/kitty-{pid}.sock"
        if os.path.exists(sock):
            return f"unix:{sock}"
    # Glob for any kitty socket
    for sock in glob.glob("/tmp/kitty-*.sock"):
        return f"unix:{sock}"
    return None


def _kitty_remote(*args: str) -> subprocess.CompletedProcess:
    """Run kitty @ command, using socket if available."""
    socket = _find_kitty_socket()
    cmd = ["kitty", "@"]
    if socket:
        cmd = ["kitty", "@", "--to", socket]
    cmd.extend(args)
    return subprocess.run(cmd, capture_output=True, text=True)


class KittyAdapter(ThemeAdapter):
    name = "kitty"
    display_name = "Kitty"
    supports_preview = True

    def list_themes(self) -> list[str]:
        return self.settings.get("known_themes", [])

    def get_current(self) -> str | None:
        if not CURRENT_THEME_CONF.exists():
            return None
        # First check for ## name: header (kitten-applied themes)
        for line in CURRENT_THEME_CONF.read_text().splitlines():
            if line.startswith("## name:"):
                return line.split(":", 1)[1].strip()
        # Fallback: match current colors against known themes
        current_colors = _parse_colors(CURRENT_THEME_CONF.read_text())
        if not current_colors:
            return None
        for theme_name in self.list_themes():
            dump = subprocess.run(
                ["kitty", "+kitten", "themes", "--dump-theme", theme_name],
                capture_output=True, text=True,
            )
            if dump.returncode == 0 and dump.stdout:
                theme_colors = _parse_colors(dump.stdout)
                if (theme_colors.get("background") == current_colors.get("background")
                        and theme_colors.get("foreground") == current_colors.get("foreground")):
                    return theme_name
        return None

    def preview(self, theme_name: str) -> None:
        if self._original_theme is None:
            self._original_theme = self.get_current()
        dump = subprocess.run(
            ["kitty", "+kitten", "themes", "--dump-theme", theme_name],
            capture_output=True, text=True,
        )
        if dump.returncode != 0 or not dump.stdout.strip():
            return
        with tempfile.NamedTemporaryFile(mode="w", suffix=".conf", delete=False) as f:
            f.write(dump.stdout)
            tmppath = f.name
        _kitty_remote("set-colors", "--all", tmppath)
        Path(tmppath).unlink(missing_ok=True)

    def revert(self) -> None:
        _kitty_remote("set-colors", "--all", "--reset")
        self._original_theme = None

    def commit(self, theme_name: str) -> bool:
        result = subprocess.run(
            ["kitty", "+kitten", "themes", "--reload-in=all", theme_name],
            capture_output=True, text=True,
        )
        self._original_theme = None
        return result.returncode == 0

    def describe(self, theme_name: str) -> str:
        current = self.get_current()
        marker = " (current)" if current == theme_name else ""
        dump = subprocess.run(
            ["kitty", "+kitten", "themes", "--dump-theme", theme_name],
            capture_output=True, text=True,
        )
        lines = [f"  {self.display_name}: {theme_name}{marker}"]
        if dump.returncode == 0 and dump.stdout:
            for dline in dump.stdout.splitlines()[:20]:
                dline = dline.strip()
                if dline and not dline.startswith("#") and not dline.startswith("//"):
                    parts = dline.split(None, 1)
                    if len(parts) == 2 and parts[1].startswith("#") and len(parts[1]) == 7:
                        hex_color = parts[1][1:]
                        try:
                            r, g, b = int(hex_color[:2], 16), int(hex_color[2:4], 16), int(hex_color[4:6], 16)
                            swatch = f"\033[48;2;{r};{g};{b}m    \033[0m"
                            lines.append(f"    {parts[0]:24s} {swatch} {parts[1]}")
                        except ValueError:
                            lines.append(f"    {dline}")
                    else:
                        lines.append(f"    {dline}")
        return "\n".join(lines)
