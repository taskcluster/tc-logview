package format

import (
	"encoding/json"
	"io"
)

// Raw formats log entries by pretty-printing each entry's Raw map as
// indented JSON, separated by newlines.
type Raw struct{}

// Format writes each entry's Raw map as pretty-printed JSON (2-space indent)
// to w, with a blank line between entries.
func (r *Raw) Format(w io.Writer, entries []LogEntry, fieldOrder []string, total int) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	for i, e := range entries {
		if i > 0 {
			if _, err := io.WriteString(w, "\n"); err != nil {
				return err
			}
		}
		if err := enc.Encode(e.Raw); err != nil {
			return err
		}
	}

	return nil
}
