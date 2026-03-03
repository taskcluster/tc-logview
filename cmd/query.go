package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lotas/tc-logview/internal/cache"
	"github.com/lotas/tc-logview/internal/config"
	"github.com/lotas/tc-logview/internal/filter"
	"github.com/lotas/tc-logview/internal/format"
	"github.com/lotas/tc-logview/internal/gcp"
	"github.com/lotas/tc-logview/internal/references"
	"github.com/spf13/cobra"
)

var (
	queryType    string
	queryService string
	queryWhere   []string
	queryFilter  string
	querySince   string
	queryFrom    string
	queryTo      string
	queryLimit   int
	queryOffset  int
	queryJSON    bool
	queryRaw     bool
	queryNoCache bool
)

const resultsCacheTTL = 14 * 24 * time.Hour

var queryCmd = &cobra.Command{
	Use:   "query",
	Short: "Query GCP Cloud Logging for Taskcluster log entries",
	RunE:  runQuery,
}

func init() {
	queryCmd.Flags().StringVar(&queryType, "type", "", "log type (e.g. worker-stopped)")
	queryCmd.Flags().StringVar(&queryService, "service", "", "filter by service (for shared types)")
	queryCmd.Flags().StringSliceVar(&queryWhere, "where", nil, "field shorthand filter (e.g. workerPoolId=\"proj-misc\")")
	queryCmd.Flags().StringVar(&queryFilter, "filter", "", "raw GCP filter expression")
	queryCmd.Flags().StringVar(&querySince, "since", "", "relative time window (e.g. 2h, 30m, 1d)")
	queryCmd.Flags().StringVar(&queryFrom, "from", "", "absolute start time (RFC3339)")
	queryCmd.Flags().StringVar(&queryTo, "to", "", "absolute end time (RFC3339)")
	queryCmd.Flags().IntVar(&queryLimit, "limit", 100, "max entries to return")
	queryCmd.Flags().IntVar(&queryOffset, "offset", 0, "skip N entries (cached results only)")
	queryCmd.Flags().BoolVar(&queryJSON, "json", false, "output as JSONL")
	queryCmd.Flags().BoolVar(&queryRaw, "raw", false, "output full raw GCP entries")
	queryCmd.Flags().BoolVar(&queryNoCache, "no-cache", false, "skip cache")
	rootCmd.AddCommand(queryCmd)
}

