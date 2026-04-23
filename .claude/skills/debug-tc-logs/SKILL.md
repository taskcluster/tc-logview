---
name: debug-tc-logs
description: Use when debugging Taskcluster issues — task failures, worker problems, API errors, queue issues. Queries task status/logs via `taskcluster` CLI and GCP Cloud Logging via `tc-logview` across community-tc, fx-ci, staging, and dev environments.
user_invocable: true
---

# Debug Taskcluster Logs

You are debugging Taskcluster by querying task status/logs via the `taskcluster` CLI and GCP Cloud Logging via `tc-logview`. Follow this protocol strictly.

## 0. Health Summary Script

For broad "what happened?" investigations across environments, use the pre-built aggregation script before running manual queries:

```bash
bash ~/.claude/skills/debug-tc-logs/tc-health-summary.sh --since 12h
# or: --since 6h, --envs "fx-ci", --envs "community-tc fx-ci staging", etc.
```

The script runs all key queries in parallel (errors, 500s, 403s, claim-expired, deadline-exceeded, worker-stopped) for each environment and prints a structured summary. Use it as a starting point — it will reveal which areas need deeper follow-up queries.

## 1. Prerequisites

Before running any query, verify:

```bash
which tc-logview
tc-logview list 2>&1 | head -5
```

If `tc-logview` is not installed, tell the user to install it (`go install github.com/taskcluster/tc-logview@latest`) or build from source.

If `tc-logview list` says "no references cached", run:
```bash
tc-logview sync
```

If config is missing, run:
```bash
tc-logview config init
# Then place GCP service account keys in ~/.config/tc-logview/keys/
```

## 2. Taskcluster CLI

The `taskcluster` CLI is a first-class debugging tool for inspecting task state, logs, and artifacts.

### Setup

**Assume pre-configured for fx-ci** — `TASKCLUSTER_ROOT_URL` is already set in the environment pointing to `https://firefox-ci-tc.services.mozilla.com`.

**For other environments**, override per-command:

```bash
# community-tc
TASKCLUSTER_ROOT_URL=https://community-tc.services.mozilla.com taskcluster ...

# staging
TASKCLUSTER_ROOT_URL=https://stage.taskcluster.net taskcluster ...
```

### Key Commands

```bash
# Get task state, runs, workerIds
taskcluster task status <taskId>

# Get full task definition (payload, metadata, workerPoolId)
taskcluster task def <taskId>

# Stream/fetch task output log
taskcluster task log <taskId>

# List available artifacts
taskcluster task artifacts <taskId>

# Overview of a task group
taskcluster group status <groupId>
```

### Fetching Task Log by URL

When you need to fetch `live_backing.log` directly (e.g., for a specific run):

```
https://<root-url>/api/queue/v1/task/<taskId>/runs/<runId>/artifacts/public/logs/live_backing.log
```

Use `curl -sL` or `WebFetch` to retrieve.

### Worker-Manager API

The `taskcluster` CLI can query worker-manager directly for live worker state:

```bash
# List workers in a pool with state breakdown
taskcluster api workerManager listWorkersForWorkerPool <workerPoolId>

# Useful jq patterns:
# State distribution
... | jq '[.workers[] | .state] | group_by(.) | map({state: .[0], count: length})'

# Stopping workers that never claimed tasks
... | jq '{stopping: [.workers[] | select(.state == "stopping")] | length, no_tasks: [.workers[] | select(.state == "stopping" and (.recentTasks | length) == 0)] | length}'

# Age of stopping workers
... | jq -r '.workers[] | select(.state == "stopping") | [.created[0:16], .workerId] | @tsv' | head -20
```

### Installing the CLI

If `taskcluster` is not available:
```bash
curl -L https://github.com/taskcluster/taskcluster/releases/download/v99.0.3/taskcluster-linux-amd64.tar.gz --output /tmp/taskcluster.tar.gz && tar -xvf /tmp/taskcluster.tar.gz -C /tmp && rm /tmp/taskcluster.tar.gz && chmod +x /tmp/taskcluster
# Then use as: /tmp/taskcluster ...
```

### Auth Note

By default only public API calls work (task status, definitions, public artifacts). If scoped credentials are needed for private artifacts or write operations, use `taskcluster signin -s <scope>`.

## 3. Environment Mapping

