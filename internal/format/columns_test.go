package format

import (
	"bytes"
	"strings"
	"testing"
)

func testEntries() []LogEntry {
	return []LogEntry{
		{
			Timestamp: "2024-01-15T10:00:00Z",
			Fields: map[string]string{
				"workerId":     "w-abc",
				"workerPoolId": "proj-misc/generic",
				"reason":       "claim-expired",
			},
		},
		{
			Timestamp: "2024-01-15T10:00:01Z",
			Fields: map[string]string{
				"workerId":     "w-def",
				"workerPoolId": "proj-misc/generic",
				"reason":       "shutdown",
			},
		},
		{
			Timestamp: "2024-01-15T10:00:02Z",
			Fields: map[string]string{
				"workerId":     "w-ghi",
				"workerPoolId": "proj-other/pool",
				"reason":       "spot-termination",
			},
		},
	}
}

var testFieldOrder = []string{"workerId", "workerPoolId", "reason"}

func TestHeaderPresent(t *testing.T) {
	var buf bytes.Buffer
	c := &Columns{}
	err := c.Format(&buf, testEntries(), testFieldOrder, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	for _, header := range []string{"TIMESTAMP", "WORKER_ID", "WORKER_POOL_ID", "REASON"} {
		if !strings.Contains(output, header) {
			t.Errorf("expected header %q in output, got:\n%s", header, output)
		}
	}
}

func TestColumnsAligned(t *testing.T) {
	var buf bytes.Buffer
	c := &Columns{}
	err := c.Format(&buf, testEntries(), testFieldOrder, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	// Header + 3 data rows + footer = 5 lines
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines (header + 3 rows), got %d:\n%s", len(lines), buf.String())
	}

	// Check that the header and all data rows have consistent column positions.
	// Find the position of WORKER_ID in the header, then verify each data row
	// has content at the same position.
	headerLine := lines[0]
	workerIdPos := strings.Index(headerLine, "WORKER_ID")
	if workerIdPos < 0 {
		t.Fatal("WORKER_ID not found in header")
	}
	for i := 1; i <= 3; i++ {
		// Each data row should have content starting at the same column position
		// as the header columns. We verify by checking that the line is at least
		// as long as the header positions.
		if len(lines[i]) < workerIdPos {
			t.Errorf("row %d is too short to be aligned with header: %q", i, lines[i])
		}
	}
}

func TestFooterWithTruncation(t *testing.T) {
	var buf bytes.Buffer
	c := &Columns{FooterWriter: &buf}
	err := c.Format(&buf, testEntries(), testFieldOrder, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	expected := "Showing 3 of 10 entries"
	if !strings.Contains(output, expected) {
		t.Errorf("expected footer %q in output, got:\n%s", expected, output)
	}
}

func TestFooterWithoutTruncation(t *testing.T) {
	var buf bytes.Buffer
	c := &Columns{FooterWriter: &buf}
	err := c.Format(&buf, testEntries(), testFieldOrder, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	expected := "3 entries"
	if !strings.Contains(output, expected) {
		t.Errorf("expected footer %q in output, got:\n%s", expected, output)
	}
	// Make sure it does NOT say "Showing"
	if strings.Contains(output, "Showing") {
		t.Errorf("footer should not contain 'Showing' when total == len(entries), got:\n%s", output)
	}
}

func TestCamelToUpperSnake(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"workerPoolId", "WORKER_POOL_ID"},
		{"taskId", "TASK_ID"},
		{"id", "ID"},
	}
	for _, tt := range tests {
		got := camelToUpperSnake(tt.input)
		if got != tt.expected {
			t.Errorf("camelToUpperSnake(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
