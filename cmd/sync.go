package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/taskcluster/tc-logview/internal/config"
	"github.com/taskcluster/tc-logview/internal/references"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Fetch and cache log type references from Taskcluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		cacheDir := filepath.Join(config.CacheDir(), "references")

		urls := cfg.UniqueRootURLs()
		if len(urls) == 0 {
			return fmt.Errorf("no environments configured")
		}

		for _, rootURL := range urls {
			fmt.Fprintf(cmd.OutOrStdout(), "Syncing references from %s ...\n", rootURL)
			services, err := references.Sync(rootURL, cacheDir)
			if err != nil {
				return fmt.Errorf("syncing %s: %w", rootURL, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  Synced %d services: %v\n", len(services), services)
		}

		// Load and display summary
		idx, err := references.LoadIndex(cacheDir)
		if err != nil {
			return fmt.Errorf("loading index: %w", err)
		}
		all := idx.All()
		types := map[string]bool{}
		for _, e := range all {
			types[e.LogType.Type] = true
		}
		fmt.Fprintf(cmd.OutOrStdout(), "\nTotal: %d log types across %d service entries\n", len(types), len(all))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(syncCmd)
}