func runQuery(cmd *cobra.Command, args []string) error {
	env, err := resolveEnv()
	if err != nil {
		return err
	}

	// Load references index
	refCacheDir := filepath.Join(config.CacheDir(), "references")
	idx, err := references.LoadIndex(refCacheDir)
	if err != nil || idx.IsEmpty() {
		fmt.Fprintln(os.Stderr, "Warning: no references cached. Run 'tc-logview sync' for type-aware features.")
		idx = nil
	}

	// Auto-detect service if type is unique
	service := queryService
	var fieldNames []string
	if idx != nil && queryType != "" {
		if service == "" {
			service = idx.ServiceFor(queryType)
		}
		fieldNames = idx.FieldNames(queryType)
	}

	// Resolve time window
	fromTime, toTime, err := resolveTimeWindow(querySince, queryFrom, queryTo)
	if err != nil {
		return err
	}

	// Build filter
	filterStr, err := filter.Build(filter.Params{
		Cluster:    env.Cluster,
		LogType:    queryType,
		Service:    service,
		Where:      queryWhere,
		RawFilter:  queryFilter,
		FieldNames: fieldNames,
	})
	if err != nil {
		return fmt.Errorf("building filter: %w", err)
	}

	// Add time constraints to filter
	filterStr += fmt.Sprintf(` AND timestamp>="%s" AND timestamp<="%s"`,
		fromTime.Format(time.RFC3339), toTime.Format(time.RFC3339))

	fmt.Fprintf(os.Stderr, "Filter: %s\n", filterStr)
	fmt.Fprintf(os.Stderr, "Time: %s to %s\n", fromTime.Format(time.RFC3339), toTime.Format(time.RFC3339))

	// Check results cache
	resultsCache := cache.New(filepath.Join(config.CacheDir(), "results"), resultsCacheTTL)
	snappedFrom := fromTime.Truncate(time.Minute)
	snappedTo := toTime.Truncate(time.Minute)
	cacheKey := resultsCache.Key(env.Cluster, snappedFrom.Format(time.RFC3339), snappedTo.Format(time.RFC3339), filterStr)

	var rawEntries []map[string]interface{}
	var totalCount int

	if !queryNoCache {
		if data, ok := resultsCache.Get(cacheKey); ok {
			rawEntries, err = gcp.EntriesFromJSON(data)
			if err == nil {
				fmt.Fprintf(os.Stderr, "Using cached results (%d entries)\n", len(rawEntries))
				totalCount = len(rawEntries)
			}
		}
	}

	// Query GCP if not cached
	if rawEntries == nil {
		ctx := context.Background()
		client, err := gcp.NewClient(ctx, env.ProjectID, env.KeyPath)
		if err != nil {
			return fmt.Errorf("creating GCP client: %w", err)
		}
		defer client.Close()

		fmt.Fprintln(os.Stderr, "Querying GCP Cloud Logging...")
		result, err := client.Query(ctx, filterStr, queryLimit+queryOffset)
		if err != nil {
			return fmt.Errorf("querying logs: %w", err)
		}

		rawEntries = result.Entries
		totalCount = result.Total

		// Cache the results
		if data, err := json.Marshal(rawEntries); err == nil {
			resultsCache.Set(cacheKey, data)
		}
	}

	// Apply offset and limit
	if queryOffset > 0 {
		if queryOffset >= len(rawEntries) {
			rawEntries = nil
		} else {
			rawEntries = rawEntries[queryOffset:]
		}
	}
	if queryLimit > 0 && queryLimit < len(rawEntries) {
		rawEntries = rawEntries[:queryLimit]
	}

	// Extract fields and format
	if fieldNames == nil {
		fieldNames = []string{}
	}
	entries := make([]format.LogEntry, len(rawEntries))
	for i, raw := range rawEntries {
		entries[i] = gcp.ExtractFields(raw, fieldNames)
	}

	// Add service field when query spans multiple services
	if service == "" {
		for i, raw := range rawEntries {
			entries[i].Fields["service"] = gcp.ExtractService(raw)
		}
		fieldNames = append([]string{"service"}, fieldNames...)
	}

	// Select formatter
	var formatter format.Formatter
	switch {
	case queryRaw:
		formatter = &format.Raw{}
	case queryJSON:
		formatter = &format.JSONL{}
	default:
		formatter = &format.Columns{}
	}

	return formatter.Format(os.Stdout, entries, fieldNames, totalCount)
}

// resolveTimeWindow parses the time flags and returns absolute from/to times.
// Default is --since 1h.
func resolveTimeWindow(since, from, to string) (time.Time, time.Time, error) {
	now := time.Now().UTC()

	if from != "" && to != "" {
		fromTime, err := time.Parse(time.RFC3339, from)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parsing --from: %w", err)
		}
		toTime, err := time.Parse(time.RFC3339, to)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parsing --to: %w", err)
		}
		return fromTime, toTime, nil
	}

	if from != "" {
		fromTime, err := time.Parse(time.RFC3339, from)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parsing --from: %w", err)
		}
		return fromTime, now, nil
	}

	// Default: --since 1h
	sinceStr := since
	if sinceStr == "" {
		sinceStr = "1h"
	}

	d, err := parseDuration(sinceStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parsing --since: %w", err)
	}

	return now.Add(-d), now, nil
}

// parseDuration parses duration strings like "30m", "2h", "1d", "7d".
// Supports m (minutes), h (hours), d (days).
func parseDuration(s string) (time.Duration, error) {
	re := regexp.MustCompile(`^(\d+)([mhd])$`)
	m := re.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid duration %q (use e.g. 30m, 2h, 1d)", s)
	}

	val, _ := strconv.Atoi(m[1])
	switch m[2] {
	case "m":
		return time.Duration(val) * time.Minute, nil
	case "h":
		return time.Duration(val) * time.Hour, nil
	case "d":
		return time.Duration(val) * 24 * time.Hour, nil
	}
	return 0, fmt.Errorf("invalid duration unit %q", m[2])
}

// listTypeFields returns a formatted string of available fields for a type.
func listTypeFields(idx *references.Index, typeName string) string {
	if idx == nil {
		return ""
	}
	fields := idx.FieldNames(typeName)
	if len(fields) == 0 {
		return ""
	}
	return fmt.Sprintf("Available fields for %s: %s", typeName, strings.Join(fields, ", "))
}
