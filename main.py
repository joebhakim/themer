"""Main CLI for themer."""

import sys
import os

from .config import load_config, save_profile, save_state, load_state, clear_state
from .adapters import get_active_adapters, get_adapter, ADAPTERS
from .fzf import run_fzf

THEMER_BIN = os.path.join(os.path.expanduser("~"), ".local", "bin", "themer")


def cmd_interactive(config):
    """Interactive profile picker with live preview."""
    adapters = get_active_adapters(config.active_adapters, config.adapter_settings)
    profile_names = list(config.profiles.keys())

    if not profile_names:
        print("No profiles defined in config.toml")
        return 1

    # Save current state for revert
    original = {}
    for adapter in adapters:
        current = adapter.get_current()
        if current:
            original[adapter.name] = current
    save_state(original)

    selected = run_fzf(
        profile_names,
        header="  up/down: cycle (live preview) | enter: accept | esc: cancel+revert",
        preview_cmd=f"{THEMER_BIN} _describe-profile {{}}",
        focus_cmd=f"{THEMER_BIN} _preview-profile {{}}",
        abort_cmd=f"{THEMER_BIN} _revert",
        prompt="profile> ",
    )

    if selected:
        # Commit the selection
        profile = config.get_profile(selected)
        if profile:
            print(f"Applying profile: {selected}")
            for adapter in adapters:
                theme = profile.themes.get(adapter.name)
                if theme:
                    ok = adapter.commit(theme)
                    status = "ok" if ok else "FAILED"
                    print(f"  {adapter.display_name}: {theme} [{status}]")
        clear_state()
    else:
        # User cancelled — revert already happened via abort binding
        # But do an explicit revert just in case
        _do_revert()
        print("Cancelled — reverted to original themes.")

    return 0


def cmd_single_adapter(adapter_name: str, config):
    """Interactive single-adapter theme picker with live preview."""
    settings = config.adapter_settings.get(adapter_name, {})
    adapter = get_adapter(adapter_name, settings)
    themes = adapter.list_themes()

    if not themes:
        print(f"No themes available for {adapter.display_name}")
        return 1

    original = adapter.get_current()
    if original:
        save_state({adapter_name: original})

    selected = run_fzf(
        themes,
        header=f"  {adapter.display_name} themes | up/down: live preview | enter: accept | esc: revert",
        preview_cmd=f"{THEMER_BIN} _describe {{adapter_name}} {{}}".replace("{adapter_name}", adapter_name),
        focus_cmd=f"{THEMER_BIN} _preview {adapter_name} {{}}" if adapter.supports_preview else None,
        abort_cmd=f"{THEMER_BIN} _revert" if adapter.supports_preview else None,
        prompt=f"{adapter_name}> ",
        current=original,
    )

    if selected:
        ok = adapter.commit(selected)
        status = "ok" if ok else "FAILED"
        print(f"{adapter.display_name}: {selected} [{status}]")
        clear_state()
    else:
        if adapter.supports_preview and original:
            _do_revert()
        print("Cancelled.")

    return 0


def cmd_apply(profile_name: str, config):
    """Non-interactive apply."""
    profile = config.get_profile(profile_name)
    if not profile:
        print(f"Unknown profile: {profile_name}")
        print(f"Available: {', '.join(config.profiles.keys())}")
        return 1

    adapters = get_active_adapters(config.active_adapters, config.adapter_settings)
    for adapter in adapters:
        theme = profile.themes.get(adapter.name)
        if theme:
            ok = adapter.commit(theme)
            status = "ok" if ok else "FAILED"
            print(f"  {adapter.display_name}: {theme} [{status}]")
    return 0


def cmd_current(config):
    """Show current theme per adapter."""
    adapters = get_active_adapters(config.active_adapters, config.adapter_settings)
    for adapter in adapters:
        current = adapter.get_current()
        print(f"  {adapter.display_name}: {current or '(unknown)'}")
    return 0


def cmd_list(config):
    """List profiles."""
    for name, profile in config.profiles.items():
        themes = ", ".join(f"{k}={v}" for k, v in profile.themes.items())
        print(f"  {name}: {themes}")
    return 0


