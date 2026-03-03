package filter

import (
	"strings"
	"testing"
)

func TestBuild_ClusterOnly(t *testing.T) {
	got, err := Build(Params{Cluster: "my-cluster"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `resource.labels.cluster_name="my-cluster"`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuild_EmptyCluster(t *testing.T) {
	_, err := Build(Params{})
	if err == nil {
		t.Fatal("expected error for empty cluster, got nil")
	}
}

func TestBuild_ClusterTypeService(t *testing.T) {
	got, err := Build(Params{
		Cluster: "my-cluster",
		LogType: "worker-started",
		Service: "worker-manager",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `resource.labels.cluster_name="my-cluster" AND jsonPayload.Type="worker-started" AND jsonPayload.serviceContext.service="worker-manager"`
	if got != want {
		t.Errorf("got:\n  %s\nwant:\n  %s", got, want)
	}
}

func TestBuild_WhereExpansion(t *testing.T) {
	got, err := Build(Params{
		Cluster:    "c",
		LogType:    "worker-stopped",
		Where:      []string{`workerPoolId="proj-misc"`},
		FieldNames: []string{"workerPoolId", "workerId"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `jsonPayload.Fields.workerPoolId="proj-misc"`) {
		t.Errorf("filter should contain expanded where clause, got: %s", got)
	}
}

func TestBuild_WhereUnknownField(t *testing.T) {
	_, err := Build(Params{
		Cluster:    "c",
		Where:      []string{`badField="x"`},
		FieldNames: []string{"goodField"},
	})
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("error should mention 'unknown field', got: %v", err)
	}
	if !strings.Contains(err.Error(), "goodField") {
		t.Errorf("error should list valid fields, got: %v", err)
	}
}

func TestBuild_WhereDifferentOperators(t *testing.T) {
	tests := []struct {
		where    string
		wantPart string
	}{
		{`count!=0`, `jsonPayload.Fields.count!=0`},
		{`retries>=3`, `jsonPayload.Fields.retries>=3`},
		{`name=~"test.*"`, `jsonPayload.Fields.name=~"test.*"`},
	}

	fields := []string{"count", "retries", "name"}

	for _, tt := range tests {
		got, err := Build(Params{
			Cluster:    "c",
			Where:      []string{tt.where},
			FieldNames: fields,
		})
		if err != nil {
			t.Errorf("where %q: unexpected error: %v", tt.where, err)
			continue
		}
		if !strings.Contains(got, tt.wantPart) {
			t.Errorf("where %q: expected filter to contain %q, got: %s", tt.where, tt.wantPart, got)
		}
	}
}

func TestBuild_RawFilter(t *testing.T) {
	got, err := Build(Params{
		Cluster:   "c",
		RawFilter: `severity="ERROR"`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `(severity="ERROR")`) {
		t.Errorf("filter should contain raw filter in parens, got: %s", got)
	}
}

func TestBuild_FullCombo(t *testing.T) {
	got, err := Build(Params{
		Cluster:    "prod-cluster",
		LogType:    "task-resolved",
		Service:    "queue",
		Where:      []string{`workerPoolId="proj-misc/generic"`},
		RawFilter:  `severity="WARNING"`,
		FieldNames: []string{"workerPoolId", "taskId"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{
		`resource.labels.cluster_name="prod-cluster"`,
		`jsonPayload.Type="task-resolved"`,
		`jsonPayload.serviceContext.service="queue"`,
		`jsonPayload.Fields.workerPoolId="proj-misc/generic"`,
		`(severity="WARNING")`,
	}
	for _, part := range expected {
		if !strings.Contains(got, part) {
			t.Errorf("full combo filter missing %q, got:\n  %s", part, got)
		}
	}

	// Verify parts are AND-joined
	parts := strings.Split(got, " AND ")
	if len(parts) != 5 {
		t.Errorf("expected 5 AND-joined parts, got %d: %s", len(parts), got)
	}
}
