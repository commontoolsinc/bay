package main

import (
	"fmt"
	"os"

	"github.com/commontoolsinc/bay/internal/cli"
)

// version is set at build time via -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	root := cli.NewRootCmd(version)
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
