package format

import (
	"encoding/json"
	"fmt"
	"io"
)

// JSONL formats log entries as JSON Lines (one JSON object per line).
type JSONL struct {
	FooterWriter io.Writer // if nil, footer is suppressed
}

// Format writes entries as JSONL to w. Each line contains only the timestamp
// (as "ts") and the fields specified by fieldOrder.
func (j *JSONL) Format(w io.Writer, entries []LogEntry, fieldOrder []string, total int) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	for _, e := range entries {
		obj := make(map[string]string, 1+len(fieldOrder))
		obj["ts"] = e.Timestamp
		for _, f := range fieldOrder {
			obj[f] = e.Fields[f]
		}
		if err := enc.Encode(obj); err != nil {
			return err
		}
	}

	// Print footer.
	if j.FooterWriter == nil {
		return nil
	}
	count := len(entries)
	if total > count {
		_, err := fmt.Fprintf(j.FooterWriter, "Showing %d of %d entries\n", count, total)
		return err
	}
	_, err := fmt.Fprintf(j.FooterWriter, "%d entries\n", count)
	return err
}
