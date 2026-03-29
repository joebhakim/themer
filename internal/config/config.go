package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

const CurrentVersion = 2

var knownAdapters = map[string]struct{}{
	"kde":    {},
	"kitty":  {},
	"fish":   {},
	"neovim": {},
	"cursor": {},
}

type UIConfig struct {
	PreviewOnMove   bool `toml:"preview_on_move"`
	PreviewDebounce int  `toml:"preview_debounce_ms"`
	ShowDiagnostics bool `toml:"show_diagnostics"`
}

type KittyConfig struct {
	KnownThemes []string `toml:"known_themes"`
	Socket      string   `toml:"socket"`
}

type FishConfig struct {
	ThemesDir       string `toml:"themes_dir"`
	FrozenThemePath string `toml:"frozen_theme_path"`
}

type NeovimConfig struct {
	ConfigPath         string   `toml:"config_path"`
	ColorschemePattern string   `toml:"colorscheme_pattern"`
	KnownThemes        []string `toml:"known_themes"`
}

type CursorConfig struct {
	SettingsPath string   `toml:"settings_path"`
	KnownThemes  []string `toml:"known_themes"`
}

type AdaptersConfig struct {
	Kitty  KittyConfig  `toml:"kitty"`
	Fish   FishConfig   `toml:"fish"`
	Neovim NeovimConfig `toml:"neovim"`
	Cursor CursorConfig `toml:"cursor"`
}

type Profile struct {
	Targets map[string]string `toml:"targets"`
}

type Config struct {
	Version         int                `toml:"version"`
	EnabledAdapters []string           `toml:"enabled_adapters"`
	UI              UIConfig           `toml:"ui"`
	Adapters        AdaptersConfig     `toml:"adapters"`
	Profiles        map[string]Profile `toml:"profiles"`
	Path            string             `toml:"-"`
}

type ErrConfigNotFound struct {
	Path string
}

func (e *ErrConfigNotFound) Error() string {
	return fmt.Sprintf("config file not found: %s", e.Path)
}

func DefaultPath() string {
	return filepath.Join(configHome(), "themer", "themer.toml")
}

func DefaultStateDir() string {
	return filepath.Join(stateHome(), "themer")
}

func DefaultRuntimeDir() string {
	if runtime := os.Getenv("XDG_RUNTIME_DIR"); runtime != "" {
		return filepath.Join(runtime, "themer")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("themer-%d", os.Getuid()))
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &ErrConfigNotFound{Path: path}
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	cfg := defaultConfig()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.normalizePaths()
	cfg.Path = path
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Version != CurrentVersion {
		return fmt.Errorf("config version must be %d", CurrentVersion)
	}
	if len(c.EnabledAdapters) == 0 {
		return errors.New("enabled_adapters must list at least one adapter")
	}
	for _, name := range c.EnabledAdapters {
		if _, ok := knownAdapters[name]; !ok {
			return fmt.Errorf("enabled_adapters contains unknown adapter %q", name)
		}
	}
	if len(c.Profiles) == 0 {
		return errors.New("profiles must define at least one profile")
	}
	for profileName, profile := range c.Profiles {
		if len(profile.Targets) == 0 {
			return fmt.Errorf("profile %q must define at least one target", profileName)
		}
		for adapter, theme := range profile.Targets {
			if _, ok := knownAdapters[adapter]; !ok {
				return fmt.Errorf("profile %q references unknown adapter %q", profileName, adapter)
			}
			if strings.TrimSpace(theme) == "" {
				return fmt.Errorf("profile %q target %q must not be empty", profileName, adapter)
			}
		}
	}
	if c.UI.PreviewDebounce <= 0 {
		c.UI.PreviewDebounce = 120
	}
	return nil
}

func (c *Config) ProfileNames() []string {
	names := make([]string, 0, len(c.Profiles))
	for name := range c.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c *Config) SetProfile(name string, targets map[string]string) {
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	cloned := make(map[string]string, len(targets))
	for key, value := range targets {
		cloned[key] = value
	}
	c.Profiles[name] = Profile{Targets: cloned}
}

