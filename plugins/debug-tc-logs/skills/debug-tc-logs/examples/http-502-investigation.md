# HTTP 502 Investigation

Investigation playbook for load balancer 502 errors.

## 1. Get broad 502 count

```bash
tc-logview query -e <env> --filter 'httpRequest.status=502' --since 1h --limit 200
```

## 2. Group by URL path

Identify which endpoints are affected:

```bash
tc-logview query -e <env> --filter 'httpRequest.status=502' --since 2h --limit 500 --json | \
  jq -r '.httpRequest.requestUrl // empty' | sed 's/\?.*//' | sort | uniq -c | sort -rn | head -20
```

## 3. Filter noise

Many 502s are transient or on non-critical paths. Focus on actionable ones by excluding known noise:

```bash
tc-logview query -e <env> \
  --filter 'httpRequest.status=502 "api/queue/v1/claim-work" -"runs/0/reclaim" -"runs/0/completed" -"runs/0/artifacts/" -"api/auth/v1" -"api/worker-manager/v1/worker/register"' \
  --since 2h --limit 100
```

## 4. claim-work 502s

If `api/queue/v1/claim-work` dominates the 502s, workers are failing to reach the queue service:

**Check if queue pods are healthy:**
```bash
tc-logview query -e <env> --type k8s.pod-crash --where 'namespace="taskcluster"' --since 2h
tc-logview query -e <env> --type k8s.events --filter 'jsonPayload.involvedObject.name=~"queue"' --since 2h
```

**Check queue API for 500s (backend errors, not LB errors):**
```bash
tc-logview query -e <env> --type monitor.apiMethod \
  --where 'statusCode="500"' --filter 'jsonPayload.serviceContext.service="queue"' --since 2h
```

**Check if pods are being scaled down during load:**
```bash
tc-logview query -e <env> --type k8s.events --filter '"replica set taskcluster" "scaled down"' --since 2h
```

## 5. Non-claim-work 502s

If other endpoints are affected:

**Check the specific service's pods:**
```bash
# Identify the service from the URL path (e.g., /api/worker-manager/v1/... → worker-manager)
tc-logview query -e <env> --type k8s.pod-crash --where 'pod=~"<SERVICE>"' --since 2h
```

**Check nginx/ingress logs for upstream connection errors:**
```bash
tc-logview query -e <env> \
  --filter 'labels."k8s-pod/fullname"=~"nginx" severity>=WARNING' --since 2h
```

## 6. Correlate with infrastructure events

If 502s are widespread across multiple services, check for cluster-level issues:

```bash
# Node pressure
tc-logview query -e <env> --type k8s.node-pressure --since 2h

# Pod evictions
tc-logview query -e <env> --type k8s.pod-evicted --since 2h

# Autoscaler decisions
tc-logview query -e <env> --type k8s.autoscaler --since 2h
```
