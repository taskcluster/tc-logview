package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/taskcluster/tc-logview/internal/cache"
	"github.com/taskcluster/tc-logview/internal/config"
	"github.com/taskcluster/tc-logview/internal/filter"
	"github.com/taskcluster/tc-logview/internal/format"
	"github.com/taskcluster/tc-logview/internal/gcp"
	"github.com/taskcluster/tc-logview/internal/presets"
	"github.com/taskcluster/tc-logview/internal/references"
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
	// When not using --json/--raw, default to verbose unless explicitly set
	if !queryJSON && !queryRaw && !cmd.Flags().Changed("verbose") {
		verbose = true
	}

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

	// Parse comma-separated types
	var types []string
	if queryType != "" {
		types = strings.Split(queryType, ",")
	}

	// Check if the type is a preset (e.g. k8s.pod-crash, cloudsql.errors)
	var preset *presets.Preset
	if len(types) == 1 {
		preset = presets.Lookup(types[0])
	}

	if preset != nil {
		// Preset mode: use preset's filter and field mappings
		presetFilter := preset.Filter
		skipCluster := false
		if strings.HasPrefix(preset.Name, "cloudsql.") {
			if env.CloudSQLInstance == "" {
				return fmt.Errorf("preset %q requires cloudsql_instance in env config (e.g. cloudsql_instance: \"taskcluster-prod-firefoxcitc-v1\")", preset.Name)
			}
			databaseID := env.ProjectID + ":" + env.CloudSQLInstance
			presetFilter = fmt.Sprintf("resource.labels.database_id=%q AND %s", databaseID, preset.Filter)
			skipCluster = true
		}
		filterStr, err := filter.Build(filter.Params{
			Cluster:      env.Cluster,
			SkipCluster:  skipCluster,
			PresetFilter: presetFilter,
			Where:        queryWhere,
			RawFilter:    queryFilter,
			FieldMap:     preset.Fields,
			FieldNames:   preset.FieldNames(),
		})
		if err != nil {
			return fmt.Errorf("building filter: %w", err)
		}

		// Add time constraints
		fromTime, toTime, err := resolveTimeWindow(querySince, queryFrom, queryTo)
		if err != nil {
			return err
		}
		filterStr += fmt.Sprintf(` AND timestamp>="%s" AND timestamp<="%s"`,
			fromTime.Format(time.RFC3339), toTime.Format(time.RFC3339))

		logInfo("Filter: %s", filterStr)
		logInfo("Time: %s to %s", fromTime.Format(time.RFC3339), toTime.Format(time.RFC3339))

		// Query, cache, format — same as TC path
		resultsCache := cache.New(filepath.Join(config.CacheDir(), "results"), resultsCacheTTL)
		cacheKey := resultsCache.Key(env.Cluster, fromTime.Format(time.RFC3339), toTime.Format(time.RFC3339), filterStr)

		var rawEntries []map[string]interface{}
		var totalCount int

		if !queryNoCache {
			if data, ok := resultsCache.Get(cacheKey); ok {
				rawEntries, err = gcp.EntriesFromJSON(data)
				if err == nil {
					logInfo("Using cached results (%d entries)", len(rawEntries))
					totalCount = len(rawEntries)
				}
			}
		}

		if rawEntries == nil {
			ctx := context.Background()
			client, err := gcp.NewClient(ctx, env.ProjectID, env.KeyPath)
			if err != nil {
				return fmt.Errorf("creating GCP client: %w", err)
			}
			defer client.Close()

			logInfo("Querying GCP Cloud Logging...")
			result, err := client.Query(ctx, filterStr, queryLimit+queryOffset)
			if err != nil {
				return fmt.Errorf("querying logs: %w", err)
			}

			rawEntries = result.Entries
			totalCount = result.Total

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

		slices.Reverse(rawEntries)

		// Extract fields — use preset field names + message
		fieldNames := preset.FieldNames()
		if !slices.Contains(fieldNames, "message") {
			fieldNames = append(fieldNames, "message")
		}

		entries := make([]format.LogEntry, len(rawEntries))
		for i, raw := range rawEntries {
			entries[i] = gcp.ExtractFieldsWithPaths(raw, fieldNames, preset.Fields)
		}

		// Apply message transform if the preset defines one (e.g. postgres log parsing)
		if preset.MessageTransform != nil {
			for i := range entries {
				if msg := entries[i].Fields["message"]; msg != "" {
					entries[i].Fields["message"] = preset.MessageTransform(msg)
				}
			}
		}

		fieldNames = filterNonEmptyFields(entries, fieldNames)

		var formatter format.Formatter
		switch {
		case queryRaw:
			formatter = &format.Raw{}
		case queryJSON:
			var fw io.Writer
			if verbose {
				fw = os.Stderr
			}
			formatter = &format.JSONL{FooterWriter: fw}
		default:
			formatter = &format.Columns{FooterWriter: os.Stdout}
		}

		return formatter.Format(os.Stdout, entries, fieldNames, totalCount)
	}

	// Resolve types and fields
	service := queryService
	var fieldNames []string
	if idx != nil {
		// Auto-discover types when --type omitted but --where provided
		if len(types) == 0 && len(queryWhere) > 0 {
			whereFields := parseWhereFieldNames(queryWhere)
			types = idx.TypesWithFields(whereFields)
			if len(types) == 0 {
				return fmt.Errorf("no log types found with fields: %s", strings.Join(whereFields, ", "))
			}
			logInfo("Matched types: %s", strings.Join(types, ", "))
		}

		if len(types) > 0 {
			fieldNames = idx.FieldNamesForTypes(types)
		}

		// Auto-detect service only for single type
		if service == "" && len(types) == 1 {
			service = idx.ServiceFor(types[0])
		}
	}

	// Resolve time window
	fromTime, toTime, err := resolveTimeWindow(querySince, queryFrom, queryTo)
	if err != nil {
		return err
	}
	// Build filter
	filterStr, err := filter.Build(filter.Params{
		Cluster:    env.Cluster,
		LogTypes:   types,
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

	logInfo("Filter: %s", filterStr)
	logInfo("Time: %s to %s", fromTime.Format(time.RFC3339), toTime.Format(time.RFC3339))

	// Check results cache
	resultsCache := cache.New(filepath.Join(config.CacheDir(), "results"), resultsCacheTTL)
	cacheKey := resultsCache.Key(env.Cluster, fromTime.Format(time.RFC3339), toTime.Format(time.RFC3339), filterStr)

	var rawEntries []map[string]interface{}
	var totalCount int

	if !queryNoCache {
		if data, ok := resultsCache.Get(cacheKey); ok {
			rawEntries, err = gcp.EntriesFromJSON(data)
			if err == nil {
				logInfo("Using cached results (%d entries)", len(rawEntries))
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

		logInfo("Querying GCP Cloud Logging...")
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

	// Reverse to chronological order (oldest first) for natural top-to-bottom reading
	slices.Reverse(rawEntries)

	// Extract fields and format
	if fieldNames == nil {
		fieldNames = []string{}
	}

	entries := make([]format.LogEntry, len(rawEntries))
	for i, raw := range rawEntries {
		entries[i] = gcp.ExtractFields(raw, fieldNames)
	}

	// Remove columns that are empty across all entries
	fieldNames = filterNonEmptyFields(entries, fieldNames)

	// Add Type column when query spans multiple types (after filtering to always preserve it)
	if len(types) != 1 {
		for i, raw := range rawEntries {
			entries[i].Fields["Type"] = gcp.ExtractType(raw)
		}
		fieldNames = append([]string{"Type"}, fieldNames...)
	}

	// Add Service column when no --service filter is specified
	if queryService == "" {
		for i, raw := range rawEntries {
			entries[i].Fields["Service"] = gcp.ExtractService(raw)
		}
		fieldNames = append([]string{"Service"}, fieldNames...)
	}

	// Add message field when --filter is used or there are just two columns (ts, service)
	if (queryFilter != "" || len(types) == 0 ) && !slices.Contains(fieldNames, "message") {
		fieldNames = append(fieldNames, "message")
	}

	// Select formatter
	var formatter format.Formatter
	switch {
	case queryRaw:
		formatter = &format.Raw{}
	case queryJSON:
		var fw io.Writer
		if verbose {
			fw = os.Stderr
		}
		formatter = &format.JSONL{FooterWriter: fw}
	default:
		formatter = &format.Columns{FooterWriter: os.Stdout}
	}

	return formatter.Format(os.Stdout, entries, fieldNames, totalCount)
}

// parseWhereFieldNames extracts field names from --where clauses.
// For example, ["taskId=\"abc\"", "runId=1"] returns ["taskId", "runId"].
func parseWhereFieldNames(wheres []string) []string {
	operators := []string{"!=", ">=", "<=", "=~", "!~", "=", ">", "<"}
	var names []string
	for _, w := range wheres {
		for _, op := range operators {
			if idx := strings.Index(w, op); idx > 0 {
				names = append(names, w[:idx])
				break
			}
		}
	}
	return names
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
		return fromTime, now.Truncate(time.Minute), nil
	}

	// Default: --since 1h
	// Truncate to minute so that repeated runs within the same minute
	// produce identical timestamps, allowing the results cache to hit.
	sinceStr := since
	if sinceStr == "" {
		sinceStr = "1h"
	}

	d, err := parseDuration(sinceStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parsing --since: %w", err)
	}

	snapped := now.Truncate(time.Minute)
	return snapped.Add(-d), snapped, nil
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

// filterNonEmptyFields returns only the fields that have at least one non-empty value across all entries.
func filterNonEmptyFields(entries []format.LogEntry, fieldNames []string) []string {
	var result []string
	for _, f := range fieldNames {
		for _, e := range entries {
			if e.Fields[f] != "" {
				result = append(result, f)
				break
			}
		}
	}
	return result
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
