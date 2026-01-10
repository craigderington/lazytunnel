package main

import (
	"os"

	"github.com/craigderington/lazytunnel/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
