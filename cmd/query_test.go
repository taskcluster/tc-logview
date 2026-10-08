package cmd

import (
	"slices"
	"testing"

	"github.com/spf13/pflag"
)

// TestQueryWhereFlagVerbatim guards against --where being CSV-parsed (as a
// StringSlice would): quoted values, unquoted values and values containing
// commas must all be passed through verbatim, one entry per flag.
func TestQueryWhereFlagVerbatim(t *testing.T) {
	flag := queryCmd.Flags().Lookup("where")
	prev := slices.Clone(queryWhere)
	t.Cleanup(func() {
		_ = flag.Value.(pflag.SliceValue).Replace(prev)
		flag.Changed = false
	})

	args := []string{
		`--where`, `name="gcpCredentials"`,
		`--where`, `runId=1`,
		`--where`, `reason="a,b"`,
		`--where`, `pod=x,y`,
	}
	if err := queryCmd.Flags().Parse(args); err != nil {
		t.Fatalf("parsing --where flags: %v", err)
	}
	want := []string{`name="gcpCredentials"`, `runId=1`, `reason="a,b"`, `pod=x,y`}
	if !slices.Equal(queryWhere, want) {
		t.Errorf("queryWhere = %q, want %q", queryWhere, want)
	}
}

func TestParseWhereFieldNames(t *testing.T) {
	got := parseWhereFieldNames([]string{`taskId="abc"`, `runId>=1`, `name="a,b"`})
	want := []string{"taskId", "runId", "name"}
	if !slices.Equal(got, want) {
		t.Errorf("parseWhereFieldNames = %q, want %q", got, want)
	}
}
