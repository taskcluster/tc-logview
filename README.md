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
    project_id: "moz-fx-webservices-high-prod"
    cluster: "webservices-high-prod"
    namespace: "taskcluster-prod"
    cloudsql_project_id: "moz-fx-taskcluster-prod"
    cloudsql_instance: "taskcluster-prod-20260409-1"
    root_url: "https://firefox-ci-tc.services.mozilla.com"
    key_path: "~/.config/tc-logview/keys/tc-prod.json"
  community-tc:
    project_id: "moz-fx-webservices-high-prod"
    cluster: "webservices-high-prod"
    namespace: "taskcluster-communitytc"
    cloudsql_project_id: "moz-fx-taskcluster-prod"
    cloudsql_instance: "taskcluster-community-20260317-1"
    root_url: "https://community-tc.services.mozilla.com"
    key_path: "~/.config/tc-logview/keys/tc-prod.json"
  staging:
    project_id: "moz-fx-webservices-high-nonpro"
    cluster: "webservices-high-nonprod"
    namespace: "taskcluster-stage"
    root_url: "https://stage.taskcluster.nonprod.cloudops.mozgcp.net"
    key_path: "~/.config/tc-logview/keys/tc-staging.json"
  dev:
    project_id: "taskcluster-dev"
    cluster: "taskcluster-dev"
    root_url: "https://tc.dev.taskcluster.mozgcp.net"
    # key_path omitted — uses ADC by default

  # Scoped envs for untrusted agents — query a single log view, not the project.
  fx-ci-scoped:
    project_id: "moz-fx-taskcluster-prod"
    cluster: "webservices-high-prod"
    namespace: "taskcluster-prod"
    log_bucket: "gke-taskcluster-prod-log-bucket"
    log_location: "global"
    log_view: "_AllLogs"
    root_url: "https://firefox-ci-tc.services.mozilla.com"
  community-tc-scoped:
    project_id: "moz-fx-taskcluster-prod"
    cluster: "webservices-high-prod"
    namespace: "taskcluster-communitytc"
    log_bucket: "gke-taskcluster-communitytc-log-bucket"
    log_location: "global"
    log_view: "_AllLogs"
    root_url: "https://community-tc.services.mozilla.com"
  staging-scoped:
    project_id: "moz-fx-webservices-high-nonpro"
    cluster: "webservices-high-nonprod"
    namespace: "taskcluster-stage"
    log_bucket: "gke-taskcluster-stage-log-bucket"
    log_location: "global"
    log_view: "_AllLogs"
    root_url: "https://stage.taskcluster.nonprod.cloudops.mozgcp.net"
```

### 2. Authenticate to GCP

Choose one of the three options below. Options A and B are configured per environment in `config.yaml`; Option C is selected at runtime via the `TC_LOGVIEW_ACCESS_TOKEN` env var and overrides whatever the config says. In the example above, `fx-ci`, `community-tc`, and `staging` use service account keys (Option B); `dev` uses ADC (Option A).

#### Option A — Application Default Credentials (recommended for local use)

Run once on your machine:

```bash
gcloud auth application-default login
```

Then **omit `key_path`** for any environment that should use ADC. tc-logview will fall back to the GCP SDK's default credential chain.

#### Option B — Service account key file (for containers / shared infra)

Place your service account JSON key in `~/.config/tc-logview/keys/` and set `key_path` for that environment. The service account only needs `roles/logging.viewer` — CloudSQL logs are read through Cloud Logging too, so no additional roles are required.

This mode is what you want inside an untrusted/containerized environment where you do not want to share your personal `gcloud` session.

#### Option C — Injected access token (recommended for containers / agents)

For containers running untrusted code (agents, CI jobs, ephemeral sandboxes), prefer a pre-issued short-lived bearer token over a long-lived service account key. The host impersonates a low-privilege service account once and passes only the resulting access token into the container.

```bash
# On the host (requires roles/iam.serviceAccountTokenCreator on the SA):
TOKEN=$(gcloud auth print-access-token \
  --impersonate-service-account=tc-log-reader@<PROJECT>.iam.gserviceaccount.com)

# Run tc-logview with the token in the container's env:
docker run --rm \
  -e TC_LOGVIEW_ACCESS_TOKEN="$TOKEN" \
  -e TASKCLUSTER_ROOT_URL=https://firefox-ci-tc.services.mozilla.com \
  tc-logview-image query --type worker-stopped --limit 5
```

When `TC_LOGVIEW_ACCESS_TOKEN` is set it takes priority over `key_path` and ADC. The container never sees the host's `gcloud` session, never calls the IAM Credentials API, and cannot mint or refresh tokens. Worst-case leakage is one access token, valid only for its TTL (~1h).

Re-mint and re-inject the token before it expires for long-running containers. An expired token surfaces as a `401` from the query path with a hint pointing back at the re-mint command.

#### Scoping a token to TaskCluster logs only (`*-scoped` envs)

An access token minted from your own ADC inherits your privileges — too broad for an untrusted agent. To hand an agent a token that can read **only** TaskCluster's logs, mint it from a dedicated, narrowly-scoped reader service account and point tc-logview at a **log view** instead of the whole project.

This is what the `*-scoped` environments are for. They set three optional fields that confine every query to a single Cloud Logging log view:

| Field | Meaning | Default |
|---|---|---|
| `log_view` | View ID; when set, queries are scoped to this view (otherwise project scope) | — (project scope) |
| `log_bucket` | Bucket holding the view | `_Default` |
| `log_location` | Bucket location | `global` |

The `tc-logview-reader` service account is granted `roles/logging.viewAccessor` on exactly that view (a per-namespace tenant log bucket), so a token minted from it can read nothing else:

```bash
# Host: mint a narrow, short-lived token by impersonating the reader SA.
TOKEN=$(gcloud auth print-access-token \
  --impersonate-service-account=tc-logview-reader@moz-fx-taskcluster-prod.iam.gserviceaccount.com)

# Agent/container: scoped env queries only the TaskCluster log view.
TC_LOGVIEW_ACCESS_TOKEN=$TOKEN tc-logview query -e fx-ci-scoped --type monitor.error --since 1h
```

The same `tc-logview-reader` service account backs all scoped envs:

| Scoped env | Project | Log bucket |
|---|---|---|
| `fx-ci-scoped` | `moz-fx-taskcluster-prod` | `gke-taskcluster-prod-log-bucket` |
| `community-tc-scoped` | `moz-fx-taskcluster-prod` | `gke-taskcluster-communitytc-log-bucket` |
| `staging-scoped` | `moz-fx-webservices-high-nonpro` | `gke-taskcluster-stage-log-bucket` |

The reader SA lives in the prod project; for `staging-scoped` it's granted `viewAccessor` on the stage bucket in the nonprod project, so the same impersonation command works for all three.

> Infrastructure presets (`k8s.*`, `cloudsql.*`) are **not** available on `*-scoped` envs — those logs live in other projects/buckets the reader SA can't see. Use the broad envs (with your own ADC) for infra queries.

> Auto-detection via `TASKCLUSTER_ROOT_URL` always resolves to the broad env (scoped envs share its `root_url`); select a scoped env explicitly with `-e`.

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

1. **Scope** — `resource.labels.cluster_name` (from environment config); `resource.labels.namespace_name` when `namespace` is set and the resource type supports it
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