func Save(path string, cfg *Config) error {
	if path == "" {
		path = cfg.Path
	}
	if path == "" {
		path = DefaultPath()
	}
	cfg.normalizePaths()
	cfg.Path = path
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return atomicWrite(path, data, 0o644)
}

func defaultConfig() *Config {
	return &Config{
		Version:         CurrentVersion,
		EnabledAdapters: []string{"kde", "kitty", "fish", "neovim", "cursor"},
		UI: UIConfig{
			PreviewOnMove:   true,
			PreviewDebounce: 120,
			ShowDiagnostics: true,
		},
		Adapters: AdaptersConfig{
			Fish: FishConfig{
				ThemesDir:       "/usr/share/fish/themes",
				FrozenThemePath: filepath.Join(configHome(), "fish", "conf.d", "fish_frozen_theme.fish"),
			},
			Neovim: NeovimConfig{
				ConfigPath:         filepath.Join(configHome(), "nvim", "lua", "kickstart", "plugins", "theme.lua"),
				ColorschemePattern: `vim\.cmd\.colorscheme\s+['"]([^'"]+)['"]`,
				KnownThemes:        []string{"tokyonight-night", "tokyonight-day", "tokyonight-moon", "tokyonight-storm"},
			},
			Cursor: CursorConfig{
				SettingsPath: filepath.Join(configHome(), "Cursor", "User", "settings.json"),
				KnownThemes: []string{
					"Default Dark+",
					"Default Dark Modern",
					"Default Light Modern",
					"Default Light+",
					"Quiet Light",
					"Default High Contrast",
				},
			},
			Kitty: KittyConfig{
				KnownThemes: []string{
					"Obsidian",
					"Dracula",
					"Nord",
					"Catppuccin-Mocha",
					"Catppuccin-Latte",
					"Catppuccin-Frappe",
					"Catppuccin-Macchiato",
					"Gruvbox Dark",
					"Gruvbox Light",
					"Solarized Dark",
					"Solarized Light",
					"Tokyo Night",
					"Tokyo Night Storm",
					"One Half Dark",
					"One Half Light",
					"Rose Pine",
					"Rose Pine Moon",
					"Rose Pine Dawn",
					"Alabaster",
					"GitHub Dark",
					"GitHub Light",
				},
			},
		},
		Profiles: map[string]Profile{
			"nord": {
				Targets: map[string]string{
					"kde":    "BreezeDark",
					"kitty":  "Nord",
					"fish":   "nord",
					"neovim": "tokyonight-storm",
					"cursor": "Default Dark+",
				},
			},
		},
	}
}

func (c *Config) normalizePaths() {
	c.Adapters.Fish.ThemesDir = expandPath(c.Adapters.Fish.ThemesDir)
	c.Adapters.Fish.FrozenThemePath = expandPath(c.Adapters.Fish.FrozenThemePath)
	c.Adapters.Neovim.ConfigPath = expandPath(c.Adapters.Neovim.ConfigPath)
	c.Adapters.Cursor.SettingsPath = expandPath(c.Adapters.Cursor.SettingsPath)
	c.Adapters.Kitty.Socket = expandSocketPath(c.Adapters.Kitty.Socket)
}

func expandSocketPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if strings.HasPrefix(raw, "unix:") {
		return "unix:" + expandPath(strings.TrimPrefix(raw, "unix:"))
	}
	return raw
}

func expandPath(raw string) string {
	raw = strings.TrimSpace(os.ExpandEnv(raw))
	if raw == "" {
		return raw
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return raw
	}

	switch {
	case raw == "~":
		return home
	case strings.HasPrefix(raw, "~/"):
		return filepath.Join(home, strings.TrimPrefix(raw, "~/"))
	case raw == "/home/you":
		return home
	case strings.HasPrefix(raw, "/home/you/"):
		return filepath.Join(home, strings.TrimPrefix(raw, "/home/you/"))
	default:
		return raw
	}
}

func configHome() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func stateHome() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state")
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, mode); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