| Environment | GCP Project | Cluster Name | Key File |
|---|---|---|---|
| community-tc | `moz-fx-taskcluster-prod-4b87` | `taskcluster-communitytc-v1` | `tc-prod.json` |
| fx-ci | `moz-fx-taskcluster-prod-4b87` | `taskcluster-firefoxcitc-v1` | `tc-prod.json` |
| staging | `moz-fx-taskclust-nonprod-9302` | `taskcluster-nonprod-v1` | `tc-nonprod.json` |
| dev | `taskcluster-dev` | `taskcluster-dev` | `tc-dev.json` |

Note: community-tc and fx-ci share the same GCP project and key file but have different cluster names. The `tc-logview` config handles this automatically — use `-e fx-ci` or `-e community-tc`.

## 4. Environment Resolution

Determine the environment from context using this priority:

1. **`TASKCLUSTER_ROOT_URL` env var** — `tc-logview` auto-detects from this, no `-e` needed
2. **Explicit name**: user says "community-tc", "fx-ci", "staging", or "dev" → use `-e <name>`
3. **TC root URL in message**:
   - `community-tc.services.mozilla.com` → community-tc
   - `firefox-ci-tc.services.mozilla.com` → fx-ci
   - `stage.taskcluster.net` → staging
   - `taskcluster-dev.net` → dev
4. **workerPoolId prefix**: pool IDs like `proj-*` are typically community-tc; `gecko-*`/`mobile-*` are fx-ci
5. **If ambiguous**: Ask the user which environment

## 5. tc-logview Command Template

Every GCP log query uses `tc-logview query`. Basic form:

```bash
tc-logview query -e <ENV> --type <LOG_TYPE> --since <TIME>
```

Key flags:
- `-e <env>` — environment (`fx-ci`, `community-tc`, `staging`, `dev`). Omit if `TASKCLUSTER_ROOT_URL` is set.
- `--type <type>` — log type (e.g. `worker-stopped`, `monitor.error`). Auto-narrows by service.
- `--where '<field>=<value>'` — filter on a known field without writing the full GCP path
- `--filter '<raw>'` — raw GCP filter expression (for advanced cases or unknown fields)
- `--since <duration>` — time window: `30m`, `2h`, `6h`, `1d` (default: `1h`)
- `--from / --to` — absolute RFC3339 timestamps for precise incident windows
- `--limit <n>` — max entries (default: 100)
- `--json` — JSONL output for piping to `jq`
- `--no-cache` — bypass result cache

**Discover available log types:**
```bash
tc-logview list                        # all types
tc-logview list --service worker-manager
tc-logview list --level err
```

## 6. Progressive Filtering Strategy

### Step 1: Start narrow
```bash
tc-logview query -e <env> --type <type> --since 1h --limit 50
```
Include all known constraints via `--where`.

### Step 2: Adjust based on results
- **0 results** → widen time: `--since 4h`, then `--since 12h`, then `--since 24h`
- **Hitting the limit (50 results)** → tighten with more `--where` constraints, do NOT just increase limit
- **Between 1-49 results** → good, analyze these

### Step 3: Hard limits
- Maximum `--limit 500` — never go higher
- Prefer adding `--where` constraints over increasing limit
- If 500 is still not enough, split by time ranges with `--from`/`--to`

### Step 4: Extract and summarize
- Default aligned-columns output is already clean — present it directly for small result sets
- For structured processing, use `--json` and pipe to `jq`
- Summarize counts and patterns — never dump large raw output to the user

## 7. Common Query Patterns

Basic flag usage examples. For full investigation flows, see the playbooks in section 12.

### Filter by log type and field
```bash
tc-logview query -e <env> --type monitor.apiMethod \
  --where 'statusCode="500"' --since 1h
```

### Use raw filter for fields not covered by --where
```bash
tc-logview query -e <env> --type task-exception \
  --filter 'jsonPayload.Logger="taskcluster.queue.claim-resolver"' --since 1h
```

### Full-text search
```bash
tc-logview query -e <env> --filter '"PulsePublisher.sendDeadline exceeded"' --since 1h
```

### Investigate a single task or worker (text match across all fields)
```bash
tc-logview query -e <env> --type monitor.apiMethod --filter '"<TASK_ID>"' --since 6h
tc-logview query -e <env> --type monitor.apiMethod --filter '"<WORKER_ID>"' --since 6h
```

### JSON output for aggregation
```bash
tc-logview query -e <env> --type worker-removed --since 6h --limit 200 --json | \
  jq -r '.reason' | sort | uniq -c | sort -rn
```

## 8. Infrastructure Log Presets

