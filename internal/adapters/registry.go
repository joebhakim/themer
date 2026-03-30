package adapters

import (
	"github.com/joebhakim/themer/internal/config"
	"github.com/joebhakim/themer/internal/core"
)

func Build(cfg *config.Config) []core.Adapter {
	return BuildWithRunner(cfg, ExecRunner{})
}

func BuildWithRunner(cfg *config.Config, runner CommandRunner) []core.Adapter {
	return []core.Adapter{
		NewKDE(runner),
		NewKitty(cfg.Adapters.Kitty, runner),
		NewFish(cfg.Adapters.Fish, runner),
		NewNeovim(cfg.Adapters.Neovim),
		NewCursor(cfg.Adapters.Cursor),
	}
}
