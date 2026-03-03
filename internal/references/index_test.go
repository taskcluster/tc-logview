package references

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeMockData(t *testing.T, dir string) {
	t.Helper()

	queue := ServiceReference{
		ServiceName: "queue",
		Types: []LogType{
			{
				Name:        "api-method",
				Type:        "monitor.apiMethod",
				Title:       "API Method",
				Description: "An API method call",
				Level:       "info",
				Version:     1,
				Fields: map[string]string{
					"method":     "The HTTP method",
					"statusCode": "The response status code",
					"duration":   "The request duration in ms",
				},
			},
			{
				Name:        "task-claimed",
				Type:        "task-claimed",
				Title:       "Task Claimed",
				Description: "A task was claimed by a worker",
				Level:       "info",
				Version:     1,
				Fields: map[string]string{
					"taskId": "The task ID",
					"runId":  "The run ID",
				},
			},
		},
	}

	workerManager := ServiceReference{
		ServiceName: "worker-manager",
		Types: []LogType{
			{
				Name:        "api-method",
				Type:        "monitor.apiMethod",
				Title:       "API Method",
				Description: "An API method call",
				Level:       "info",
				Version:     1,
				Fields: map[string]string{
					"method":     "The HTTP method",
					"statusCode": "The response status code",
					"duration":   "The request duration in ms",
				},
			},
			{
				Name:        "worker-stopped",
				Type:        "worker-stopped",
				Title:       "Worker Stopped",
				Description: "A worker was stopped",
				Level:       "notice",
				Version:     1,
				Fields: map[string]string{
					"workerId":     "The worker ID",
					"workerPoolId": "The worker pool ID",
					"reason":       "The reason for stopping",
				},
			},
		},
	}

	writeJSON(t, filepath.Join(dir, "queue.json"), queue)
	writeJSON(t, filepath.Join(dir, "worker-manager.json"), workerManager)
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshalling JSON: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func TestLoadIndex(t *testing.T) {
	dir := t.TempDir()
	writeMockData(t, dir)

	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if idx.IsEmpty() {
		t.Fatal("expected non-empty index")
	}

	// Should have entries for 3 distinct type names.
	all := idx.All()
	// queue has 2 types, worker-manager has 2 types = 4 entries total
	// (monitor.apiMethod appears twice, task-claimed once, worker-stopped once)
	if len(all) != 4 {
		t.Fatalf("expected 4 total entries, got %d", len(all))
	}
}

func TestMonitorApiMethodNotUnique(t *testing.T) {
	dir := t.TempDir()
	writeMockData(t, dir)

	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if idx.IsUnique("monitor.apiMethod") {
		t.Error("monitor.apiMethod should NOT be unique (exists in both services)")
	}

	entries, ok := idx.Lookup("monitor.apiMethod")
	if !ok {
		t.Fatal("monitor.apiMethod not found in index")
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for monitor.apiMethod, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Unique {
			t.Errorf("entry for %s in service %s should not be marked unique", e.LogType.Type, e.Service)
		}
	}

	svc := idx.ServiceFor("monitor.apiMethod")
	if svc != "" {
		t.Errorf("ServiceFor(monitor.apiMethod) should be empty, got %q", svc)
	}
}

func TestTaskClaimedUnique(t *testing.T) {
	dir := t.TempDir()
	writeMockData(t, dir)

	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if !idx.IsUnique("task-claimed") {
		t.Error("task-claimed should be unique")
	}

	svc := idx.ServiceFor("task-claimed")
	if svc != "queue" {
		t.Errorf("ServiceFor(task-claimed) = %q, want %q", svc, "queue")
	}
}

func TestWorkerStoppedUnique(t *testing.T) {
	dir := t.TempDir()
	writeMockData(t, dir)

	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if !idx.IsUnique("worker-stopped") {
		t.Error("worker-stopped should be unique")
	}

	svc := idx.ServiceFor("worker-stopped")
	if svc != "worker-manager" {
		t.Errorf("ServiceFor(worker-stopped) = %q, want %q", svc, "worker-manager")
	}
}

func TestFieldNames(t *testing.T) {
	dir := t.TempDir()
	writeMockData(t, dir)

	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	fields := idx.FieldNames("task-claimed")
	expected := []string{"runId", "taskId"}
	if len(fields) != len(expected) {
		t.Fatalf("FieldNames(task-claimed) = %v, want %v", fields, expected)
	}
	for i, f := range fields {
		if f != expected[i] {
			t.Errorf("FieldNames(task-claimed)[%d] = %q, want %q", i, f, expected[i])
		}
	}
}

func TestAllSorted(t *testing.T) {
	dir := t.TempDir()
	writeMockData(t, dir)

	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	all := idx.All()
	// Expected order (sorted by service, then type):
	// 1. queue / monitor.apiMethod
	// 2. queue / task-claimed
	// 3. worker-manager / monitor.apiMethod
	// 4. worker-manager / worker-stopped
	expected := []struct {
		service  string
		typeName string
	}{
		{"queue", "monitor.apiMethod"},
		{"queue", "task-claimed"},
		{"worker-manager", "monitor.apiMethod"},
		{"worker-manager", "worker-stopped"},
	}

	if len(all) != len(expected) {
		t.Fatalf("All() returned %d entries, want %d", len(all), len(expected))
	}

	for i, e := range expected {
		if all[i].Service != e.service || all[i].LogType.Type != e.typeName {
			t.Errorf("All()[%d] = {%s, %s}, want {%s, %s}",
				i, all[i].Service, all[i].LogType.Type, e.service, e.typeName)
		}
	}
}

func TestIsEmptyOnFreshIndex(t *testing.T) {
	dir := t.TempDir()
	// No JSON files written — should produce an empty index.
	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	if !idx.IsEmpty() {
		t.Error("expected empty index for directory with no JSON files")
	}
}
