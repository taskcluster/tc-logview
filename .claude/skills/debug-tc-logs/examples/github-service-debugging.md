# GitHub Service Debugging

Investigation playbook for GitHub integration issues: webhook failures, handler errors, API timeouts.

## 1. Check webhook signature errors

Bad webhook secret — X-Hub-Signature validation failures:

```bash
tc-logview query -e <env> --type monitor.error \
  --filter 'jsonPayload.Fields.message="X-hub-signature does not match"' --since 2h
```

If widespread across many repos: webhook secret rotation needed.
If isolated to one repo: that repo's webhook may have a stale secret.

For more detail on which events failed:

```bash
tc-logview query -e <env> \
  --filter 'jsonPayload.serviceContext.service="github" jsonPayload.message="X-hub-signature does not match; bad webhook secret?"' --since 6h
```

## 2. Check handler processing metrics

See how GitHub event handlers are performing:

```bash
tc-logview query -e <env> --type github-handler-count \
  --filter 'resource.labels.container_name="taskcluster-github-worker"' --since 2h
```

Key fields: `handlerName`, `totalCount`, `runningCount`, `errorCount`, `status`.

High `errorCount` on a specific handler → that handler is failing consistently.
High `runningCount` → handlers are queuing up (processing slower than events arrive).

## 3. Check timed handler durations

Spot handlers that are timing out:

```bash
tc-logview query -e <env> --type monitor.timedHandler \
  --filter 'resource.labels.container_name="taskcluster-github-worker"' --since 2h
```

Key fields: `name`, `duration` (ms), `status` (`success` or `exception`).

Long durations or `status=exception` indicate the handler is hitting timeouts or crashing.

## 4. Check 502s from GitHub API

GitHub returning 502 to our API calls (GitHub outage vs our issue):

```bash
tc-logview query -e <env> \
  --filter 'resource.labels.container_name="taskcluster-github-worker" jsonPayload.message=~"- 502"' --since 2h
```

If these cluster in time, GitHub had an outage. Check [githubstatus.com](https://www.githubstatus.com/) for correlation.

## 5. Task group cancellation

When a new push arrives, TC cancels previous running task groups for the same branch:

```bash
# Finding running task groups to cancel
tc-logview query -e <env> \
  --filter 'jsonPayload.serviceContext.service="github" "found running task groups"' --since 6h

# Actually canceling them
tc-logview query -e <env> \
  --filter 'jsonPayload.serviceContext.service="github" "canceling previous task groups"' --since 6h
```

If cancellations are frequent, it may indicate rapid push sequences (rebases, force-pushes) or flapping CI.

## 6. JSON-e template errors

Errors in `.taskcluster.yml` processing:

```bash
tc-logview query -e <env> --type monitor.error \
  --filter 'jsonPayload.Logger="taskcluster.github.api"' --since 168h
```

Look for `badMessage` field containing `InterpreterError` or `TemplateError`.

To find which repo triggered the error, use the `traceId` from the error entry:
```bash
tc-logview query -e <env> \
  --filter '"<TRACE_ID>"' --since 168h
```

Or search by repo name directly:
```bash
tc-logview query -e <env> \
  --filter '"<REPO_NAME>" severity="ERROR"' --since 24h
```

Note: `owner`/`repoName` fields may be null on error entries. `issue_comment` webhooks containing template text can produce false positives on broad text searches — add `severity="ERROR"` to filter them out.

## 7. Release events

Release events that probably shouldn't trigger TC (usually misconfigured webhooks):

```bash
tc-logview query -e <env> --type monitor.generic \
  --filter 'jsonPayload.serviceContext.service="github" jsonPayload.Fields.eventType="release"' --since 24h
```

Key fields: `body/action`, `body/repository/full_name` — identifies which repo is sending release webhooks to TC.
