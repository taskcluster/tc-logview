# Queue Health Investigation

Investigation playbook for queue issues: claim-expired tasks, deadline-exceeded, pulse failures, artifact expiration.

## 1. Count claim-expired vs deadline-exceeded

These two metrics give the clearest picture of queue health:

```bash
# Claim-expired: workers claimed tasks but didn't finish in time
tc-logview query -e <env> --type task-exception \
  --filter 'jsonPayload.Logger="taskcluster.queue.claim-resolver"' --since 1h --limit 200 --json | \
  jq -r '.taskQueueId // .workerPoolId // "unknown"' | sort | uniq -c | sort -rn

# Deadline-exceeded: tasks weren't completed before their deadline
tc-logview query -e <env> --type task-exception \
  --filter 'jsonPayload.Logger="taskcluster.queue.deadline-resolver"' --since 1h --limit 200 --json | \
  jq -r '.taskQueueId // .workerPoolId // "unknown"' | sort | uniq -c | sort -rn
```

## 2. Interpret the distribution

**Claim-expired concentrated in one pool:**
Workers in that pool are claiming tasks but failing to complete or reclaim them. Likely a worker-level problem:
- Check worker removal reasons → see `examples/worker-removal-reasons.md`
- Check if workers are crashing mid-task: query `worker-removed` for that pool

**Deadline-exceeded concentrated in one pool:**
Tasks aren't being scheduled or are stuck in the queue:
```bash
tc-logview query -e <env> --type simple-estimate \
  --where 'workerPoolId="<POOL>"' --since 2h --limit 100 --json | \
  jq -r '[.timestamp[0:16], "pending=" + .pendingTasks, "existing=" + .existingCapacity, "requested=" + .requestedCapacity] | @tsv'
```
If `pendingTasks` is high but `existingCapacity` is 0: provisioner isn't creating workers.
If `existingCapacity` > 0 but tasks aren't being claimed: workers exist but aren't claiming from the right queue.

**Widespread across many pools:**
Systemic issue — proceed to step 3 (pulse health).

## 3. Check pulse (RabbitMQ) health

Pulse message delivery failures cascade into claim-expired and deadline-exceeded:

```bash
# PulsePublisher.sendDeadline exceeded — message delivery timing out
tc-logview query -e <env> \
  --filter '"PulsePublisher.sendDeadline exceeded"' --since 1h

# pulsePublisherBlocked — publisher blocked by RabbitMQ flow control
tc-logview query -e <env> \
  --filter '"pulsePublisherBlocked"' --since 1h
```

If either is present: RabbitMQ is under pressure. Check:
- Is RabbitMQ memory/disk alarm triggered?
- Are there queues with large backlogs?
- Has a consumer stopped draining messages?

The pulse failures are often the root cause of widespread queue problems.

## 4. Check expire-artifacts health

Artifact expiration running behind can indicate broader queue/DB pressure:

**Run times:**
```bash
tc-logview query -e <env> --type monitor.timedHandler \
  --filter 'jsonPayload.Fields.name="expire-artifacts"' --since 24h --limit 100
```

Look at `duration` (ms) and `status`. Growing durations = backlog building up.

**Removed counts:**
```bash
tc-logview query -e <env> --type expired-artifacts-removed --since 24h --limit 100
```

Key fields: `count`, `prefix`. High counts are normal during catch-up; watch for the count trending upward over time.

If expire-artifacts is slow, check for CloudSQL slow queries:
```bash
tc-logview query -e <env> --type cloudsql.slow-query --since 2h
```

## 5. Investigate specific claim-expired tasks

For a specific task that was claim-expired:

```bash
# Get the task status to find workerId
taskcluster task status <taskId>
```

Extract `workerId` from the run that was claim-expired, then check what that worker was doing:

```bash
tc-logview query -e <env> --type monitor.apiMethod \
  --filter '"<WORKER_ID>"' --since 12h --limit 100
```

Look for:
- Last `claimWork` / `reclaimTask` calls — did the worker stop reclaiming?
- `reclaimTask` returning 401 — worker lost credentials (restarted?)
- No calls after a certain time — worker died silently
