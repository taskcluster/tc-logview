package presets

import (
	"sort"
	"testing"
)

func TestLookup_Found(t *testing.T) {
	p := Lookup("k8s.pod-crash")
	if p == nil {
		t.Fatal("expected preset for k8s.pod-crash, got nil")
	}
	if p.Service != "k8s" {
		t.Errorf("expected service k8s, got %q", p.Service)
	}
	if p.Filter == "" {
		t.Error("expected non-empty filter")
	}
}

func TestLookup_NotFound(t *testing.T) {
	p := Lookup("nonexistent.type")
	if p != nil {
		t.Errorf("expected nil for unknown preset, got %+v", p)
	}
}

func TestLookup_AllPresetsHaveService(t *testing.T) {
	for _, p := range All {
		if p.Service == "" {
			t.Errorf("preset %q has empty service", p.Name)
		}
		if p.Filter == "" {
			t.Errorf("preset %q has empty filter", p.Name)
		}
		if p.Description == "" {
			t.Errorf("preset %q has empty description", p.Name)
		}
	}
}

func TestLookup_FieldMappings(t *testing.T) {
	p := Lookup("k8s.pod-crash")
	if p == nil {
		t.Fatal("expected preset for k8s.pod-crash")
	}
	if _, ok := p.Fields["pod"]; !ok {
		t.Error("k8s.pod-crash should have 'pod' field mapping")
	}
	if _, ok := p.Fields["namespace"]; !ok {
		t.Error("k8s.pod-crash should have 'namespace' field mapping")
	}
}

func TestAll_Sorted(t *testing.T) {
	names := make([]string, len(All))
	for i, p := range All {
		names[i] = p.Name
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("All presets should be sorted by name, got: %v", names)
	}
}

func TestFieldNames_ReturnsKeys(t *testing.T) {
	p := Lookup("k8s.events")
	if p == nil {
		t.Fatal("expected preset for k8s.events")
	}
	names := p.FieldNames()
	if len(names) == 0 {
		t.Error("k8s.events should have field names")
	}
}
