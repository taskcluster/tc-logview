package references

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type IndexEntry struct {
	Service string
	LogType LogType
	Unique  bool // true if this type exists in only one service
}

type Index struct {
	entries map[string][]IndexEntry // keyed by LogType.Type
}

// LoadIndex reads all *.json files from cacheDir, parses each as a
// ServiceReference, and builds an index of log types. Types that appear in
// exactly one service are marked as Unique.
func LoadIndex(cacheDir string) (*Index, error) {
	idx := &Index{
		entries: make(map[string][]IndexEntry),
	}

	files, err := filepath.Glob(filepath.Join(cacheDir, "*.json"))
	if err != nil {
		return nil, err
	}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}

		var ref ServiceReference
		if err := json.Unmarshal(data, &ref); err != nil {
			return nil, err
		}

		for _, lt := range ref.Types {
			idx.entries[lt.Type] = append(idx.entries[lt.Type], IndexEntry{
				Service: ref.ServiceName,
				LogType: lt,
			})
		}
	}

	// Determine uniqueness: a type is unique if it appears in exactly one service.
	for typeName, entries := range idx.entries {
		services := map[string]bool{}
		for _, e := range entries {
			services[e.Service] = true
		}
		unique := len(services) == 1
		for i := range entries {
			idx.entries[typeName][i].Unique = unique
		}
	}

	return idx, nil
}

// Lookup returns the index entries for a given type name.
func (idx *Index) Lookup(typeName string) ([]IndexEntry, bool) {
	entries, ok := idx.entries[typeName]
	return entries, ok
}

// All returns all index entries sorted by service name, then type name.
func (idx *Index) All() []IndexEntry {
	var all []IndexEntry
	for _, entries := range idx.entries {
		all = append(all, entries...)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Service != all[j].Service {
			return all[i].Service < all[j].Service
		}
		return all[i].LogType.Type < all[j].LogType.Type
	})
	return all
}

// IsUnique returns true if the given type exists in exactly one service.
func (idx *Index) IsUnique(typeName string) bool {
	entries, ok := idx.entries[typeName]
	if !ok {
		return false
	}
	services := map[string]bool{}
	for _, e := range entries {
		services[e.Service] = true
	}
	return len(services) == 1
}

// ServiceFor returns the service name if the type is unique to one service,
// or an empty string if the type is shared across multiple services or not found.
func (idx *Index) ServiceFor(typeName string) string {
	entries, ok := idx.entries[typeName]
	if !ok {
		return ""
	}
	services := map[string]bool{}
	for _, e := range entries {
		services[e.Service] = true
	}
	if len(services) != 1 {
		return ""
	}
	return entries[0].Service
}

// FieldNames returns the sorted field names for a type, taken from the first
// entry found. Returns nil if the type is not in the index.
func (idx *Index) FieldNames(typeName string) []string {
	entries, ok := idx.entries[typeName]
	if !ok || len(entries) == 0 {
		return nil
	}
	fields := entries[0].LogType.Fields
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsEmpty returns true if the index contains no entries.
func (idx *Index) IsEmpty() bool {
	return len(idx.entries) == 0
}

// String returns a human-readable summary of the index for debugging.
func (idx *Index) String() string {
	if idx.IsEmpty() {
		return "Index: (empty)"
	}
	var b strings.Builder
	b.WriteString("Index:\n")
	for _, e := range idx.All() {
		unique := ""
		if e.Unique {
			unique = " (unique)"
		}
		b.WriteString("  " + e.Service + "/" + e.LogType.Type + unique + "\n")
	}
	return b.String()
}
