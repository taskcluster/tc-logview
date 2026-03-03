package format

import (
	"fmt"
	"io"
	"strings"
	"unicode"
)

// Columns formats log entries as aligned text columns.
type Columns struct{}

// camelToUpperSnake converts a camelCase string to UPPER_SNAKE_CASE.
// For example, "workerPoolId" becomes "WORKER_POOL_ID".
func camelToUpperSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}

// Format writes entries as aligned columns to w.
func (c *Columns) Format(w io.Writer, entries []LogEntry, fieldOrder []string, total int) error {
	// Build the list of columns: TIMESTAMP first, then fieldOrder.
	columns := make([]string, 0, 1+len(fieldOrder))
	columns = append(columns, "TIMESTAMP")
	for _, f := range fieldOrder {
		columns = append(columns, camelToUpperSnake(f))
	}

	// Build rows as string slices. Each row has len(columns) cells.
	rows := make([][]string, len(entries))
	for i, e := range entries {
		row := make([]string, len(columns))
		row[0] = e.Timestamp
		for j, f := range fieldOrder {
			row[j+1] = e.Fields[f]
		}
		rows[i] = row
	}

	// Calculate max width for each column by scanning header + all rows.
	widths := make([]int, len(columns))
	for i, col := range columns {
		widths[i] = len(col)
	}
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Print header.
	if err := printRow(w, columns, widths); err != nil {
		return err
	}

	// Print data rows.
	for _, row := range rows {
		if err := printRow(w, row, widths); err != nil {
			return err
		}
	}

	// Print footer.
	count := len(entries)
	if total > count {
		_, err := fmt.Fprintf(w, "Showing %d of %d entries\n", count, total)
		return err
	}
	_, err := fmt.Fprintf(w, "%d entries\n", count)
	return err
}

// printRow writes a single row with columns padded to the given widths,
// separated by at least 2 spaces.
func printRow(w io.Writer, cells []string, widths []int) error {
	var b strings.Builder
	for i, cell := range cells {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(cell)
		// Pad with spaces up to the column width (but not after the last column).
		if i < len(cells)-1 {
			for pad := len(cell); pad < widths[i]; pad++ {
				b.WriteByte(' ')
			}
		}
	}
	b.WriteByte('\n')
	_, err := io.WriteString(w, b.String())
	return err
}
