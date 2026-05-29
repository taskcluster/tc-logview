package cmd

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// version is bumped manually with each release. Keep in sync with CHANGELOG.md.
const version = "v1.3.1"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the tc-logview version",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(cmd.OutOrStdout(), versionString())
		return nil
	},
}

func versionString() string {
	return fmt.Sprintf("tc-logview %s\ncommit:     %s\nbuilt with: %s",
		version, vcsRevision(), runtime.Version())
}

// vcsRevision returns the short commit SHA from build info, or "(unknown)" if
// build info is missing (e.g. `go run`) or the revision setting isn't recorded.
func vcsRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(unknown)"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			if len(s.Value) >= 7 {
				return s.Value[:7]
			}
			return s.Value
		}
	}
	return "(unknown)"
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
