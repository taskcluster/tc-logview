# Worker Removal Reasons

Investigation playbook for workers disappearing, getting removed, or failing to claim work.

## 1. Get removal reason distribution

Start broad — see why workers are being removed for a specific pool (or omit `--where` for all pools):

```bash
tc-logview query -e <env> --type worker-removed \
  --where 'workerPoolId="<POOL>"' --since 6h --limit 200 --json | \
  jq -r '.reason' | sort | uniq -c | sort -rn
```

Then branch by the dominant reason:

## 2a. Zombie workers — "worker never claimed work" / "worker never reclaimed work"

Workers that registered but never reached `claimWork`, or claimed once and never reclaimed.

```bash
tc-logview query -e <env> --type worker-removed \
  --where 'workerPoolId="<POOL>"' \
  --filter '(jsonPayload.Fields.reason=~"worker never claimed work" OR jsonPayload.Fields.reason=~"worker never reclaimed work")' \
  --since 6h --limit 100
```

**Next steps:**
- Check if the pool's idle timeout is too short — workers may be dying before tasks arrive. Look at the pool config's `idleTimeoutSecs`.
- Check if a specific worker reached claimWork at all:
  ```bash
  tc-logview query -e <env> --type monitor.apiMethod \
    --filter '"<WORKER_ID>"' --since 12h --limit 100
  ```
  Healthy flow: `registerWorker` -> `getSecrets` (404 ok) -> `shouldWorkerTerminate` -> `claimWork`.
  If only `registerWorker` + `shouldWorkerTerminate` with no `claimWork`: worker is being told to terminate before it gets to claim.
- If no API calls at all: worker died during boot, check worker system logs (Papertrail) or cloud provider events.

## 2b. terminateAfter time exceeded

Workers removed because they exceeded their maximum lifetime.

```bash
tc-logview query -e <env> --type worker-removed \
  --where 'reason="terminateAfter time exceeded"' \
  --where 'workerPoolId="<POOL>"' --since 6h
```

**Next steps:**
- Check pool config `terminateAfter` value — is it reasonable for the task durations in this pool?
- Cross-reference with simple-estimate to see if workers were still needed when they got killed:
  ```bash
  tc-logview query -e <env> --type simple-estimate \
    --where 'workerPoolId="<POOL>"' --since 6h --limit 100 --json | \
    jq -r '[.timestamp[0:16], "pending=" + .pendingTasks, "existing=" + .existingCapacity] | @tsv'
  ```
  If pendingTasks > 0 when workers are being terminated, the terminateAfter is too short.

## 2c. queueInactivityTimeout

Workers removed because they sat idle with no tasks in their queue for too long.

```bash
tc-logview query -e <env> \
  --filter 'labels."k8s-pod/app_kubernetes_io/name"="taskcluster-worker-manager" "queueInactivityTimeout"' \
  --where 'workerPoolId="<POOL>"' --since 6h
```

**Next steps:**
- Check if tasks are actually being created for this pool's task queue.
- Check if there's a mismatch between `taskQueueId` and `workerPoolId` — workers may be listening on a different queue than where tasks land.

## 2d. task-resolved-by-worker-removed

Tasks that were forcibly resolved because their worker was removed mid-execution.

```bash
tc-logview query -e <env> --type task-resolved-by-worker-removed \
  --where 'workerPoolId="<POOL>"' --since 6h
```

**Next steps:**
- For each affected task, determine why the worker was removed — query worker-removed for the same workerId:
  ```bash
  tc-logview query -e <env> --type worker-removed \
    --where 'workerId="<WORKER_ID>"' --since 12h
  ```
- Check if the tasks got retried successfully:
  ```bash
  taskcluster task status <taskId>
  ```
  Look at the number of runs — if run 1 was resolved by worker removal but run 2 completed, it self-healed.
- If many tasks are affected in the same pool, the root cause is likely in the worker removal reason (go back to step 2a-2c for that worker).