For debugging infrastructure-level issues (not TC application logs), use preset types:

### Kubernetes Events
```bash
# Pod crash loops
tc-logview query -e <env> --type k8s.pod-crash --since 2h

# Pod crashes in a specific namespace
tc-logview query -e <env> --type k8s.pod-crash --where 'namespace="taskcluster"'

# OOM kills (node-level)
tc-logview query -e <env> --type k8s.oom-kill --since 6h

# Container died events (includes normal CronJob exits — use to investigate unexpected terminations)
tc-logview query -e <env> --type k8s.container-died --since 2h

# Health probe failures
tc-logview query -e <env> --type k8s.pod-unhealthy --where 'namespace="taskcluster"'

# Scheduling failures
tc-logview query -e <env> --type k8s.pod-scheduling --since 2h

# Pod evictions
tc-logview query -e <env> --type k8s.pod-evicted --since 6h

# Node memory/disk/PID pressure
tc-logview query -e <env> --type k8s.node-pressure --since 2h

# Cluster autoscaler decisions
tc-logview query -e <env> --type k8s.autoscaler --since 2h

# All k8s events (catch-all) — filter with --where reason=<reason>
tc-logview query -e <env> --type k8s.events --where 'pod="worker-manager-abc"' --since 2h
```

### CloudSQL
```bash
# Database errors
tc-logview query -e <env> --type cloudsql.errors --since 2h

# Slow queries
tc-logview query -e <env> --type cloudsql.slow-query --since 2h
```

### Preset field shorthands for --where:

| Shorthand | GCP path | Available in |
|---|---|---|
| `pod` | `jsonPayload.involvedObject.name` | k8s.pod-*, k8s.events |
| `namespace` | `resource.labels.namespace_name` | k8s.pod-*, k8s.events |
| `node` | `resource.labels.node_name` | k8s.oom-kill, k8s.container-died, k8s.node-pressure, k8s.events |
| `message` | `MESSAGE` (payload) | k8s.oom-kill, k8s.container-died, k8s.node-pressure |
| `reason` | `jsonPayload.reason` | k8s.events |

Use `tc-logview list --service k8s` or `tc-logview list --service cloudsql` to see all available presets.

## 9. GCP Log Field Reference

`tc-logview` handles field paths automatically. For `--where` use the short field name; for `--filter` use the full path.

| Short name (`--where`) | Full path (`--filter`) | Description | Example Values |
|---|---|---|---|
| `statusCode` | `jsonPayload.Fields.statusCode` | HTTP status code | `"403"`, `"500"` (string) |
| `taskId` | `jsonPayload.Fields.taskId` | Task identifier | `UwFQ3YVMRjyxmxHEO6_tQg` |
| `workerId` | `jsonPayload.Fields.workerId` | Worker identifier | `i-06e4c2473c69c4d72` |
| `workerPoolId` | `jsonPayload.Fields.workerPoolId` | Worker pool ID | `nss-1/b-win2022-alpha` |
| `name` | `jsonPayload.Fields.name` | API method name | `claimWork`, `createTask` |
| `clientId` | `jsonPayload.Fields.clientId` | TC client ID | |
| `message` | `jsonPayload.Fields.message` | Error message text | |
| `providerId` | `jsonPayload.Fields.providerId` | Cloud provider | |
| `reason` | `jsonPayload.Fields.reason` | Stop/remove reason | |
| — | `jsonPayload.Type` | Event type | `monitor.apiMethod`, `worker-stopped` |
| — | `jsonPayload.Logger` | Logger module | `taskcluster.queue.claim-resolver` |
| — | `jsonPayload.serviceContext.service` | Service name | `worker-manager`, `queue` |
| — | `severity` | Log severity | `ERROR`, `WARNING`, `INFO` |

Important: `statusCode` is a **string** in the logs — filter with `="403"` not `=403`.

Use `tc-logview list --service <name>` to see all available fields for each log type.

## 10. Investigation Playbooks

Map the user's problem to the right playbook, then read and follow it. Each playbook is a self-contained investigation flow with decision points and concrete commands.

| Problem area | Playbook |
|---|---|
| Task failure (given a task ID/URL) | `examples/task-failure-debugging.md` |
| Workers disappearing, removed, zombie | `examples/worker-removal-reasons.md` |
| Azure provider, ARM deployments, scanner slow, throttling | `examples/azure-provider-debugging.md` |
| Queue health, claim-expired, deadlines, pulse | `examples/queue-health-investigation.md` |
| GitHub integrations, webhooks, handler errors | `examples/github-service-debugging.md` |
| HTTP 502s, load balancer errors | `examples/http-502-investigation.md` |
| Service pods crashing, k8s/infra, database | `examples/infra-debugging.md` |

