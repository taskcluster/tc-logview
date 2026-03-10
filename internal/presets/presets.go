package presets

import "sort"

// Preset defines a curated GCP Cloud Logging query for infrastructure events.
type Preset struct {
	Name        string            // e.g. "k8s.pod-crash"
	Service     string            // e.g. "k8s" — derived from prefix
	Description string            // human-readable description
	Filter      string            // GCP filter fragment (no cluster/time)
	Fields      map[string]string // shorthand → GCP path for --where
}

// FieldNames returns sorted field shorthand names for this preset.
func (p *Preset) FieldNames() []string {
	names := make([]string, 0, len(p.Fields))
	for k := range p.Fields {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Common field mappings reused across presets.
var podFields = map[string]string{
	"pod":       "jsonPayload.involvedObject.name",
	"namespace": "resource.labels.namespace_name",
}

var nodeFields = map[string]string{
	"node": "resource.labels.node_name",
}

var eventFields = map[string]string{
	"pod":       "jsonPayload.involvedObject.name",
	"namespace": "resource.labels.namespace_name",
	"node":      "resource.labels.node_name",
	"reason":    "jsonPayload.reason",
}

// All is the sorted list of all available presets.
var All = []Preset{
	{
		Name:        "cloudsql.errors",
		Service:     "cloudsql",
		Description: "CloudSQL errors",
		Filter:      `resource.type="cloudsql_database" severity>=ERROR`,
		Fields:      nil,
	},
	{
		Name:        "cloudsql.slow-query",
		Service:     "cloudsql",
		Description: "CloudSQL slow queries",
		Filter:      `resource.type="cloudsql_database" "duration:"`,
		Fields:      nil,
	},
	{
		Name:        "k8s.autoscaler",
		Service:     "k8s",
		Description: "Cluster autoscaler scale decisions",
		Filter:      `resource.type="k8s_cluster" log_id("container.googleapis.com/cluster-autoscaler-visibility") "decision" NOT "noDecisionStatus"`,
		Fields:      nil,
	},
	{
		Name:        "k8s.events",
		Service:     "k8s",
		Description: "All kubernetes events (catch-all)",
		Filter:      `log_id("events") (resource.type="k8s_pod" OR resource.type="k8s_node" OR resource.type="k8s_cluster")`,
		Fields:      eventFields,
	},
	{
		Name:        "k8s.node-pressure",
		Service:     "k8s",
		Description: "Node memory/disk/PID pressure",
		Filter:      `resource.type="k8s_node" log_id("events") (jsonPayload.reason="MemoryPressure" OR jsonPayload.reason="DiskPressure" OR jsonPayload.reason="PIDPressure")`,
		Fields:      nodeFields,
	},
	{
		Name:        "k8s.oom-kill",
		Service:     "k8s",
		Description: "OOMKilled containers (node-level)",
		Filter:      `resource.type="k8s_node" (jsonPayload.MESSAGE:"TaskOOM" OR jsonPayload.MESSAGE:"ContainerDied")`,
		Fields:      nodeFields,
	},
	{
		Name:        "k8s.pod-crash",
		Service:     "k8s",
		Description: "Pod crash loops and BackOff events",
		Filter:      `resource.type="k8s_pod" log_id("events") (jsonPayload.reason="BackOff" OR jsonPayload.reason="CrashLoopBackOff")`,
		Fields:      podFields,
	},
	{
		Name:        "k8s.pod-evicted",
		Service:     "k8s",
		Description: "Pod evictions",
		Filter:      `resource.type="k8s_pod" log_id("events") jsonPayload.reason="Evicted"`,
		Fields:      podFields,
	},
	{
		Name:        "k8s.pod-scheduling",
		Service:     "k8s",
		Description: "Scheduling failures and preemptions",
		Filter:      `resource.type="k8s_pod" log_id("events") (jsonPayload.reason="FailedScheduling" OR jsonPayload.reason="Preempted")`,
		Fields:      podFields,
	},
	{
		Name:        "k8s.pod-unhealthy",
		Service:     "k8s",
		Description: "Health probe failures",
		Filter:      `resource.type="k8s_pod" log_id("events") (jsonPayload.reason="Unhealthy" OR jsonPayload.reason="ProbeError")`,
		Fields:      podFields,
	},
}

// index maps preset names to their index in All for fast lookup.
var index map[string]int

func init() {
	index = make(map[string]int, len(All))
	for i, p := range All {
		index[p.Name] = i
	}
}

// Lookup returns the preset with the given name, or nil if not found.
func Lookup(name string) *Preset {
	if i, ok := index[name]; ok {
		return &All[i]
	}
	return nil
}
