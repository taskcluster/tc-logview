# tc-logview

A CLI tool for querying GCP Cloud Logging for [Taskcluster](https://taskcluster.net/) services. It knows the Taskcluster log schema — log types, their fields, and which services own them — so you don't have to manually construct GCP filter strings.

## Features

- **Schema-aware queries** — Fetches log type definitions from Taskcluster references. Knows that `worker-stopped` has fields `workerId`, `workerPoolId`, `reason`, etc.
- **Field shorthand** — Write `--where 'workerPoolId="proj-misc"'` instead of `--filter 'jsonPayload.Fields.workerPoolId="proj-misc"'`
- **Auto service narrowing** — When a log type is unique to one service (e.g., `worker-stopped` → `worker-manager`), the tool automatically narrows the query
- **Multiple output formats** — Aligned columns (default), JSONL (`--json`), or raw GCP entries (`--raw`)
- **Result caching** — Queries with absolute time windows are cached for 14 days, keyed by filter + time range hash
- **Environment management** — Configure multiple Taskcluster deployments, auto-detect from `TASKCLUSTER_ROOT_URL`

## Installation

```bash
go install github.com/taskcluster/tc-logview@latest
```

Or build from source:

```bash
git clone https://github.com/taskcluster/tc-logview.git
cd tc-logview
go build -o tc-logview .
```

## Setup

### 1. Create config

```bash
tc-logview config init
```

This creates `~/.config/tc-logview/config.yaml`:

```yaml
environments:
  fx-ci:
    project_id: "moz-fx-taskcluster-prod-4b87"
    cluster: "taskcluster-firefoxcitc-v1"
    cloudsql_instance: "taskcluster-prod-firefoxcitc-v1"
    root_url: "https://firefox-ci-tc.services.mozilla.com"
    key_path: "~/.config/tc-logview/keys/tc-prod.json"
  community-tc:
    project_id: "moz-fx-taskcluster-prod-4b87"
    cluster: "taskcluster-communitytc-v1"
    cloudsql_instance: "taskcluster-prod-communitytc-v1"
    root_url: "https://community-tc.services.mozilla.com"
    key_path: "~/.config/tc-logview/keys/tc-prod.json"
```

### 2. Add GCP credentials

Place your service account JSON key in `~/.config/tc-logview/keys/` and update the `key_path` in config.

### 3. Sync references

```bash
tc-logview sync
```

Fetches log type definitions from your Taskcluster deployment(s) and caches them locally.

## Usage

### List available log types

```bash
# All types
tc-logview list

# Filter by service
tc-logview list --service worker-manager

# Filter by severity
tc-logview list --level err
```

Output:
```
SERVICE          TYPE                    LEVEL   FIELDS
worker-manager   worker-requested        notice  workerId, workerPoolId, providerId, workerGroup
worker-manager   worker-running          notice  workerId, workerPoolId, providerId, registrationDuration
worker-manager   worker-stopped          notice  workerId, workerPoolId, providerId, runningDuration, workerAge
queue            task-claimed            notice  taskId, runId, workerId, workerGroup, taskQueueId
...
```

### Query logs

```bash
# Workers stopped in the last hour (default)
tc-logview query -e fx-ci --type worker-stopped

# Specific pool, last 6 hours
tc-logview query -e fx-ci --type worker-stopped \
  --where 'workerPoolId="proj-misc/generic"' --since 6h

# Errors in the last 2 hours
tc-logview query -e fx-ci --type monitor.error --since 2h --limit 50

# Precise time window (cacheable)
tc-logview query -e fx-ci --type task-claimed \
  --from 2024-01-15T10:00:00Z --to 2024-01-15T12:00:00Z

# JSONL output, pipe to jq
tc-logview query -e fx-ci --type hook-fire --json | jq '.taskId'

# Raw GCP entries for debugging
tc-logview query -e fx-ci --type worker-stopped --raw --limit 5

# Page through cached results
tc-logview query -e fx-ci --type worker-stopped \
  --from 2024-01-15T10:00:00Z --to 2024-01-15T12:00:00Z \
  --limit 100 --offset 200
```

### Infrastructure log presets

Query Kubernetes and CloudSQL events without constructing GCP filters:

```bash
# Pod crash loops in the last hour
tc-logview query -e fx-ci --type k8s.pod-crash

# Pod crashes in a specific namespace
tc-logview query -e fx-ci --type k8s.pod-crash --where 'namespace="taskcluster"'

# OOM kills in the last 6 hours
tc-logview query -e fx-ci --type k8s.oom-kill --since 6h

# All k8s events for a specific pod
tc-logview query -e fx-ci --type k8s.events --where 'pod="worker-manager-abc"'

# Health probe failures
tc-logview query -e fx-ci --type k8s.pod-unhealthy --where 'namespace="taskcluster"'

# Cluster autoscaler decisions
tc-logview query -e fx-ci --type k8s.autoscaler --since 2h

# CloudSQL errors
tc-logview query -e fx-ci --type cloudsql.errors --since 2h

# List all available presets
tc-logview list --service k8s
```

Available presets:

| Type | Description | Filter fields |
|---|---|---|
| `k8s.pod-crash` | Pod crash loops and BackOff events | pod, namespace |
| `k8s.pod-unhealthy` | Health probe failures | pod, namespace |
| `k8s.pod-evicted` | Pod evictions | pod, namespace |
| `k8s.pod-scheduling` | Scheduling failures and preemptions | pod, namespace |
| `k8s.container-died` | Container died events (includes normal exits) | node, message |
| `k8s.oom-kill` | OOMKilled containers (node-level) | node, message |
| `k8s.node-pressure` | Node memory/disk/PID pressure | node, message |
| `k8s.autoscaler` | Cluster autoscaler scale decisions | — |
| `k8s.events` | All kubernetes events (catch-all) | pod, namespace, node, reason |
| `cloudsql.errors` | CloudSQL errors | — |
| `cloudsql.slow-query` | CloudSQL slow queries | — |

### Environment selection

Either pass `-e` explicitly or set `TASKCLUSTER_ROOT_URL`:

```bash
# Explicit
tc-logview query -e fx-ci --type worker-stopped

# Auto-detect from env var
export TASKCLUSTER_ROOT_URL=https://firefox-ci-tc.services.mozilla.com
tc-logview query --type worker-stopped
```

## Query flags

| Flag | Description | Default |
|------|-------------|---------|
| `-e, --env` | Environment name | from `TASKCLUSTER_ROOT_URL` |
| `--type` | Log type (e.g. `worker-stopped`, `monitor.error`) | |
| `--service` | Filter by service (useful for shared types like `monitor.apiMethod`) | auto-detected if type is unique |
| `--where` | Field shorthand filter (e.g. `workerPoolId="proj-misc"`) | |
| `--filter` | Raw GCP filter expression | |
| `--since` | Relative time window (`30m`, `2h`, `1d`) | `1h` |
| `--from` | Absolute start time (RFC3339) | |
| `--to` | Absolute end time (RFC3339) | |
| `--limit` | Max entries | `100` |
| `--offset` | Skip N entries (cached results only) | `0` |
| `--json` | JSONL output | |
| `--raw` | Full raw GCP entries | |
| `--no-cache` | Skip result cache | |

## How it works

### Filter construction

The tool builds GCP filters from three layers:

1. **Scope** — `resource.labels.cluster_name` (from environment config)
2. **Type** — `jsonPayload.Type` + `jsonPayload.serviceContext.service` (from `--type`, auto-narrowed)
3. **User** — `--where` (expanded to `jsonPayload.Fields.*`) and `--filter` (raw passthrough)

### Caching

- **References** (`~/.cache/tc-logview/references/`) — Cached indefinitely, refresh with `tc-logview sync`
- **Query results** (`~/.cache/tc-logview/results/`) — 14-day TTL, keyed by MD5 of environment + time window + filter. Only absolute time windows are cached; `--since` is resolved to the nearest minute for the cache key.

## Project structure

```
tc-logview/
  main.go                     Entry point
  cmd/
    root.go                   Cobra root, --env flag, env resolution
    sync.go                   Sync references from Taskcluster
    list.go                   List known log types
    query.go                  Query logs with filter building + caching
    config_init.go            Generate default config
  internal/
    presets/presets.go          Curated k8s/CloudSQL query presets
    config/config.go          YAML config parsing, ~ expansion
    cache/cache.go            File cache with MD5 keys and TTL
    references/
      sync.go                 HTTP fetch of Taskcluster log references
      index.go                Type → service+fields lookup index
    gcp/client.go             GCP logadmin client, field extraction
    filter/builder.go         Three-layer filter string construction
    format/
      format.go               LogEntry type and Formatter interface
      columns.go              Aligned columns output
      jsonl.go                JSONL output
      raw.go                  Raw JSON output
```

## Note on module path

The Go module is currently `github.com/taskcluster/tc-logview`. Update `go.mod` and all import paths if hosting under a different path.
