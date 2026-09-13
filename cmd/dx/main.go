package main

import (
	"os"

	"github.com/joshualawson/dx/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(cli.Main(cli.System(version)))
}
