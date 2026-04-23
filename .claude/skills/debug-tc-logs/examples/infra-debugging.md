# Infrastructure Debugging

Investigation playbook for service pods crashing, k8s cluster issues, database problems, and broad TC errors.

## 1. Service pods crashing / not starting

Start with crash loops, then widen:

```bash
# Pod crash loops and BackOff events
tc-logview query -e <env> --type k8s.pod-crash --where 'namespace="taskcluster"' --since 2h

# OOM kills (k8s level)
tc-logview query -e <env> --type k8s.oom-kill --since 2h

# Cgroup OOM (kernel level — different from k8s.oom-kill, catches cases k8s doesn't report)
tc-logview query -e <env> --type k8s.events --filter '"Memory cgroup out of memory"' --since 2h

# Container died events (includes normal CronJob exits — filter for unexpected ones)
tc-logview query -e <env> --type k8s.container-died --since 2h

# Health probe failures (pod exists but isn't responding)
tc-logview query -e <env> --type k8s.pod-unhealthy --where 'namespace="taskcluster"' --since 2h
```

For a specific pod, get all events:
```bash
tc-logview query -e <env> --type k8s.events --where 'pod="<POD_NAME>"' --since 2h
```

## 2. Cluster scaling issues / pods pending

```bash
# Scheduling failures (no node has capacity)
tc-logview query -e <env> --type k8s.pod-scheduling --since 2h

# Autoscaler decisions
tc-logview query -e <env> --type k8s.autoscaler --since 2h

# Scale up/down events for TC deployments
tc-logview query -e <env> --type k8s.events --filter '"replica set taskcluster"' --since 2h

# Node removal (autoscaler removing underutilized nodes)
tc-logview query -e <env> --type k8s.events --filter '"removing empty node"' --since 2h

# Node pressure (memory/disk/PID)
tc-logview query -e <env> --type k8s.node-pressure --since 2h

# Pod evictions (node ran out of resources)
tc-logview query -e <env> --type k8s.pod-evicted --since 6h
```

If pods are pending and autoscaler isn't scaling up: check if the node pool has hit its maximum size or if there's a quota issue in the cloud project.

## 3. Database issues

```bash
# CloudSQL errors
tc-logview query -e <env> --type cloudsql.errors --since 2h

# Slow queries (often the root cause of API timeouts)
tc-logview query -e <env> --type cloudsql.slow-query --since 2h
```

Slow queries often correlate with:
- API 500s (statement timeout)
- Scanner slowdowns (DB operations blocking the scan loop)
- expire-artifacts taking longer than usual

## 4. Broad TC errors triage

When something is wrong but unclear what, get the error distribution across all services:

```bash
tc-logview query -e <env> \
  --filter 'severity>=WARNING labels."k8s-pod/app_kubernetes_io/part-of"="taskcluster"' \
  --since 1h --limit 300 --json | \
  jq -r '[.serviceContext.service // "unknown", .Type // "unknown"] | @tsv' | sort | uniq -c | sort -rn
```

This shows which service is producing the most warnings/errors and what type they are. Follow up on the top entries:

```bash
# Drill into a specific service's errors
tc-logview query -e <env> --type monitor.error \
  --filter 'jsonPayload.serviceContext.service="<SERVICE>"' --since 1h --limit 100 --json | \
  jq -r '.message[:80]' | sort | uniq -c | sort -rn
```

## 5. Cross-service correlation

When multiple services are failing simultaneously, look for shared infrastructure causes:

1. **Database pressure** → Check `cloudsql.errors` and `cloudsql.slow-query` (step 3)
2. **Pulse/RabbitMQ** → See `examples/queue-health-investigation.md` step 3
3. **Node pressure** → Check `k8s.node-pressure` and `k8s.pod-evicted` (step 2)
4. **Network** → Check 502s across services → see `examples/http-502-investigation.md`
