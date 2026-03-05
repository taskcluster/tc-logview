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

### API errors (403/500)
```bash
tc-logview query -e <env> --type monitor.apiMethod \
  --where 'statusCode="500"' --since 1h
```
Key fields in output: `method`, `statusCode`, `clientId`, `duration`

For a specific status code and method:
```bash
tc-logview query -e <env> --type monitor.apiMethod \
  --where 'statusCode="403"' --filter 'jsonPayload.Fields.name="claimWork"' --since 2h
```

### Task claim-expired
```bash
tc-logview query -e <env> --type task-exception \
  --filter 'jsonPayload.Logger="taskcluster.queue.claim-resolver"' --since 1h
```

### Task deadline-exceeded
```bash
tc-logview query -e <env> --type task-exception \
  --filter 'jsonPayload.Logger="taskcluster.queue.deadline-resolver"' --since 1h
```

### Pulse publisher deadline
```bash
tc-logview query -e <env> \
  --filter '"PulsePublisher.sendDeadline exceeded"' --since 1h
```

### GitHub signature mismatch
```bash
tc-logview query -e <env> --type monitor.error \
  --filter 'jsonPayload.Fields.message="X-hub-signature does not match"' --since 2h
```

### Investigate a single task
```bash
tc-logview query -e <env> --type monitor.apiMethod \
  --filter '"<TASK_ID>"' --since 6h
```

### Investigate a single worker (all API calls)
```bash
tc-logview query -e <env> --type monitor.apiMethod \
  --filter '"<WORKER_ID>"' --since 6h
```

Faster variant (worker-manager events only):
```bash
tc-logview query -e <env> --where 'workerId="<WORKER_ID>"' --since 6h
```

### Worker pool lifecycle
```bash
# All lifecycle events for a pool
tc-logview query -e <env> --type worker-requested \
  --where 'workerPoolId="<POOL_ID>"' --since 6h

tc-logview query -e <env> --type worker-stopped \
  --where 'workerPoolId="<POOL_ID>"' --since 6h

# Or use raw filter to get all lifecycle types in one query
tc-logview query -e <env> \
  --filter '(jsonPayload.Type="worker-requested" OR jsonPayload.Type="worker-running" OR jsonPayload.Type="worker-stopped" OR jsonPayload.Type="worker-removed") AND jsonPayload.Fields.workerPoolId="<POOL_ID>"' \
  --since 6h --json | jq '{type: .jsonPayload.Type, workerId: .workerId, reason: .reason}'
```

### GitHub service errors
```bash
tc-logview query -e <env> --type monitor.error \
  --filter 'jsonPayload.Logger="taskcluster.github.api"' --since 168h
```

To identify the repo, correlate with `traceId`:
```bash
tc-logview query -e <env> \
  --filter '"<repo-name>" AND severity="ERROR"' --since 24h
```

### Simple estimate (capacity planning)
```bash
tc-logview query -e <env> --type simple-estimate \
  --where 'workerPoolId="<POOL_ID>"' --since 2h
```

## 8. GCP Log Field Reference

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

## 9. Task-Level Debugging Workflow

Step-by-step protocol when given a task ID or task URL:

1. **Extract task ID** — parse `tasks/<taskId>` from TC URLs (e.g., `https://firefox-ci-tc.services.mozilla.com/tasks/IKBB-zASS_uNES7KC8MJpg`)
2. **Get task status** — `taskcluster task status <taskId>` → identify state (`failed`/`exception`/`completed`), number of runs, resolution reason
3. **Get task definition** — `taskcluster task def <taskId>` → extract `workerPoolId`, `taskQueueId`, `metadata.name`
4. **Fetch task log** — `taskcluster task log <taskId>` → search for error patterns (broken pipe, OOM, timeout, exit codes)
5. **Identify worker** — from task status runs, extract `workerId` and `workerGroup`
6. **Cross-reference with TC service logs** — use `tc-logview query` filtered by taskId or workerId (see Common Query Patterns)

## 10. Common Task Failure Patterns

| Pattern | Log Signature | Next Step |
|---|---|---|
| Broken pipe | `write \|1\|: broken pipe` in task log | Check worker for preemption/network issues |
| Livelog port conflict | `listen tcp :60098: bind: address already in use` in worker log | Usually non-fatal, check if task continued |
| Claim expired | Task state `exception`, reason `claim-expired` | Check worker API calls via tc-logview |
| Deadline exceeded | Task state `exception`, reason `deadline-exceeded` | Check if task was actually running or stuck |
| Docker container crash | Container exited non-zero, veth interface down | Check task log for OOM or command failure |
| GCP preemption | `google_guest_agent ERROR metadata.go: Error watching metadata: context canceled` | Check GCP audit logs for instance lifecycle |
| Worker idle shutdown | Worker hits idle timeout after task failure | Not a root cause — look earlier in timeline |