def cmd_save(name: str, config):
    """Snapshot current state as a new profile."""
    if name in config.profiles:
        print(f"Profile '{name}' already exists.")
        return 1

    adapters = get_active_adapters(config.active_adapters, config.adapter_settings)
    themes = {}
    for adapter in adapters:
        current = adapter.get_current()
        if current:
            themes[adapter.name] = current

    save_profile(name, themes)
    print(f"Saved profile '{name}':")
    for k, v in sorted(themes.items()):
        print(f"  {k} = {v}")
    return 0


# --- Internal commands (called by fzf bindings) ---

def cmd_preview_profile(profile_name: str, config):
    """Live-preview all previewable adapters for a profile."""
    profile = config.get_profile(profile_name)
    if not profile:
        return 1
    adapters = get_active_adapters(config.active_adapters, config.adapter_settings)
    for adapter in adapters:
        theme = profile.themes.get(adapter.name)
        if theme and adapter.supports_preview:
            adapter.preview(theme)
    return 0


def cmd_preview_single(adapter_name: str, theme_name: str, config):
    """Live-preview a single adapter."""
    settings = config.adapter_settings.get(adapter_name, {})
    adapter = get_adapter(adapter_name, settings)
    if adapter.supports_preview:
        adapter.preview(theme_name)
    return 0


def cmd_describe_profile(profile_name: str, config):
    """Text output for fzf preview pane."""
    profile = config.get_profile(profile_name)
    if not profile:
        print(f"Unknown profile: {profile_name}")
        return 1
    print(f"Profile: {profile_name}\n")
    adapters = get_active_adapters(config.active_adapters, config.adapter_settings)
    for adapter in adapters:
        theme = profile.themes.get(adapter.name)
        if theme:
            print(adapter.describe(theme))
        else:
            print(f"  {adapter.display_name}: (not set)")
    return 0


def cmd_describe_single(adapter_name: str, theme_name: str, config):
    """Text output for fzf preview pane (single adapter)."""
    settings = config.adapter_settings.get(adapter_name, {})
    adapter = get_adapter(adapter_name, settings)
    print(adapter.describe(theme_name))
    return 0


def _do_revert():
    """Revert all adapters to pre-preview state."""
    state = load_state()
    if not state:
        return
    config = load_config()
    for adapter_name, theme_name in state.items():
        if adapter_name in ADAPTERS:
            settings = config.adapter_settings.get(adapter_name, {})
            adapter = get_adapter(adapter_name, settings)
            if adapter.supports_preview:
                # For kitty, use the revert mechanism (reset to config)
                if adapter_name == "kitty":
                    adapter.revert()
                else:
                    adapter.preview(theme_name)
    clear_state()


def main():
    args = sys.argv[1:]

    if not args:
        config = load_config()
        return cmd_interactive(config)

    cmd = args[0]

    # Internal commands (called by fzf)
    if cmd == "_preview-profile" and len(args) >= 2:
        config = load_config()
        return cmd_preview_profile(args[1], config)
    elif cmd == "_preview" and len(args) >= 3:
        config = load_config()
        return cmd_preview_single(args[1], args[2], config)
    elif cmd == "_revert":
        _do_revert()
        return 0
    elif cmd == "_describe-profile" and len(args) >= 2:
        config = load_config()
        return cmd_describe_profile(args[1], config)
    elif cmd == "_describe" and len(args) >= 3:
        config = load_config()
        return cmd_describe_single(args[1], args[2], config)

    # Public commands
    elif cmd == "apply" and len(args) >= 2:
        config = load_config()
        return cmd_apply(args[1], config)
    elif cmd == "current":
        config = load_config()
        return cmd_current(config)
    elif cmd == "list":
        config = load_config()
        return cmd_list(config)
    elif cmd == "save" and len(args) >= 2:
        config = load_config()
        return cmd_save(args[1], config)
    elif cmd in ADAPTERS:
        config = load_config()
        return cmd_single_adapter(cmd, config)
    else:
        print("Usage: themer [command]")
        print()
        print("Commands:")
        print("  (no args)        Interactive profile picker with live preview")
        print("  apply <profile>  Apply a profile non-interactively")
        print("  current          Show current theme per adapter")
        print("  list             List all profiles")
        print("  save <name>      Save current themes as a new profile")
        print(f"  <adapter>        Browse themes for one app ({', '.join(ADAPTERS.keys())})")
        return 1
