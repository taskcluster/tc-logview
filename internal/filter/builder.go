package filter

import (
	"fmt"
	"strings"
)

// Params holds the inputs for building a GCP log filter string.
type Params struct {
	Cluster    string   // from env config — resource.labels.cluster_name
	LogType    string   // --type flag — jsonPayload.Type
	Service    string   // --service flag or auto-detected — jsonPayload.serviceContext.service
	Where      []string // --where flags, field shorthand like: workerPoolId="proj-misc/generic"
	RawFilter  string   // --filter flag, raw GCP filter expression
	FieldNames []string // known fields for the log type (from references index), used to validate --where
}

// operators lists supported comparison operators, longest first so that
// two-character operators are matched before their single-character prefixes.
var operators = []string{"!=", ">=", "<=", "=~", "!~", "=", ">", "<"}

// Build constructs a GCP Cloud Logging filter string from the given Params.
// It returns an error if required fields are missing or a --where entry
// references an unknown field name.
func Build(p Params) (string, error) {
	if p.Cluster == "" {
		return "", fmt.Errorf("cluster is required")
	}

	var parts []string

	parts = append(parts, fmt.Sprintf("resource.labels.cluster_name=%q", p.Cluster))

	if p.LogType != "" {
		parts = append(parts, fmt.Sprintf("jsonPayload.Type=%q", p.LogType))
	}

	if p.Service != "" {
		parts = append(parts, fmt.Sprintf("jsonPayload.serviceContext.service=%q", p.Service))
	}

	for _, w := range p.Where {
		fieldName, op, value, err := parseWhere(w)
		if err != nil {
			return "", err
		}
		if !isValidField(fieldName, p.FieldNames) {
			return "", fmt.Errorf(
				"unknown field %q for type, valid fields: [%s]",
				fieldName,
				strings.Join(p.FieldNames, ", "),
			)
		}
		parts = append(parts, fmt.Sprintf("jsonPayload.Fields.%s%s%s", fieldName, op, value))
	}

	if p.RawFilter != "" {
		parts = append(parts, fmt.Sprintf("(%s)", p.RawFilter))
	}

	return strings.Join(parts, " AND "), nil
}

// parseWhere splits a where clause like `workerPoolId="proj-misc"` into
// its field name, operator, and value components.
func parseWhere(expr string) (field, op, value string, err error) {
	for _, candidate := range operators {
		idx := strings.Index(expr, candidate)
		if idx > 0 {
			return expr[:idx], candidate, expr[idx+len(candidate):], nil
		}
	}
	return "", "", "", fmt.Errorf("no operator found in where clause %q", expr)
}

// isValidField checks whether fieldName appears in the list of known fields.
func isValidField(fieldName string, known []string) bool {
	for _, f := range known {
		if f == fieldName {
			return true
		}
	}
	return false
}
