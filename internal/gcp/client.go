package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cloud.google.com/go/logging/logadmin"
	"github.com/tidwall/gjson"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/lotas/tc-logview/internal/format"
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
// entries, newest first.
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

		m := make(map[string]interface{})
		m["timestamp"] = entry.Timestamp.Format(time.RFC3339)
		m["severity"] = entry.Severity.String()
		m["insertId"] = entry.InsertID

		switch p := entry.Payload.(type) {
		case map[string]interface{}:
			m["jsonPayload"] = p
		case string:
			m["textPayload"] = p
		}

		result.Entries = append(result.Entries, m)
	}

	result.Total = len(result.Entries)
	return result, nil
}

// Close closes the underlying logadmin client.
func (c *Client) Close() error {
	return c.adminClient.Close()
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
		fields[name] = val.String()
	}

	return format.LogEntry{
		Timestamp: ts,
		Fields:    fields,
		Raw:       raw,
	}
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
