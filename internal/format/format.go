package format

import "io"

type LogEntry struct {
	Timestamp string
	Fields    map[string]string      // field name -> stringified value
	Raw       map[string]interface{} // full original entry
}

type Formatter interface {
	Format(w io.Writer, entries []LogEntry, fieldOrder []string, total int) error
}
