package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/taskcluster/tc-logview/internal/config"
	"github.com/taskcluster/tc-logview/internal/references"
	"github.com/spf13/cobra"
)

var (
	listService string
	listLevel   string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List known log types from cached references",
	RunE: func(cmd *cobra.Command, args []string) error {
		cacheDir := filepath.Join(config.CacheDir(), "references")
		idx, err := references.LoadIndex(cacheDir)
		if err != nil {
			return fmt.Errorf("loading index: %w", err)
		}
		if idx.IsEmpty() {
			return fmt.Errorf("no references cached, run 'tc-logview sync' first")
		}

		all := idx.All()

		// Calculate column widths
		svcW, typeW, levelW := len("SERVICE"), len("TYPE"), len("LEVEL")
		var filtered []references.IndexEntry
		for _, e := range all {
			if listService != "" && e.Service != listService {
				continue
			}
			if listLevel != "" && e.LogType.Level != listLevel {
				continue
			}
			filtered = append(filtered, e)
			if len(e.Service) > svcW {
				svcW = len(e.Service)
			}
			if len(e.LogType.Type) > typeW {
				typeW = len(e.LogType.Type)
			}
			if len(e.LogType.Level) > levelW {
				levelW = len(e.LogType.Level)
			}
		}

		if len(filtered) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No matching log types found.")
			return nil
		}

		w := cmd.OutOrStdout()
		fmt.Fprintf(w, "%-*s  %-*s  %-*s  %s\n", svcW, "SERVICE", typeW, "TYPE", levelW, "LEVEL", "FIELDS")
		for _, e := range filtered {
			fields := make([]string, 0, len(e.LogType.Fields))
			for name := range e.LogType.Fields {
				fields = append(fields, name)
			}
			fmt.Fprintf(w, "%-*s  %-*s  %-*s  %s\n",
				svcW, e.Service,
				typeW, e.LogType.Type,
				levelW, e.LogType.Level,
				strings.Join(fields, ", "),
			)
		}
		fmt.Fprintf(w, "\n%d log types\n", len(filtered))
		return nil
	},
}

func init() {
	listCmd.Flags().StringVar(&listService, "service", "", "filter by service name")
	listCmd.Flags().StringVar(&listLevel, "level", "", "filter by severity level")
	rootCmd.AddCommand(listCmd)
}
