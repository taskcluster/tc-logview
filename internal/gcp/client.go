package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/logging"
	"cloud.google.com/go/logging/logadmin"
	"github.com/tidwall/gjson"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/taskcluster/tc-logview/internal/format"
)

// Client wraps a GCP logadmin client for querying Cloud Logging.
type Client struct {
	project     string
	adminClient *logadmin.Client
}

// QueryResult holds the entries returned from a GCP log query.
type QueryResult struct {
	Entries []map[string]interface{}
	Total   int
}

// NewClient creates a new GCP logging client authenticated with the given
// credentials file.
func NewClient(ctx context.Context, projectID, keyPath string) (*Client, error) {
	adminClient, err := logadmin.NewClient(ctx, projectID, option.WithCredentialsFile(keyPath))
	if err != nil {
		return nil, fmt.Errorf("failed to create logadmin client: %w", err)
	}
	return &Client{
		project:     projectID,
		adminClient: adminClient,
	}, nil
}

// Query executes a GCP Cloud Logging filter query and returns up to limit
// entries, newest first (reversed before display).
func (c *Client) Query(ctx context.Context, filter string, limit int) (*QueryResult, error) {
	it := c.adminClient.Entries(ctx, logadmin.Filter(filter), logadmin.NewestFirst())

	result := &QueryResult{}
	for i := 0; i < limit; i++ {
		entry, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating log entries: %w", err)
		}

		result.Entries = append(result.Entries, entryToMap(entry))
	}

	result.Total = len(result.Entries)
	return result, nil
}

// Close closes the underlying logadmin client.
func (c *Client) Close() error {
	return c.adminClient.Close()
}

// entryToMap converts a GCP logging Entry into a raw map capturing all
// relevant fields: timestamp, severity, insertId, logName, labels, resource,
// trace, spanId, and the payload (json/text/proto).
func entryToMap(entry *logging.Entry) map[string]interface{} {
	m := make(map[string]interface{})
	m["timestamp"] = entry.Timestamp.Format(time.RFC3339)
	m["severity"] = entry.Severity.String()
	m["insertId"] = entry.InsertID

	if entry.LogName != "" {
		m["logName"] = entry.LogName
	}
	if len(entry.Labels) > 0 {
		m["labels"] = entry.Labels
	}
	if entry.Resource != nil {
		res := map[string]interface{}{"type": entry.Resource.Type}
		if len(entry.Resource.Labels) > 0 {
			res["labels"] = entry.Resource.Labels
		}
		m["resource"] = res
	}
	if entry.Trace != "" {
		m["trace"] = entry.Trace
	}
	if entry.SpanID != "" {
		m["spanId"] = entry.SpanID
	}

	switch p := entry.Payload.(type) {
	case map[string]interface{}:
		m["jsonPayload"] = p
	case string:
		m["textPayload"] = p
	case proto.Message:
		b, err := protojson.Marshal(p)
		if err == nil {
			var pm map[string]interface{}
			if json.Unmarshal(b, &pm) == nil {
				m["protoPayload"] = pm
			}
		}
	}

	return m
}

// ExtractFields extracts the timestamp and the named fields from a raw log
// entry map using gjson path lookups and returns a format.LogEntry.
func ExtractFields(raw map[string]interface{}, fieldNames []string) format.LogEntry {
	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return format.LogEntry{
			Timestamp: "",
			Fields:    make(map[string]string),
			Raw:       raw,
		}
	}

	ts := gjson.GetBytes(jsonBytes, "timestamp").String()

	fields := make(map[string]string, len(fieldNames))
	for _, name := range fieldNames {
		val := gjson.GetBytes(jsonBytes, "jsonPayload.Fields."+name)
		if !val.Exists() {
			val = gjson.GetBytes(jsonBytes, "protoPayload.Fields."+name)
		}
		if !val.Exists() {
			val = gjson.GetBytes(jsonBytes, "jsonPayload."+name)
		}
		if !val.Exists() {
			val = gjson.GetBytes(jsonBytes, "protoPayload."+name)
		}
		fields[name] = val.String()
	}

	return format.LogEntry{
		Timestamp: ts,
		Fields:    fields,
		Raw:       raw,
	}
}

// ExtractFieldsWithPaths extracts fields using an optional path mapping.
// When pathMap provides a GCP path for a field name (e.g. "node" → "resource.labels.node_name"),
// that path is used directly. Fields not in the map fall back to the standard lookup chain.
func ExtractFieldsWithPaths(raw map[string]interface{}, fieldNames []string, pathMap map[string]string) format.LogEntry {
	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return format.LogEntry{Timestamp: "", Fields: make(map[string]string), Raw: raw}
	}

	ts := gjson.GetBytes(jsonBytes, "timestamp").String()

	fields := make(map[string]string, len(fieldNames))
	for _, name := range fieldNames {
		// Try mapped path first
		if pathMap != nil {
			if gcpPath, ok := pathMap[name]; ok {
				if v := resolvePathField(jsonBytes, gcpPath); v != "" {
					fields[name] = v
					continue
				}
			}
		}
		// Fall back to standard lookup
		val := gjson.GetBytes(jsonBytes, "jsonPayload.Fields."+name)
		if !val.Exists() {
			val = gjson.GetBytes(jsonBytes, "protoPayload.Fields."+name)
		}
		if !val.Exists() {
			val = gjson.GetBytes(jsonBytes, "jsonPayload."+name)
		}
		if !val.Exists() {
			val = gjson.GetBytes(jsonBytes, "protoPayload."+name)
		}
		fields[name] = val.String()
	}

	return format.LogEntry{Timestamp: ts, Fields: fields, Raw: raw}
}

// resolvePathField resolves a GCP path from a preset's field mapping.
// For dotted paths (e.g. "resource.labels.node_name") it tries the path directly.
// For bare names (e.g. "MESSAGE") it probes jsonPayload and protoPayload.
func resolvePathField(jsonBytes []byte, gcpPath string) string {
	val := gjson.GetBytes(jsonBytes, gcpPath)
	if val.Exists() {
		return val.String()
	}
	if !strings.Contains(gcpPath, ".") {
		for _, prefix := range []string{"jsonPayload", "protoPayload"} {
			val = gjson.GetBytes(jsonBytes, prefix+"."+gcpPath)
			if val.Exists() {
				return val.String()
			}
		}
	}
	return ""
}

// ExtractService returns the service name from a raw GCP log entry by
// checking serviceContext.service across all payload types (json, proto, text).
func ExtractService(raw map[string]interface{}) string {
	for _, key := range []string{"jsonPayload", "protoPayload"} {
		payload, ok := raw[key].(map[string]interface{})
		if !ok {
			continue
		}
		sc, ok := payload["serviceContext"].(map[string]interface{})
		if !ok {
			continue
		}
		if s, ok := sc["service"].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// ExtractType returns the log type (jsonPayload.Type) from a raw GCP log entry.
func ExtractType(raw map[string]interface{}) string {
	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	val := gjson.GetBytes(jsonBytes, "jsonPayload.Type")
	if !val.Exists() {
		val = gjson.GetBytes(jsonBytes, "protoPayload.Type")
	}
	return val.String()
}

// EntriesFromJSON unmarshals a JSON array of log entries, typically loaded
// from a cache file.
func EntriesFromJSON(data []byte) ([]map[string]interface{}, error) {
	var entries []map[string]interface{}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("failed to unmarshal log entries from JSON: %w", err)
	}
	return entries, nil
}
