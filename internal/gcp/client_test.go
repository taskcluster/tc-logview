package gcp

import (
	"testing"
	"time"

	"cloud.google.com/go/logging"
	monitoredres "google.golang.org/genproto/googleapis/api/monitoredres"
)

func TestEntryToMap_JsonPayload(t *testing.T) {
	entry := &logging.Entry{
		Timestamp: time.Date(2026, 3, 3, 12, 0, 0, 0, time.UTC),
		Severity:  logging.Notice,
		InsertID:  "abc123",
		Payload: map[string]interface{}{
			"workerPoolId": "proj/misc",
			"workerId":     "worker-1",
		},
		Labels:  map[string]string{"env": "production"},
		LogName: "projects/my-project/logs/worker-stopped",
		Resource: &monitoredres.MonitoredResource{
			Type:   "gce_instance",
			Labels: map[string]string{"zone": "us-central1-a"},
		},
	}

	m := entryToMap(entry)

	if m["insertId"] != "abc123" {
		t.Errorf("insertId: got %v", m["insertId"])
	}
	payload, ok := m["jsonPayload"].(map[string]interface{})
	if !ok {
		t.Fatalf("jsonPayload missing or wrong type, got: %v", m["jsonPayload"])
	}
	if payload["workerPoolId"] != "proj/misc" {
		t.Errorf("workerPoolId: got %v", payload["workerPoolId"])
	}
	if m["logName"] != "projects/my-project/logs/worker-stopped" {
		t.Errorf("logName: got %v", m["logName"])
	}
	labels, ok := m["labels"].(map[string]string)
	if !ok || labels["env"] != "production" {
		t.Errorf("labels: got %v", m["labels"])
	}
	resource, ok := m["resource"].(map[string]interface{})
	if !ok || resource["type"] != "gce_instance" {
		t.Errorf("resource: got %v", m["resource"])
	}
}

func TestEntryToMap_TextPayload(t *testing.T) {
	entry := &logging.Entry{
		Timestamp: time.Now(),
		Severity:  logging.Info,
		InsertID:  "txt1",
		Payload:   "hello log line",
	}
	m := entryToMap(entry)
	if m["textPayload"] != "hello log line" {
		t.Errorf("textPayload: got %v", m["textPayload"])
	}
	if _, ok := m["jsonPayload"]; ok {
		t.Error("jsonPayload should not be present for text payload")
	}
}

func TestEntryToMap_NilPayload(t *testing.T) {
	entry := &logging.Entry{
		Timestamp: time.Now(),
		Severity:  logging.Warning,
		InsertID:  "nil1",
		Payload:   nil,
	}
	m := entryToMap(entry)
	if _, ok := m["jsonPayload"]; ok {
		t.Error("jsonPayload should not be present for nil payload")
	}
	if _, ok := m["textPayload"]; ok {
		t.Error("textPayload should not be present for nil payload")
	}
	if m["insertId"] != "nil1" {
		t.Errorf("insertId: got %v", m["insertId"])
	}
}

func TestEntryToMap_TraceAndSpan(t *testing.T) {
	entry := &logging.Entry{
		Timestamp: time.Now(),
		Severity:  logging.Debug,
		InsertID:  "trace1",
		Trace:     "projects/p/traces/abc",
		SpanID:    "span123",
	}
	m := entryToMap(entry)
	if m["trace"] != "projects/p/traces/abc" {
		t.Errorf("trace: got %v", m["trace"])
	}
	if m["spanId"] != "span123" {
		t.Errorf("spanId: got %v", m["spanId"])
	}
}

func TestExtractFields_FlatJsonPayload(t *testing.T) {
	raw := map[string]interface{}{
		"timestamp": "2026-03-03T12:00:00Z",
		"jsonPayload": map[string]interface{}{
			"workerPoolId": "proj/misc",
			"workerId":     "worker-1",
		},
	}
	entry := ExtractFields(raw, []string{"workerPoolId", "workerId"})
	if entry.Fields["workerPoolId"] != "proj/misc" {
		t.Errorf("workerPoolId: got %q", entry.Fields["workerPoolId"])
	}
	if entry.Fields["workerId"] != "worker-1" {
		t.Errorf("workerId: got %q", entry.Fields["workerId"])
	}
}

