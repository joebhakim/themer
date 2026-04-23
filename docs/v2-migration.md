# v2 Migration

v2 is a clean break from the old Python implementation.

## Config path

- v1: `~/.config/themer/config.toml`
- v2: `~/.config/themer/themer.toml`

## Schema changes

- Add `version = 2` at the top level.
- Rename `active_adapters` to `enabled_adapters`.
- Move profile mappings under `[profiles.<name>.targets]`.
- Move UI settings under `[ui]`.
- Keep adapter-specific settings under `[adapters.<name>]`.

Example:

```toml
version = 2
enabled_adapters = ["kde", "kitty", "fish", "neovim", "cursor"]

[profiles.nord.targets]
kde = "BreezeDark"
kitty = "Nord"
fish = "nord"
neovim = "tokyonight-storm"
cursor = "Default Dark+"
```

## Command changes

- `themer` now launches the Bubble Tea browser directly.
- `themer apply <profile>` still applies a profile.
- `themer current` still reports current themes.
- `themer save <name>` is replaced by `themer capture <name>`.
- `themer fish-refresh | source` refreshes a live fish shell when using `adapters.fish.apply_mode = "session_refresh"`.
- Per-adapter interactive pickers from v1 are gone.
- Internal preview helper commands from the `fzf` implementation are gone.

## Behavior changes

- Preview is only enabled where the adapter can justify it.
- Cursor settings are parsed as JSONC.
- Fish no longer writes startup globals in `conf.d`; choose between explicit session refresh and universal-variable persistence.
- Neovim writes fail if the configured pattern matches zero or multiple lines.
- Kitty preview requires an explicit socket via config or `KITTY_LISTEN_ON`.
