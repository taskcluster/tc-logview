package main

import (
	"os"

	"github.com/lotas/tc-logview/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
