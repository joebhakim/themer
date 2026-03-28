"""Fish shell theme adapter."""

import subprocess
from pathlib import Path
from .base import ThemeAdapter

FISH_THEMES_DIR = Path("/usr/share/fish/themes")
FROZEN_THEME = Path.home() / ".config" / "fish" / "conf.d" / "fish_frozen_theme.fish"


class FishAdapter(ThemeAdapter):
    name = "fish"
    display_name = "Fish Shell"
    supports_preview = False

    def list_themes(self) -> list[str]:
        result = subprocess.run(
            ["fish", "-c", "fish_config theme list"],
            capture_output=True, text=True,
        )
        return [line.strip() for line in result.stdout.splitlines() if line.strip()]

    def get_current(self) -> str | None:
        # Check our marker universal variable
        result = subprocess.run(
            ["fish", "-c", "echo $themer_current_theme"],
            capture_output=True, text=True,
        )
        marker = result.stdout.strip()
        if marker:
            return marker
        # Fallback: match fish_color_command against theme files
        result = subprocess.run(
            ["fish", "-c", "set -S fish_color_command"],
            capture_output=True, text=True,
        )
        # Parse the actual color value (first word, ignoring flags)
        for line in result.stdout.splitlines():
            if "set in" in line or "|" in line:
                continue
            val = line.strip().split()[0] if line.strip() else ""
            if val and all(c in "0123456789abcdefABCDEF" for c in val):
                for theme_file in sorted(FISH_THEMES_DIR.glob("*.theme")):
                    colors = self._parse_theme_file_colors(theme_file)
                    theme_cmd = colors.get("fish_color_command", "").split()[0]
                    if theme_cmd == val:
                        return theme_file.stem
        return None

    def _parse_theme_file_colors(self, path: Path) -> dict[str, str]:
        colors = {}
        for line in path.read_text().splitlines():
            line = line.strip()
            if line.startswith("["):
                continue
            if line.startswith("fish_color_"):
                parts = line.split(None, 1)
                if len(parts) == 2:
                    colors[parts[0]] = parts[1]
        return colors

    def commit(self, theme_name: str) -> bool:
        # fish_config theme choose sets -g vars. We then:
        # 1. Promote them to -U for persistence
        # 2. Rewrite the frozen theme file so new shells get the right -g vars at init
        script = f'''
            fish_config theme choose "{theme_name}"
            or exit 1
            # Promote -g to -U for persistence
            for color in (__fish_theme_variables)
                set -l value $$color
                set -eU $color
                set -U $color $value
            end
            set -U themer_current_theme "{theme_name}"
            # Rewrite frozen theme file so new shells pick up these colors
            set -l frozen "{FROZEN_THEME}"
            echo '# Theme set by themer: {theme_name}' > $frozen
            for color in (__fish_theme_variables)
                set -l value $$color
                if set -q $color
                    echo "set --global $color $value" >> $frozen
                end
            end
        '''
        result = subprocess.run(
            ["fish", "-c", script],
            capture_output=True, text=True,
        )
        return result.returncode == 0

    def describe(self, theme_name: str) -> str:
        current = self.get_current()
        marker = " (current)" if current == theme_name else ""
        lines = [f"  {self.display_name}: {theme_name}{marker}"]
        theme_file = FISH_THEMES_DIR / f"{theme_name}.theme"
        if theme_file.exists():
            colors = self._parse_theme_file_colors(theme_file)
            for key in sorted(colors.keys())[:10]:
                val = colors[key]
                hex_color = val.split()[0] if val else ""
                if len(hex_color) == 6 and all(c in "0123456789abcdefABCDEF" for c in hex_color):
                    r, g, b = int(hex_color[:2], 16), int(hex_color[2:4], 16), int(hex_color[4:6], 16)
                    swatch = f"\033[48;2;{r};{g};{b}m    \033[0m"
                    lines.append(f"    {key}: {swatch} {val}")
                else:
                    lines.append(f"    {key}: {val}")
        return "\n".join(lines)