## 11. Debugging Decision Tree

Map user intent to query sequence:

**"Given a TC task URL"**
1. Parse taskId from URL (extract `tasks/<taskId>` segment)
2. Determine environment from the root URL (see Environment Resolution)
3. Follow "Why did task X fail?" flow below

**"Why did task X fail?"**
1. `taskcluster task status <taskId>` → check state and resolution reason
2. `taskcluster task log <taskId>` → search for error patterns in output
3. If `claim-expired` → extract workerId from status runs → query GCP logs for worker's API calls
4. If `deadline-exceeded` → check if task was actually running or stuck in queue
5. If `failed` → check task log for exit codes, broken pipe, OOM, or command errors
6. If infrastructure suspicion → note: check Papertrail/audit logs manually (future automation)

**"Task has broken pipe / unexpected termination"**
1. Get task log, confirm the error pattern
2. Get workerId from task status
3. Query GCP logs by workerId to check if other tasks on the same worker also failed
4. If only this task failed: likely a transient issue. If multiple tasks failed: worker-level problem

**"Check worker X" / "Worker seems stuck"**
1. Investigate single worker (all API calls by worker)
2. Look for pattern: only `claimWork` calls = worker is claiming but tasks keep expiring
3. Check if worker appears in claim-expired results

**"Are there 500s?" / "API errors?"**
1. Query 500 API calls with `--type monitor.apiMethod --where 'statusCode="500"'`
2. Group by method: `--json | jq -r '.name' | sort | uniq -c | sort -rn`
3. Check for Pulse publisher deadline errors (common root cause of 500s)

**"Is the queue healthy?"**
1. Count claim-expired tasks: `--type task-exception --filter 'jsonPayload.Logger="taskcluster.queue.claim-resolver"'`
2. Count deadline-exceeded tasks: `--type task-exception --filter 'jsonPayload.Logger="taskcluster.queue.deadline-resolver"'`
3. If counts are high, group by task queue with `--json | jq -r '.taskQueueId' | sort | uniq -c`

**"Worker pool X is slow/broken"**
1. Query worker lifecycle events: `--type worker-stopped --where 'workerPoolId="<POOL>"'`
2. Check reasons: `--json | jq -r '.reason' | sort | uniq -c`
3. Check `worker-requested` vs `worker-running` counts to spot provisioning failures

**"GitHub integrations broken?"**
1. Check GitHub signature mismatch errors
2. Check 500s filtered to github-related API methods

**"GitHub service failing to process .taskcluster.yml / JSON-e template errors?"**
1. Check github api errors: `--type monitor.error --filter 'jsonPayload.Logger="taskcluster.github.api"' --since 168h`
2. Errors surface as `monitor.error` with `jsonPayload.Fields.badMessage` containing the JSON-e error (e.g. `InterpreterError`, `TemplateError`)
3. To find which repo triggered the error, use the `traceId` from the error entry to correlate with other log entries in the same request — but note: `owner`/`repoName` fields may be null on error entries
4. To identify the repo from a webhook event, search for `"<repo-name>"` as a text filter and look at debug-level `Github webhook payload` entries
5. Note: PR/push processing is async — the webhook handler returns 200 immediately, then the handlers process the queued message; template errors may appear in the api logger (for direct preview calls) or not surface at all if the handlers swallow them
6. `issue_comment` webhooks containing the template text will match broad text searches for variable names — filter with `--filter 'severity="ERROR"'` to avoid false positives

## 12. Auth Setup

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

## 13. Anti-Patterns

- **Never** run a query without `--limit`
- **Never** start with `--since 30d` — always start narrow (`--since 1h`) and widen
- **Never** dump large raw output to the user — always summarize or pipe through `jq`
- **Never** omit the cluster scope — `tc-logview` handles this automatically via the environment config, but if using raw `--filter` always include `resource.labels.cluster_name`
- **Never** run multiple widening steps without checking results between them
- **Never** use `--limit` greater than 500
- **Prefer** `--type` + `--where` over raw `--filter` — the tool validates field names and auto-narrows the service scope

## 14. Current Limitations

- **Worker system logs (Papertrail)**: Not yet automated — when worker-level logs are needed, tell the user to check Papertrail manually with the worker hostname
- **GCP Audit Logs for VM lifecycle**: Worker VMs may run in different GCP projects depending on the pool/provider; infra-level debugging requires knowing which project, which varies by environment and pool configuration
- **`--offset` with live queries**: `--offset` only works on cached results (absolute time windows). For relative `--since` queries, increase `--limit` or switch to `--from`/`--to`
