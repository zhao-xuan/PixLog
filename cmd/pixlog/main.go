package main

import (
	"os"

	"github.com/pixlog/pixlog/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