**Quick dispatch hints:**
- Given a task ID/URL → `task-failure-debugging.md`
- "Are there 500s?" / API errors → start with `--type monitor.apiMethod --where 'statusCode="500"'`, then follow the relevant playbook based on which service is erroring
- "What's broken?" (broad) → run the health summary script first (section 0), then follow the playbook matching the area with the most errors
- Worker pool slow/broken → `worker-removal-reasons.md` for removal patterns, `azure-provider-debugging.md` for provisioning/scanner issues
- Check worker X / worker stuck → `task-failure-debugging.md` section 4a-4b covers worker API call investigation

## 11. Auth Setup

When key files are missing, guide the user through setup. Keys go in `~/.config/tc-logview/keys/` (update `key_path` in `~/.config/tc-logview/config.yaml` to match).

### For `moz-fx-taskcluster-prod-4b87` (community-tc, fx-ci):
```bash
mkdir -p ~/.config/tc-logview/keys

# Create service account (one-time)
gcloud iam service-accounts create tc-log-reader \
  --project=moz-fx-taskcluster-prod-4b87 \
  --display-name="TC Log Reader"

# Grant logging read access
gcloud projects add-iam-policy-binding moz-fx-taskcluster-prod-4b87 \
  --member="serviceAccount:tc-log-reader@moz-fx-taskcluster-prod-4b87.iam.gserviceaccount.com" \
  --role="roles/logging.viewer"

# Create key file
gcloud iam service-accounts keys create ~/.config/tc-logview/keys/tc-prod.json \
  --iam-account=tc-log-reader@moz-fx-taskcluster-prod-4b87.iam.gserviceaccount.com
```

### For `moz-fx-taskclust-nonprod-9302` (staging):
```bash
gcloud iam service-accounts create tc-log-reader \
  --project=moz-fx-taskclust-nonprod-9302 \
  --display-name="TC Log Reader"

gcloud projects add-iam-policy-binding moz-fx-taskclust-nonprod-9302 \
  --member="serviceAccount:tc-log-reader@moz-fx-taskclust-nonprod-9302.iam.gserviceaccount.com" \
  --role="roles/logging.viewer"

gcloud iam service-accounts keys create ~/.config/tc-logview/keys/tc-nonprod.json \
  --iam-account=tc-log-reader@moz-fx-taskclust-nonprod-9302.iam.gserviceaccount.com
```

### For `taskcluster-dev` (dev):
```bash
gcloud iam service-accounts create tc-log-reader \
  --project=taskcluster-dev \
  --display-name="TC Log Reader"

gcloud projects add-iam-policy-binding taskcluster-dev \
  --member="serviceAccount:tc-log-reader@taskcluster-dev.iam.gserviceaccount.com" \
  --role="roles/logging.viewer"

gcloud iam service-accounts keys create ~/.config/tc-logview/keys/tc-dev.json \
  --iam-account=tc-log-reader@taskcluster-dev.iam.gserviceaccount.com
```

After placing the key, update `~/.config/tc-logview/config.yaml` with the correct `key_path` for that environment, then run `tc-logview sync`.

## 12. Anti-Patterns

- **Never** run a query without `--limit`
- **Never** start with `--since 30d` — always start narrow (`--since 1h`) and widen
- **Never** dump large raw output to the user — always summarize or pipe through `jq`
- **Never** omit the cluster scope — `tc-logview` handles this automatically via the environment config, but if using raw `--filter` always include `resource.labels.cluster_name`
- **Never** run multiple widening steps without checking results between them
- **Never** use `--limit` greater than 500
- **Prefer** `--type` + `--where` over raw `--filter` — the tool validates field names and auto-narrows the service scope

## 13. Current Limitations

- **Worker system logs (Papertrail)**: Not yet automated — when worker-level logs are needed, tell the user to check Papertrail manually with the worker hostname
- **GCP Audit Logs for VM lifecycle**: Worker VMs may run in different GCP projects depending on the pool/provider; infra-level debugging requires knowing which project, which varies by environment and pool configuration
- **`--offset` with live queries**: `--offset` only works on cached results (absolute time windows). For relative `--since` queries, increase `--limit` or switch to `--from`/`--to`
