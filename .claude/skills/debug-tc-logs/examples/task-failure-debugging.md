# Task Failure Debugging

Investigation playbook when given a task ID or task URL.

## 1. Extract task ID and environment

Parse `tasks/<taskId>` from TC URLs:
- `https://firefox-ci-tc.services.mozilla.com/tasks/IKBB-zASS_uNES7KC8MJpg` → taskId=`IKBB-zASS_uNES7KC8MJpg`, env=fx-ci
- `https://community-tc.services.mozilla.com/tasks/...` → env=community-tc
- `https://stage.taskcluster.net/tasks/...` → env=staging

## 2. Get task status and definition

```bash
taskcluster task status <taskId>
taskcluster task def <taskId>
```

From status: identify state (`failed`/`exception`/`completed`), number of runs, resolution reason.
From definition: extract `workerPoolId`, `taskQueueId`, `metadata.name`.

## 3. Fetch task log

```bash
taskcluster task log <taskId>
```

For a specific run:
```
https://<root-url>/api/queue/v1/task/<taskId>/runs/<runId>/artifacts/public/logs/live_backing.log
```

Use `curl -sL` or `WebFetch` to retrieve.

## 4. Branch by resolution

### 4a. failed (non-zero exit code)

Check the task log for:
- Exit codes: search for `exit code`, `exited with`, `returned`
- Broken pipe: `write |1|: broken pipe` — usually transient, check if worker was preempted
- OOM: `out of memory`, `killed`, `oom-kill` — task used too much memory
- Command errors: the actual test/build failure

If broken pipe or OOM, identify the worker and check if other tasks on it also failed:
```bash
tc-logview query -e <env> --type monitor.apiMethod \
  --filter '"<WORKER_ID>"' --since 12h --limit 100
```

### 4b. exception — claim-expired

Worker claimed the task but didn't reclaim in time.

```bash
# Extract workerId from the task status runs
# Then check what the worker was doing
tc-logview query -e <env> --type monitor.apiMethod \
  --filter '"<WORKER_ID>"' --since 12h --limit 100
```

Look for:
- Last `reclaimTask` call — did it stop reclaiming?
- `reclaimTask` returning 401 — worker lost credentials
- Only `claimWork` calls with no task work — worker is claiming but tasks keep expiring
- No calls after a certain time — worker died silently

If many tasks in the same pool are claim-expired → see `examples/queue-health-investigation.md`

### 4c. exception — deadline-exceeded

Task wasn't completed before its deadline.

Was it actually running? Check the runs in task status:
- If it has a `workerId` → task was running but took too long. Check the task log for what it was doing.
- If no `workerId` on any run → task was never scheduled. Check pending tasks for this pool:
  ```bash
  tc-logview query -e <env> --type simple-estimate \
    --where 'workerPoolId="<POOL>"' --since 6h
  ```

### 4d. exception — worker-shutdown

Worker shut down while the task was running.

Check why the worker was removed:
```bash
tc-logview query -e <env> --type worker-removed \
  --where 'workerId="<WORKER_ID>"' --since 12h
```

Then follow `examples/worker-removal-reasons.md` based on the removal reason.

## 5. Common failure signatures

| Pattern | Log signature | Next step |
|---|---|---|
| Broken pipe | `write \|1\|: broken pipe` in task log | Check worker for preemption/network issues |
| Livelog port conflict | `listen tcp :60098: bind: address already in use` | Usually non-fatal, check if task continued |
| Docker container crash | Container exited non-zero, veth interface down | Check task log for OOM or command failure |
| GCP preemption | `google_guest_agent ERROR metadata.go: Error watching metadata: context canceled` | Check GCP audit logs |
| Worker idle shutdown | Worker hits idle timeout after task failure | Not root cause — look earlier in timeline |
| reclaimTask 401 | `statusCode="401" name="reclaimTask"` in API logs | Worker lost creds, check if worker restarted |

## 6. Cross-reference with other tasks on the same worker

Check if the failure is isolated to this task or a worker-level problem:

```bash
tc-logview query -e <env> --type monitor.apiMethod \
  --filter '"<WORKER_ID>"' --since 12h --limit 100 --json | \
  jq -r 'select(.name == "claimWork" or .name == "reportCompleted" or .name == "reportFailed") | [.timestamp[0:16], .name, .statusCode] | @tsv'
```

If multiple tasks failed on the same worker: worker-level problem.
If only this task failed: likely a task-specific issue (bad command, resource limits).
