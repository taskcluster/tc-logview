package cmd

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/taskcluster/tc-logview/internal/config"
	"github.com/taskcluster/tc-logview/internal/presets"
	"github.com/taskcluster/tc-logview/internal/references"
)

var (
	listService string
	listLevel   string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List known log types from cached references",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Unified row type for display
		type listRow struct {
			service string
			typ     string
			level   string
			fields  string
		}

		var rows []listRow

		// Add presets
		for _, p := range presets.All {
			if listService != "" && p.Service != listService {
				continue
			}
			if listLevel != "" {
				continue // presets have no level
			}
			fields := strings.Join(p.FieldNames(), ", ")
			rows = append(rows, listRow{
				service: p.Service,
				typ:     p.Name,
				level:   "\u2014",
				fields:  fields,
			})
		}

		// Add TC reference types
		cacheDir := filepath.Join(config.CacheDir(), "references")
		idx, err := references.LoadIndex(cacheDir)
		if err == nil && !idx.IsEmpty() {
			for _, e := range idx.All() {
				if listService != "" && e.Service != listService {
					continue
				}
				if listLevel != "" && e.LogType.Level != listLevel {
					continue
				}
				fields := make([]string, 0, len(e.LogType.Fields))
				for name := range e.LogType.Fields {
					fields = append(fields, name)
				}
				sort.Strings(fields)
				rows = append(rows, listRow{
					service: e.Service,
					typ:     e.LogType.Type,
					level:   e.LogType.Level,
					fields:  strings.Join(fields, ", "),
				})
			}
		}

		if len(rows) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No matching log types found.")
			return nil
		}

		// Sort by service, then type
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].service != rows[j].service {
				return rows[i].service < rows[j].service
			}
			return rows[i].typ < rows[j].typ
		})

		// Calculate column widths
		svcW, typeW, levelW := len("SERVICE"), len("TYPE"), len("LEVEL")
		for _, r := range rows {
			if len(r.service) > svcW {
				svcW = len(r.service)
			}
			if len(r.typ) > typeW {
				typeW = len(r.typ)
			}
			if len(r.level) > levelW {
				levelW = len(r.level)
			}
		}

		w := cmd.OutOrStdout()
		fmt.Fprintf(w, "%-*s  %-*s  %-*s  %s\n", svcW, "SERVICE", typeW, "TYPE", levelW, "LEVEL", "FIELDS")
		for _, r := range rows {
			fmt.Fprintf(w, "%-*s  %-*s  %-*s  %s\n",
				svcW, r.service,
				typeW, r.typ,
				levelW, r.level,
				r.fields,
			)
		}
		fmt.Fprintf(w, "\n%d log types\n", len(rows))
		return nil
	},
}

func init() {
	listCmd.Flags().StringVar(&listService, "service", "", "filter by service name")
	listCmd.Flags().StringVar(&listLevel, "level", "", "filter by severity level")
	rootCmd.AddCommand(listCmd)
}
