package main

import (
	"os"

	"github.com/taskcluster/tc-logview/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