func TestExtractFields_NestedJsonPayload(t *testing.T) {
	raw := map[string]interface{}{
		"timestamp": "2026-03-03T12:00:00Z",
		"jsonPayload": map[string]interface{}{
			"Fields": map[string]interface{}{
				"providerId":   "community-tc-workers-google",
				"workerPoolId": "proj-misc/misc-t-linux-large",
				"workerId":     "i-abc123",
			},
		},
	}
	entry := ExtractFields(raw, []string{"providerId", "workerPoolId", "workerId"})
	if entry.Fields["providerId"] != "community-tc-workers-google" {
		t.Errorf("providerId: got %q", entry.Fields["providerId"])
	}
	if entry.Fields["workerPoolId"] != "proj-misc/misc-t-linux-large" {
		t.Errorf("workerPoolId: got %q", entry.Fields["workerPoolId"])
	}
	if entry.Fields["workerId"] != "i-abc123" {
		t.Errorf("workerId: got %q", entry.Fields["workerId"])
	}
}

func TestExtractService_JsonPayload(t *testing.T) {
	raw := map[string]interface{}{
		"jsonPayload": map[string]interface{}{
			"serviceContext": map[string]interface{}{
				"service": "taskcluster-queue",
			},
		},
	}
	if got := ExtractService(raw); got != "taskcluster-queue" {
		t.Errorf("ExtractService jsonPayload: got %q, want %q", got, "taskcluster-queue")
	}
}

func TestExtractService_ProtoPayload(t *testing.T) {
	raw := map[string]interface{}{
		"protoPayload": map[string]interface{}{
			"serviceContext": map[string]interface{}{
				"service": "taskcluster-worker-manager",
			},
		},
	}
	if got := ExtractService(raw); got != "taskcluster-worker-manager" {
		t.Errorf("ExtractService protoPayload: got %q, want %q", got, "taskcluster-worker-manager")
	}
}

func TestExtractService_NoServiceContext(t *testing.T) {
	raw := map[string]interface{}{
		"jsonPayload": map[string]interface{}{
			"workerPoolId": "proj/misc",
		},
	}
	if got := ExtractService(raw); got != "" {
		t.Errorf("ExtractService no serviceContext: got %q, want empty", got)
	}
}

func TestExtractService_TextPayload(t *testing.T) {
	raw := map[string]interface{}{
		"textPayload": "hello log line",
	}
	if got := ExtractService(raw); got != "" {
		t.Errorf("ExtractService textPayload: got %q, want empty", got)
	}
}

func TestExtractFields_ProtoPayload(t *testing.T) {
	raw := map[string]interface{}{
		"timestamp": "2026-03-03T12:00:00Z",
		"protoPayload": map[string]interface{}{
			"Fields": map[string]interface{}{
				"providerId":   "community-tc-workers-google",
				"workerPoolId": "proj-misc/misc-t-linux-large",
				"workerId":     "i-def456",
				"workerGroup":  "us-central1-b",
			},
		},
	}
	entry := ExtractFields(raw, []string{"providerId", "workerPoolId", "workerId", "workerGroup"})
	if entry.Fields["providerId"] != "community-tc-workers-google" {
		t.Errorf("providerId: got %q", entry.Fields["providerId"])
	}
	if entry.Fields["workerPoolId"] != "proj-misc/misc-t-linux-large" {
		t.Errorf("workerPoolId: got %q", entry.Fields["workerPoolId"])
	}
	if entry.Fields["workerId"] != "i-def456" {
		t.Errorf("workerId: got %q", entry.Fields["workerId"])
	}
	if entry.Fields["workerGroup"] != "us-central1-b" {
		t.Errorf("workerGroup: got %q", entry.Fields["workerGroup"])
	}
}
