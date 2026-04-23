# Azure Provider Debugging

Investigation playbook for Azure provider issues: API latency, throttling, ARM deployment failures, scanner slowdowns.

## 1. Check cloud API metrics

Start with the overall API call volume and latency per provider:

```bash
tc-logview query -e <env> --type cloud-api-metrics \
  --where 'providerId="<PROVIDER>"' --since 12h --limit 200
```

Normal baseline: avg 2-3.5s per call. Flags: p95 > 10s or max > 30s indicates outlier latency.

To see which pools are generating the most API traffic:

```bash
tc-logview query -e <env> --type cloud-api-metrics --since 12h --limit 200 --json | \
  jq -r 'select(.total != "0") | [.timestamp[0:16], .providerId, .total, .duration, .workerPoolId] | @tsv'
```

## 2. Check Azure throttling (429s)

The `azure-throttled` log type gives detailed rate-limit information:

```bash
tc-logview query -e <env> --type azure-throttled \
  --where 'providerId="<PROVIDER>"' --since 12h
```

Key fields to examine:
- **`retryAfterSeconds`** — how long the backoff is
- **`remainingReads/Writes/Deletes`** — which subscription limit is exhausted
- **`operationType`** — `read` (scanner), `write` (provisioner), or `delete` (cleanup)

If `operationType` is mostly `read`: the scanner is hitting read limits scanning worker state.
If `write`: provisioning new workers is exceeding write limits.
If `delete`: cleanup of stopped workers is hitting delete limits.

## 3. Check cloud-api-paused (older, broader signal)

The older rate-limiting signal, less detail than azure-throttled but covers more cases:

```bash
tc-logview query -e <env> --type cloud-api-paused --since 30d --limit 100
```

A 429 from Azure causes a 50s pause (`_backoffDelay * 50`). Frequent pauses here compound scanner slowdowns.

## 4. Track ARM deployments for a pool

See when Azure workers are being requested via ARM deployment templates:

```bash
tc-logview query -e <env> --type monitor.apiMethod \
  --filter 'labels."k8s-pod/app_kubernetes_io/name"="taskcluster-worker-manager" jsonPayload.Fields.message="creating ARM deployment"' \
  --where 'workerPoolId="<POOL>"' --since 6h
```

Key fields: `workerPoolId`, `deploymentName`, `name`.

## 5. Check worker registration failures

Workers that fail to register due to schema validation errors:

```bash
tc-logview query -e <env> --type monitor.apiMethod \
  --where 'name="reportWorkerError"' --where 'statusCode="400"' --since 6h
```

400 status = schema validation failure in the worker error report. Check the response body for which field is invalid.

## 6. Scanner per-pool deep dive

Filter scanner logs for a specific pool:

```bash
tc-logview query -e <env> \
  --filter 'labels."k8s-pod/app_kubernetes_io/component"="taskcluster-worker-manager-workerscanner-azure" jsonPayload.Fields.workerPoolId="<POOL>"' \
  --since 6h --limit 100
```

Check scan-seen counts per provider (how many workers the scanner sees each cycle):

```bash
tc-logview query -e <env> --type scan-seen --since 4d --limit 200 --json | \
  jq -r 'select(.total != "0" and .total != 0) | [.timestamp[0:16], .providerId, .total] | @tsv'
```

## 7. Scanner slowdown diagnosis

When `monitor.periodic` shows long scanner durations (workerScannerAzure duration in ms, 6000000ms = timeout):

```bash
tc-logview query -e <env> --type monitor.periodic \
  --filter 'jsonPayload.serviceContext.service="worker-manager"' --since 12h --limit 200
```

**Check the stopping:running ratio** — fastest diagnostic signal:

```bash
tc-logview query -e <env> --type simple-estimate --since 6h --limit 200 --json | \
  jq -r 'select((.stoppingCapacity | tonumber) > 0) | [.timestamp[0:16], .workerPoolId, "existing=" + .existingCapacity, "requested=" + .requestedCapacity, "stopping=" + .stoppingCapacity, "pending=" + .pendingTasks] | @tsv' | sort -t'=' -k4 -rn | head -20
```

A stopping:running ratio > 3:1 means the scanner is drowning in deprovision work.

**Check worker-manager errors:**

```bash
tc-logview query -e <env> --type monitor.error \
  --filter 'jsonPayload.serviceContext.service="worker-manager"' --since 4d --limit 200 --json | \
  jq -r '.message[:80]' | sort | uniq -c | sort -rn
```

Common error patterns:
- "Iteration exceeded maximum time allowed" — scanner timeout
- "Invalid resource group location" — RG conflicts
- "Cannot read properties of undefined" — code bug
- "statement timeout" — DB pressure

**Inspect live worker state** with the taskcluster CLI:

```bash
taskcluster api workerManager listWorkersForWorkerPool <POOL_ID> | \
  jq '[.workers[] | .state] | group_by(.) | map({state: .[0], count: length})'
```

Check if stopping workers ever ran tasks:

```bash
taskcluster api workerManager listWorkersForWorkerPool <POOL_ID> | \
  jq '{total_stopping: [.workers[] | select(.state == "stopping")] | length, with_tasks: [.workers[] | select(.state == "stopping" and (.recentTasks | length) > 0)] | length, no_tasks: [.workers[] | select(.state == "stopping" and (.recentTasks | length) == 0)] | length}'
```

Note: `recentTasks` may be empty for workers that did run tasks (field isn't always populated). Cross-check by looking for `claimWork` API calls for specific workers.

**Root causes of scanner slowdowns (in order of likelihood):**
- **Stopping worker backlog**: Each stopping worker needs ~8 Azure API calls (VM, NIC, IP, disks) at 2-3.5s each = ~20s per worker, single-threaded. 1000 stopping workers = ~5.5hrs.
- **Low idle timeout**: Workers with short `idleTimeoutSecs` (e.g., 150s) die after every task, creating churn. Bumping to 900-1800s helps when tasks are always pending.
- **Azure API outlier latency**: Occasional calls take 20-34s, blocking the single-threaded queue.
- **Rate limit backoff**: 429s cause 50s pauses. Check `azure-throttled` and `cloud-api-paused`.
- **Resource group ensure race**: Per-pool RGs + concurrent workers = TOCTOU race in `ensureResourceGroup` cache.
- **`shouldWorkerTerminate` staleness**: Decisions computed at end of scanner run. With 30-60min cycles, workers get stale terminate decisions.
