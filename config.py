"""Load and write themer configuration."""

import tomllib
from dataclasses import dataclass, field
from pathlib import Path

CONFIG_PATH = Path.home() / ".config" / "themer" / "config.toml"
STATE_PATH = Path.home() / ".config" / "themer" / ".state"


@dataclass
class Profile:
    name: str
    themes: dict[str, str]  # adapter_name -> theme_name


@dataclass
class Config:
    active_adapters: list[str]
    profiles: dict[str, Profile]
    adapter_settings: dict[str, dict]  # adapter_name -> settings dict

    def get_profile(self, name: str) -> Profile | None:
        return self.profiles.get(name)


def load_config(path: Path = CONFIG_PATH) -> Config:
    with open(path, "rb") as f:
        raw = tomllib.load(f)

    active = raw.get("active_adapters", [])
    profiles = {}
    for pname, pdata in raw.get("profiles", {}).items():
        profiles[pname] = Profile(name=pname, themes=dict(pdata))

    adapter_settings = {}
    for aname, adata in raw.get("adapters", {}).items():
        adapter_settings[aname] = dict(adata)

    return Config(
        active_adapters=active,
        profiles=profiles,
        adapter_settings=adapter_settings,
    )


def save_profile(name: str, themes: dict[str, str], path: Path = CONFIG_PATH):
    """Append a new profile to config.toml."""
    lines = [f'\n[profiles.{name}]']
    for adapter, theme in sorted(themes.items()):
        lines.append(f'{adapter} = "{theme}"')
    lines.append("")

    with open(path, "a") as f:
        f.write("\n".join(lines))


def save_state(original_themes: dict[str, str]):
    """Save pre-preview state so _revert can restore it."""
    STATE_PATH.parent.mkdir(parents=True, exist_ok=True)
    lines = [f"{k}={v}" for k, v in original_themes.items()]
    STATE_PATH.write_text("\n".join(lines))


def load_state() -> dict[str, str]:
    """Load saved pre-preview state."""
    if not STATE_PATH.exists():
        return {}
    result = {}
    for line in STATE_PATH.read_text().splitlines():
        if "=" in line:
            k, v = line.split("=", 1)
            result[k] = v
    return result


def clear_state():
    if STATE_PATH.exists():
        STATE_PATH.unlink()
