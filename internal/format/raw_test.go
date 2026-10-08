package format

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRaw_OneCompactObjectPerLine(t *testing.T) {
	entries := []LogEntry{
		{Raw: map[string]interface{}{
			"timestamp":   "2026-03-10T12:00:00Z",
			"jsonPayload": map[string]interface{}{"reason": "FailedPreStopHook", "msg": "a\nb <x>"},
		}},
		{Raw: map[string]interface{}{"timestamp": "2026-03-10T12:00:01Z", "textPayload": "hello"}},
	}

	var buf bytes.Buffer
	if err := (&Raw{}).Format(&buf, entries, nil, 2); err != nil {
		t.Fatalf("Format: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d:\n%s", len(lines), buf.String())
	}
	for i, line := range lines {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("line %d is not a JSON object: %v\n%s", i, err, line)
		}
		if strings.HasPrefix(line, " ") || strings.Contains(line, "\n") {
			t.Errorf("line %d is not compact: %q", i, line)
		}
	}
	if !strings.Contains(lines[0], "<x>") {
		t.Errorf("HTML characters should not be escaped, got: %s", lines[0])
	}
}

func TestRaw_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := (&Raw{}).Format(&buf, nil, nil, 0); err != nil {
		t.Fatalf("Format: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output, got %q", buf.String())
	}
}
