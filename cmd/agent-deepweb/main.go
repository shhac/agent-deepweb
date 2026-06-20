package main

import (
	"github.com/shhac/agent-deepweb/internal/cli"
)

var version = "dev"

func main() {
	cli.Run(version)
}
