package format

import (
	"encoding/json"
	"io"
)

// Raw formats log entries as JSON Lines: each entry's full Raw map is written
// as one compact JSON object per line, suitable for piping to jq or other
// line-based tools.
type Raw struct{}

// Format writes each entry's Raw map as a single-line JSON object to w.
func (r *Raw) Format(w io.Writer, entries []LogEntry, fieldOrder []string, total int) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	for _, e := range entries {
		if err := enc.Encode(e.Raw); err != nil {
			return err
		}
	}

	return nil
}
