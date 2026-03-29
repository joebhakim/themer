package main

import (
	"os"

	"github.com/joebhakim/themer/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
